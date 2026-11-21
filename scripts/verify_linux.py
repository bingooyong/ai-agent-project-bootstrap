"""Run OMBP in a local Linux arm64 guest and record the runtime evidence."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import tempfile
import time


ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / "docs/05-testing/evidence"
PACKAGES = ("safeio", "updater", "governance", "cli")
CASE_MARKER = "@@ombp-case@@ "
FLOW = r"""
set -eu
printf 'runtime=%s %s\n' "$(uname -s)" "$(uname -m)"
test "$(uname -s):$(uname -m)" = 'Linux:aarch64'
mkdir -m 700 /tmp/project /tmp/outside
printf 'module example.test/linux-fixture\n\ngo 1.25.0\n' > /tmp/project/go.mod

probe() {
    name=$1
    want=$2
    shift 2
    got=0
    /artifacts/ombp "$@" --format json >/tmp/ombp-stdout.json 2>/tmp/ombp-stderr || got=$?
    printf '@@ombp-case@@ %s %s\n' "$name" "$got"
    cat /tmp/ombp-stdout.json
    cat /tmp/ombp-stderr >&2
    test "$got" -eq "$want"
}

probe scan 0 scan /tmp/project
printf 'module example.test/linux-fixture\ngo 1.25.0\ngo 1.26.0\n' > /tmp/project/go.mod
probe scan-duplicate-go 3 scan /tmp/project
grep -q '"operation_status": "incomplete"' /tmp/ombp-stdout.json
grep -q '"status": "incomplete"' /tmp/ombp-stdout.json
printf 'module example.test/linux-fixture\n\ngo 1.25.0\n' > /tmp/project/go.mod
probe dry-run 0 init /tmp/project --dry-run
test ! -e /tmp/project/AGENTS.md
test ! -e /tmp/project/.ombp
probe save-plan 0 init /tmp/project --plan-out /tmp/reviewed-plan.json
test ! -e /tmp/project/AGENTS.md
test ! -e /tmp/project/.ombp
probe apply 0 init /tmp/project --apply --plan /tmp/reviewed-plan.json
test -s /tmp/project/AGENTS.md
test -s /tmp/project/.ombp/lock.yaml
probe doctor 0 doctor /tmp/project
sh -c 'exit 0' & dead_owner=$!
wait "$dead_owner"
printf '{"pid":%s,"nonce":"%032d"}\n' "$dead_owner" 1 > /tmp/project/.ombp/state/mutex.json
probe doctor-orphan-lock 4 doctor /tmp/project
grep -q 'WRITE_MUTEX_PRESENT' /tmp/ombp-stdout.json
probe preview-orphan-lock 0 recover /tmp/project
mutex_sha=$(sed -n 's/.*"mutex_sha256": "\([a-f0-9]*\)".*/\1/p' /tmp/ombp-stdout.json)
test "${#mutex_sha}" -eq 64
probe recover-orphan-lock 0 recover /tmp/project --apply --mutex-sha "$mutex_sha"
test ! -e /tmp/project/.ombp/state/mutex.json
rm -rf /tmp/project/.ombp/state
ln -s /tmp/outside /tmp/project/.ombp/state
probe doctor-state-unverified 3 doctor /tmp/project
grep -q '"operation_status": "incomplete"' /tmp/ombp-stdout.json
rm -f /tmp/project/.ombp/state
mkdir -m 700 /tmp/project/.ombp/state
probe check-and-save-report 0 check /tmp/project --ci --output check.json
cmp /tmp/project/check.json /tmp/ombp-stdout.json
cp /tmp/project/check.json /tmp/first-report.json
probe refuse-report-overwrite 3 check /tmp/project --output check.json
cmp /tmp/project/check.json /tmp/first-report.json
probe idempotent 0 init /tmp/project --dry-run
ln -s /tmp/outside /tmp/project/linked
probe reject-linked-parent 4 check /tmp/project --output linked/report.json
test ! -e /tmp/outside/report.json
"""
EXPECTED = {
    "scan": 0,
    "scan-duplicate-go": 3,
    "dry-run": 0,
    "save-plan": 0,
    "apply": 0,
    "doctor": 0,
    "doctor-orphan-lock": 4,
    "preview-orphan-lock": 0,
    "recover-orphan-lock": 0,
    "doctor-state-unverified": 3,
    "check-and-save-report": 0,
    "refuse-report-overwrite": 3,
    "idempotent": 0,
    "reject-linked-parent": 4,
}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_hashes():
    files = (ROOT / "go.mod", ROOT / "go.sum", Path(__file__).resolve(),
             *ROOT.glob("cmd/**/*.go"), *ROOT.glob("internal/**/*.go"),
             *ROOT.glob("internal/rules/builtin/*.md"))
    return {str(path.relative_to(ROOT)): digest(path) for path in sorted(files)}


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n")


def evaluate_flow(stdout, _stderr):
    """Validate machine-readable outcomes and keep the evidence compact."""
    runtime, *cases = stdout.split(CASE_MARKER)
    if runtime.strip() != "runtime=Linux aarch64":
        raise ValueError(f"unexpected guest environment: {runtime}")
    evidence = {}
    for case in cases:
        header, body = case.split("\n", 1)
        name, exit_code = header.split()
        expected = EXPECTED[name]
        value = json.loads(body)
        if int(exit_code) != expected or value["exit_code"] != expected or value["command_execution"] != "NOT_RUN":
            raise ValueError(f"unexpected outcome in {name}")
        data = value.get("data") or {}
        codes = [finding["code"] for finding in value.get("findings", [])]
        entry = {"expected_exit_code": expected, "actual_exit_code": int(exit_code),
                 "command_execution": value["command_execution"], "result": value["result"], "finding_codes": codes}
        if name == "doctor":
            if (data["os"], data["architecture"]) != ("linux", "arm64"):
                raise ValueError("CLI did not run as Linux arm64")
            entry["cli_platform"] = f"{data['os']}/{data['architecture']}"
        if name == "doctor-orphan-lock" and "WRITE_MUTEX_PRESENT" not in codes:
            raise ValueError("doctor did not report the retained writer mutex")
        if name == "doctor-state-unverified" and (value["operation_status"], value["result"]) != ("incomplete", "error"):
            raise ValueError("doctor claimed an unsafe transaction path was fully inspected")
        if name in {"scan", "check-and-save-report"}:
            entry["coverage"] = value["coverage"]["status"]
            if entry["coverage"] != "complete":
                raise ValueError(f"unexpected partial coverage in {name}")
        if name == "scan-duplicate-go" and (value["operation_status"], value["coverage"]["status"]) != ("incomplete", "incomplete"):
            raise ValueError("duplicate go.mod version directives were not marked incomplete")
        if name == "apply":
            entry["changed_files"] = data["changed_files"]
            if entry["changed_files"] != 4 or data["applied"] is not True:
                raise ValueError("the reviewed plan did not apply exactly four changes")
        if name in {"dry-run", "save-plan", "idempotent"}:
            entry["changed_paths"] = [change["path"] for change in data["changes"]]
            if name == "idempotent" and entry["changed_paths"]:
                raise ValueError("worktree differs after replay")
            if name != "idempotent" and set(entry["changed_paths"]) != {
                ".ombp/project.yaml", ".ombp/profile.json", ".ombp/lock.yaml", "AGENTS.md",
            }:
                raise ValueError(f"unexpected change set in {name}")
        if name in {"refuse-report-overwrite", "reject-linked-parent"} and "REPORT_WRITE_FAILED" not in codes:
            raise ValueError(f"missing report write finding in {name}")
        evidence[name] = entry
    if list(evidence) != list(EXPECTED):
        raise ValueError("Linux CLI workflow did not exercise each expected case in order")
    return {"runtime": runtime.strip().removeprefix("runtime="), "cases": evidence}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True, help="local Alpine image ID (sha256:<digest>)")
    parser.add_argument("--out", required=True, type=Path, help="new directory under docs/05-testing/evidence")
    args = parser.parse_args()
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", args.image):
        parser.error("--image must be a complete local image ID")
    out = args.out.resolve()
    if not out.is_relative_to(EVIDENCE) or out == EVIDENCE:
        parser.error("--out must be a new child directory under docs/05-testing/evidence")
    out.mkdir(parents=True, exist_ok=False)

    env = {**os.environ, "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local",
           "GOWORK": "off", "GOFLAGS": "", "CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "arm64"}
    results = []
    before = source_hashes()
    write_json(out / "source-hashes-before.json", before)
    summary = {"status": "FAIL", "host": platform.platform(), "image_id": args.image,
               "build_environment": {key: env[key] for key in ("GOPROXY", "GOSUMDB", "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "CGO_ENABLED", "GOOS", "GOARCH")},
               "scope": "Linux arm64 runtime in isolated Docker guest, CGO and race instrumentation disabled",
               "results": results}

    def run(name, argv, inspect=None):
        started = datetime.now(timezone.utc).isoformat()
        start = time.monotonic()
        record = {"command": argv, "cwd": str(ROOT), "started_at": started, "expected_exit_code": 0, "status": "FAIL"}
        try:
            proc = subprocess.run(argv, cwd=ROOT, env=env, capture_output=True, text=True, timeout=180)
            record.update({"exit_code": proc.returncode, "stderr": proc.stderr})
            if proc.returncode != 0 or inspect is None:
                record["stdout"] = proc.stdout
            if proc.returncode != 0:
                raise RuntimeError(f"{name} returned {proc.returncode}")
            observed = inspect(proc.stdout, proc.stderr) if inspect else proc.stdout
            if inspect:
                record["observed"] = observed
            record["status"] = "PASS"
            return observed
        except Exception as exc:
            record["error"] = str(exc)
            raise
        finally:
            record["duration_seconds"] = round(time.monotonic() - start, 3)
            name_json = name + ".json"
            write_json(out / name_json, record)
            results.append({"name": name, "status": record["status"], "log": name_json})
            print(name, record["status"], flush=True)

    try:
        context = run("docker-context", ["docker", "context", "show"]).strip()
        endpoint = run("docker-endpoint", ["docker", "context", "inspect", context,
                                           "--format", "{{.Endpoints.docker.Host}}"]).strip()
        if not endpoint.startswith("unix:///"):
            raise RuntimeError("a local Docker Unix socket is required")
        docker = ["docker", "--context", context]
        server = run("docker-server", docker + ["version", "--format", "{{.Server.Os}}/{{.Server.Arch}} {{.Server.Version}}"])
        if server.split()[0] != "linux/arm64":
            raise RuntimeError("the Docker guest must be Linux arm64")
        image_meta = run("image", docker + ["image", "inspect", args.image, "--format",
                                           "{{.Id}} {{.Os}}/{{.Architecture}} {{json .Config.Volumes}} {{json .RepoDigests}}"])
        image_id, image_platform, volumes, _digests = image_meta.split(maxsplit=3)
        if image_id != args.image or image_platform != "linux/arm64" or volumes != "null":
            raise RuntimeError("expected the selected local arm64 image without implicit volumes")

        run("go-version", ["go", "version"])
        # A temporary name under the repository parent keeps the bind mount
        # visible to desktop Docker VMs. Only this new directory is mounted.
        with tempfile.TemporaryDirectory(prefix="ombp-linux-", dir=ROOT.parent) as directory:
            temp = Path(directory).resolve()
            if "," in str(temp):
                raise RuntimeError("temporary artifact path may not contain a comma")
            temp.chmod(0o755)
            run("build-ombp", ["go", "build", "-buildvcs=false", "-trimpath", "-o", str(temp / "ombp"), "./cmd/ombp"])
            for package in PACKAGES:
                run(f"build-test-{package}", ["go", "test", "-c", "-buildvcs=false", "-trimpath",
                                         "-o", str(temp / f"{package}.test"), f"./internal/{package}"])
            (temp / "flow.sh").write_text(FLOW)
            summary["artifact_sha256"] = {path.name: digest(path) for path in sorted(temp.iterdir())}
            run_args = docker + ["run", "--rm", "--pull=never", "--platform=linux/arm64", "--network=none",
                                 "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
                                 "--user=65534:65534", "--no-healthcheck", "--workdir=/tmp",
                                 "--tmpfs=/tmp:rw,nosuid,nodev,noexec,size=32m,mode=1777",
                                 "--mount", f"type=bind,src={temp},dst=/artifacts,readonly",
                                 "--entrypoint=/bin/sh", args.image]
            run("cli-linux-arm64", run_args + ["/artifacts/flow.sh"], inspect=evaluate_flow)
            for package in PACKAGES:
                run(f"test-{package}-linux-arm64", run_args + ["-c", 'exec "$@"', "sh", f"/artifacts/{package}.test",
                                                             "-test.v", "-test.timeout=60s"])
        summary["status"] = "PASS"
    finally:
        after = source_hashes()
        summary["source_stable"] = (before == after)
        if not summary["source_stable"]:
            summary["status"] = "FAIL"
        write_json(out / "source-hashes.json", after)
        write_json(out / "results.json", summary)
        if not summary["source_stable"]:
            raise RuntimeError("source changed during validation; rerun in a new evidence directory")


if __name__ == "__main__":
    main()
