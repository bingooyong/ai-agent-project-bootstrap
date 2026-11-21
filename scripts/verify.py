"""Record fresh local evidence without rewriting earlier runs or executing fixture scripts."""
from pathlib import Path
from datetime import datetime, timezone
import argparse
import hashlib
import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("--out", required=True, help="new evidence directory, must not exist")
parser.add_argument("--go125", type=Path, help="optional installed Go 1.25 executable for minimum-series tests")
args = parser.parse_args()
out = Path(args.out).resolve()
out.relative_to(ROOT / "docs")
out.mkdir(parents=True, exist_ok=False)
env = dict(os.environ, GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local")
results = []


def sources():
    files = sorted([*ROOT.glob("go.*"), *ROOT.glob("Makefile"),
                    *ROOT.glob("internal/**/*.go"), *ROOT.glob("internal/rules/builtin/*.md"),
                    *ROOT.glob("cmd/**/*.go"), *ROOT.glob("schemas/*.json"), *ROOT.glob("schemas/*.go"),
                    *(ROOT / "scripts" / name for name in ("verify.py", "check_plan.py", "gen_schema.go"))])
    return {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in files if p.is_file()}


before_sources = sources()
(out / "source-hashes-before.json").write_text(json.dumps(before_sources, indent=2) + "\n")


def run(name, argv, *, cwd=ROOT, expected=0, quiet=False, env_extra=None):
    started = datetime.now(timezone.utc).isoformat()
    process = subprocess.run(argv, cwd=cwd, env=dict(env, **(env_extra or {})),
                             capture_output=True, text=True, timeout=240)
    log = out / (name + ".json")
    record = {"name": name, "command": argv, "cwd": str(cwd), "started_at": started,
              "exit_code": process.returncode, "expected_exit_code": expected,
              "stdout": process.stdout, "stderr": process.stderr,
              "status": "PASS" if process.returncode == expected and (not quiet or not process.stdout.strip()) else "FAIL"}
    log.write_text(json.dumps(record, indent=2, ensure_ascii=False) + "\n")
    results.append({"name": name, "status": record["status"], "log": str(log.relative_to(ROOT))})
    print(name, record["status"], flush=True)
    if record["status"] != "PASS":
        raise RuntimeError(f"{name} failed; see {log}")
    return process.stdout


try:
    run("format", ["gofmt", "-l", "cmd", "internal", "scripts/gen_schema.go"], quiet=True)
    run("module-verify", ["go", "mod", "verify"])
    run("vet", ["go", "vet", "./..."])
    run("race", ["go", "test", "-race", "-count=1", "./..."])
    if args.go125:
        go125 = str(args.go125.resolve())
        out125 = run("go125-version", [go125, "version"])
        if out125.split()[2].split(".")[:2] != ["go1", "25"]:
            raise RuntimeError("--go125 must select a locally installed Go 1.25 toolchain")
        run("go125-race", [go125, "test", "-race", "-count=1", "./..."])
    run("schema-freshness", ["go", "run", "scripts/gen_schema.go", "--check"])
    run("task-and-docs", ["python3", "scripts/check_plan.py"])
    run("whitespace", ["git", "diff", "--check"])
    with tempfile.TemporaryDirectory(prefix="ombp-e2e-") as tmp:
        temp = Path(tmp)
        binary = temp / "ombp"
        run("build-native", ["go", "build", "-trimpath", "-o", str(binary), "./cmd/ombp"])
        project = temp / "project"
        project.mkdir()
        (project / "go.mod").write_text("module example.test/fixture\n\ngo 1.25.0\n")

        def cli(name, command, *flags, expected=0, target=project):
            body = run(name, [str(binary), command, str(target), *flags, "--format", "json"], expected=expected)
            value = json.loads(body)
            assert value["exit_code"] == expected and value["command_execution"] == "NOT_RUN"
            return value

        assert cli("scan-static", "scan")["coverage"]["status"] == "complete"
        go_manifest = project / "go.mod"
        original_go = go_manifest.read_bytes()
        go_manifest.write_text("module example.test/fixture\ngo 1.25.0\ngo 1.26.0\n")
        invalid_go = cli("scan-reject-duplicate-go", "scan", expected=3)
        assert invalid_go["operation_status"] == "incomplete" and invalid_go["coverage"]["status"] == "incomplete"
        go_manifest.write_bytes(original_go)
        before = sorted(str(p.relative_to(project)) for p in project.rglob("*"))
        cli("init-dry-run", "init", "--dry-run", "--print-diff")
        assert sorted(str(p.relative_to(project)) for p in project.rglob("*")) == before
        plan = temp / "plan.json"
        cli("init-plan", "init", "--plan-out", str(plan))
        cli("init-apply", "init", "--apply", "--plan", str(plan))
        assert cli("check-static", "check", "--ci")["coverage"]["status"] == "complete"
        again = cli("init-idempotent", "init", "--dry-run")
        assert again["data"]["changes"] == []
        cli("profile-stored", "profile")
        report_file = project / "check-report.json"
        check_report = cli("check-save-report", "check", "--ci", "--output", report_file.name)
        assert json.loads(report_file.read_text()) == check_report
        cli("check-refuse-report-overwrite", "check", "--output", report_file.name, expected=3)
        assert json.loads(report_file.read_text()) == check_report
        cli("dry-run-refuse-report", "init", "--dry-run", "--output", "dry-run-report.json", expected=2)
        assert not (project / "dry-run-report.json").exists()
        configuration = project / "engineering.yaml"
        configuration_body = "schema_version: '1.0'\nagents: [codex]\n"
        configuration.write_text(configuration_body)
        config_plan = temp / "selected-config-plan.json"
        cli("selected-config-plan", "init", "--config", configuration.name, "--plan-out", str(config_plan))
        configuration.write_text(configuration_body + "# bytes changed after review\n")
        cli("selected-config-stale", "init", "--apply", "--plan", str(config_plan), expected=4)
        configuration.write_text(configuration_body)
        cli("selected-config-apply", "init", "--apply", "--plan", str(config_plan))
        cli("selected-config-check", "check", "--ci", "--config", configuration.name)
        agents = project / "AGENTS.md"
        clean_agents = agents.read_bytes()
        with agents.open("a") as handle:
            handle.write("\n[Project instructions](docs/missing.md)\n")
        broken = cli("check-broken-reference", "check", "--config", configuration.name, expected=1)
        assert any(f["code"] == "DOCUMENT_REFERENCE_MISSING" and f["location"]["path"] == "AGENTS.md"
                   and f["location"]["line"] > 0
                   for f in broken["findings"])
        agents.write_text(agents.read_text().replace("Engineering instructions", "User modified instructions"))
        cli("check-protect-manual", "check", "--ci", "--config", configuration.name, expected=4)
        cli("init-protect-manual", "init", "--dry-run", "--config", configuration.name, expected=4)
        with subprocess.Popen([sys.executable, "-c", "pass"], cwd=temp, env=env) as owner:
            assert owner.wait(timeout=10) == 0
            mutex = project / ".ombp/state/mutex.json"
            mutex.write_text(json.dumps({"pid": owner.pid, "nonce": "a" * 32}) + "\n")
        before_agents = agents.read_bytes()
        orphan = cli("recover-orphan-preview", "recover")
        assert orphan["data"]["state"] == "orphaned_mutex" and mutex.exists()
        mutex_sha = orphan["data"]["mutex_sha256"]
        cli("recover-orphan-refuse-wrong-digest", "recover", "--apply", "--mutex-sha", "b" * 64, expected=4)
        assert mutex.exists()
        cli("recover-orphan-refuse-ci", "recover", "--apply", "--mutex-sha", mutex_sha, "--ci", expected=2)
        cli("recover-orphan-apply", "recover", "--apply", "--mutex-sha", mutex_sha)
        assert not mutex.exists() and agents.read_bytes() == before_agents
        with subprocess.Popen([sys.executable, "-c", "pass"], cwd=temp, env=env) as owner:
            assert owner.wait(timeout=10) == 0
            mutex.write_text(json.dumps({"pid": owner.pid, "nonce": "c" * 32}) + "\n")
        doctor_state = cli("doctor-orphan-lock", "doctor", "--config", configuration.name,
                           expected=4)
        assert any(f["code"] == "WRITE_MUTEX_PRESENT" and f["blocking"]
                   for f in doctor_state["findings"])
        view = cli("recover-orphan-second-preview", "recover")
        assert view["data"]["state"] == "orphaned_mutex"
        cli("recover-orphan-second-apply", "recover", "--apply", "--mutex-sha",
            view["data"]["mutex_sha256"])
        assert not mutex.exists()
        shutil.rmtree(mutex.parent)
        (project / ".ombp/state").symlink_to(temp / "outside-state")
        incomplete_doctor = cli("doctor-transaction-state-unverified", "doctor", "--config",
                                configuration.name, expected=3)
        assert incomplete_doctor["operation_status"] == "incomplete" and incomplete_doctor["result"] == "error"
        (project / ".ombp/state").unlink()
        js = temp / "javascript"
        js.mkdir()
        package = js / "package.json"
        package.write_text('{"packageManager":"pnpm@10.0.0","scripts":{"test":"touch SHOULD_NOT_EXIST"}}\n')
        node_scan = cli("scan-node-manager-source", "scan", target=js)
        assert [e["pointer"] for e in node_scan["data"]["commands"][0]["evidence"]] == ["/scripts/test", "/packageManager"]
        js_plan = temp / "js-plan.json"
        cli("js-plan", "init", "--plan-out", str(js_plan), target=js)
        cli("js-apply", "init", "--apply", "--plan", str(js_plan), target=js)
        package.write_text("{}\n")
        command_drift = cli("check-command-source-removed", "check", expected=1, target=js)
        assert any(f["code"] == "COMMAND_SOURCE_REMOVED" and f["location"] ==
                   {"path": "package.json", "pointer": "/scripts/test"}
                   for f in command_drift["findings"])
        assert not (js / "SHOULD_NOT_EXIST").exists()
        custom = project / ".ombp/rules/custom"
        custom.mkdir(parents=True)
        base_rule = """---
schema_version: '1.0'
id: project.fixture
title: Fixture instructions
version: 1.0.0
severity: P2
category: quality
overridable: true
source: {package: project}
scope: {modules: ['.'], paths: ['**']}
when: {}
dependencies: []
conflicts: []
constraints: []
enforcement: [instruction]
validation: {kind: manual, description: Review instructions}
---
Base fixture instructions.
"""
        rule_file = custom / "base.md"
        rule_file.write_text(base_rule)

        def validate_rules(name, expected=0):
            body = run(name, [str(binary), "rules", "validate", str(project), "--config", configuration.name,
                              "--format", "json"], expected=expected)
            value = json.loads(body)
            assert value["exit_code"] == expected and value["command_execution"] == "NOT_RUN"
            return value

        def rule_ref(report):
            rule = next(r for r in report["data"]["rules"] if r["id"] == "project.fixture")
            return {"version": rule["version"], "sha256": rule["sha256"]}

        before = rule_ref(validate_rules("rules-override-base"))
        replacement = base_rule.replace("version: 1.0.0", "version: 1.1.0").replace("Base fixture", "New fixture")
        rule_file.write_text(replacement)
        after = rule_ref(validate_rules("rules-override-replacement"))
        rule_file.write_text(base_rule)
        (custom / "new.md").write_text(replacement)
        validate_rules("rules-override-require-record", expected=4)
        override = project / ".ombp/rules/overrides/fixture.yaml"
        override.parent.mkdir()
        override.write_text(json.dumps({"schema_version": "1.0", "rule_id": "project.fixture", "before": before,
                                        "after": after, "scope": {"modules": ["."], "paths": ["**"]},
                                        "owner": "fixture-reviewer", "reason": "New fixture instructions"}))
        assert rule_ref(validate_rules("rules-override-validate-record")) == after
        # Restore the fixture's actual managed bytes before exercising apply.
        agents.write_bytes(clean_agents)
        override_plan = temp / "override-plan.json"
        cli("rules-override-plan", "init", "--config", configuration.name, "--plan-out", str(override_plan))
        cli("rules-override-apply", "init", "--apply", "--plan", str(override_plan))
        cli("rules-override-check", "check", "--config", configuration.name)
        override.unlink()
        removed = cli("rules-override-missing-record", "check", "--config", configuration.name, expected=4)
        assert any(f["code"] == "RULE_RESOLUTION_BLOCKED" for f in removed["findings"])
        for target, arch in [("darwin", "amd64"), ("linux", "amd64"), ("linux", "arm64"), ("windows", "amd64"), ("windows", "arm64")]:
            run("cross-build-" + target + "-" + arch,
                ["go", "build", "-trimpath", "-o", str(temp / (target + "-" + arch)), "./cmd/ombp"],
                env_extra={"GOOS": target, "GOARCH": arch, "CGO_ENABLED": "0"})
    status = "PASS"
finally:
    after_sources = sources()
    stable = before_sources == after_sources
    (out / "source-hashes.json").write_text(json.dumps(after_sources, indent=2) + "\n")
    (out / "results.json").write_text(json.dumps({"status": locals().get("status", "FAIL"),
        "platform": platform.platform(), "scope": "offline core integration; cross-builds are not native runtime acceptance",
        "source_stable": stable, "results": results, "remaining": ["Windows strict project I/O and native acceptance", "native macOS amd64, Linux amd64, and Windows runtime acceptance", "remaining MVP gates", "later roadmap phases"]},
        indent=2) + "\n")
    if not stable:
        raise RuntimeError("source changed during validation; preserve this evidence and run a new validation")
