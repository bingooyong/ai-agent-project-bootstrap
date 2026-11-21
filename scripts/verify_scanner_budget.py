"""Measure a fresh local macOS CLI against disposable synthetic scan fixtures."""
import argparse
from datetime import datetime, timezone
from hashlib import sha256
import json
import os
from pathlib import Path
import platform
import re
import subprocess
from tempfile import TemporaryDirectory
import time


ROOT = Path(__file__).resolve().parents[1]
OFFLINE = {"GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "LC_ALL": "C"}
MANIFESTS = {
    "go.mod": "module example.test/bench\ngo 1.25\n",
    "api/pyproject.toml": "[project]\nname='api'\ndependencies=['fastapi>=0.100']\n",
    "web/package.json": '{"dependencies":{"react":"19"},"scripts":{"test":"touch SHOULD_NOT_RUN"}}\n',
    "web/tsconfig.json": '{"compilerOptions":{"strict":true}}\n',
}
EXCLUDED = {
    ".env": "SYNTHETIC_SECRET_DO_NOT_REPORT\n",
    "node_modules/fake/package.json": "intentionally invalid JSON",
    "secrets/package.json": "intentionally invalid JSON",
}


def digest(data):
    return sha256(data).hexdigest()


def source_hashes():
    paths = [*ROOT.glob("internal/**/*.go"), *ROOT.glob("internal/rules/builtin/*.md"),
             *ROOT.glob("cmd/**/*.go"), ROOT / "go.mod", ROOT / "go.sum", Path(__file__).resolve()]
    return {str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in sorted(set(paths))}


def run(argv, cwd, records):
    start = time.monotonic()
    proc = subprocess.run(argv, cwd=cwd, env=os.environ | OFFLINE, capture_output=True,
                          text=True, timeout=45)
    record = {"command": list(map(str, argv)), "cwd": str(cwd), "exit_code": proc.returncode,
              "elapsed_ms": round((time.monotonic() - start) * 1000, 3),
              "stdout": proc.stdout, "stderr": proc.stderr}
    records.append(record)
    return record


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def scan(name, binary, fixture, expected, records, *flags, expected_entries=10_109):
    record = run(["/usr/bin/time", "-l", binary, "scan", fixture, "--format", "json", *flags],
                 fixture, records)
    record["name"] = name
    rss = re.search(r"^\s*(\d+)\s+maximum resident set size$", record["stderr"], re.M)
    require(rss is not None, "macOS time did not report peak resident memory")
    record["peak_rss_bytes"] = int(rss[1])  # Darwin reports ru_maxrss in bytes.
    require(record["exit_code"] == expected, f"{name}: unexpected process exit")
    envelope = json.loads(record["stdout"])
    require(envelope["exit_code"] == expected and envelope["command_execution"] == "NOT_RUN",
            f"{name}: incorrect report exit or command execution state")
    require(not (fixture / "SHOULD_NOT_RUN").exists() and not (fixture / "web/SHOULD_NOT_RUN").exists()
            and "SYNTHETIC_SECRET_DO_NOT_REPORT" not in record["stdout"],
            f"{name}: fixture script ran or sensitive sentinel leaked")
    data = envelope["data"]
    record["coverage"] = data["coverage"]
    if expected == 0:
        require(envelope["operation_status"] == data["coverage"]["status"] == "complete",
                f"{name}: expected complete scan")
        require({p: digest(body.encode()) for p, body in MANIFESTS.items()} == data["inputs"],
                f"{name}: incorrect read set or source digest")
        require({p.split("/")[0] for p in EXCLUDED} <= set(data["coverage"]["excluded"]),
                f"{name}: exclusion coverage is missing")
        require(data["coverage"]["files_visited"] == expected_entries,
                f"{name}: scan did not visit the expected fixture entries")
    else:
        require(envelope["operation_status"] == data["coverage"]["status"] == "incomplete"
                and 0 < data["coverage"]["files_visited"] <= 20_001 and data["coverage"]["bytes_read"] == 0
                and not data["inputs"],
                f"{name}: entry ceiling did not stop coverage before reading declarations")
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, help="new evidence directory under docs/05-testing/evidence")
    args = parser.parse_args()
    out = Path(args.out).resolve()
    out.relative_to(ROOT / "docs/05-testing/evidence")
    require(platform.system() == "Darwin", "this measurement uses Darwin /usr/bin/time -l")
    out.mkdir(parents=True, exist_ok=False)
    before = source_hashes()
    results = {"status": "FAIL", "platform": platform.platform(), "environment": OFFLINE,
               "started_at": datetime.now(timezone.utc).isoformat(), "records": [],
               "fixture": {"directories_of_100_files": 100, "additional_files_at_cap": 9_890,
                           "additional_files_over_cap": 10_001,
                           "manifests": sorted(MANIFESTS), "excluded": sorted(EXCLUDED)}}
    records = results["records"]
    try:
        with TemporaryDirectory(prefix="ombp-scanner-budget-") as tmp:
            sandbox = Path(tmp)
            fixture = sandbox / "project"
            binary = sandbox / "ombp"
            build = run(["go", "build", "-trimpath", "-o", binary, "./cmd/ombp"], ROOT, records)
            build["name"] = "build-native"
            require(build["exit_code"] == 0, "native CLI build failed")
            results["binary_sha256"] = digest(binary.read_bytes())
            results["go_version"] = subprocess.check_output(["go", "version"], text=True,
                                                            env=os.environ | OFFLINE).strip()
            for name, body in (MANIFESTS | EXCLUDED).items():
                file = fixture / name
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_text(body, encoding="utf-8")
            for group in range(100):
                folder = fixture / f"source-{group:03d}"
                folder.mkdir()
                for item in range(100):
                    (folder / f"ignored-{item:03d}.txt").touch()
            for index in range(1, 4):
                scan(f"normal-{index}", binary, fixture, 0, records)
            bulk = fixture / "bulk"
            bulk.mkdir()
            for item in range(9_890):
                (bulk / f"ignored-{item:05d}.txt").touch()
            scan("exact-entry-cap", binary, fixture, 0, records, expected_entries=20_000)
            for item in range(9_890, 10_001):
                (bulk / f"ignored-{item:05d}.txt").touch()
            bulk_scan = scan("entry-cap", binary, fixture, 3, records)
            require("SCAN_INCOMPLETE" in bulk_scan["stdout"], "entry cap lacks its incomplete finding")
            recovered = scan("exclude-bulk", binary, fixture, 0, records, "--exclude", "bulk",
                             expected_entries=10_110)
            require("bulk" in recovered["coverage"]["excluded"], "explicit exclusion is not reported")
        results["status"] = "PASS"
    except (RuntimeError, subprocess.SubprocessError, ValueError, OSError) as error:
        results["error"] = str(error)
        raise
    finally:
        results["source_sha256"] = before
        results["source_stable"] = before == source_hashes()
        if not results["source_stable"]:
            results["status"] = "FAIL"
        results["finished_at"] = datetime.now(timezone.utc).isoformat()
        (out / "results.json").write_text(json.dumps(results, indent=2) + "\n", encoding="utf-8")
    require(results["source_stable"], "source changed during the measurement")
    for record in records[1:]:
        print(record["name"], record["exit_code"], record["elapsed_ms"], record["peak_rss_bytes"],
              record["coverage"]["files_visited"])


if __name__ == "__main__":
    main()
