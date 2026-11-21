"""Verify Codex's local prompt assembly against generated OMBP instructions."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / "docs/05-testing/evidence"
SANDBOX = Path("/usr/bin/sandbox-exec")


def digest(data):
    return hashlib.sha256(data).hexdigest()


def source_hashes():
    patterns = ["go.*", "cmd/**/*.go", "internal/**/*.go", "internal/rules/builtin/*.md"]
    files = {path for pattern in patterns for path in ROOT.glob(pattern)}
    files.add(Path(__file__).resolve())
    return {p.relative_to(ROOT).as_posix(): digest(p.read_bytes()) for p in sorted(files)}


def sandbox_policy(fixture, codex, node):
    """Admit the CLI installation and isolated fixture while hiding user files."""
    user_home = Path.home().resolve()

    def filters(kind, paths):
        return " ".join(f"({kind} {json.dumps(str(p))})" for p in paths)

    executables = {codex, codex.resolve()}
    distributions = {fixture}
    if node:
        # npm's launcher locates its native binary next to the JS package.
        executables.update((node, node.resolve()))
        distributions.add(codex.resolve().parent.parent)
    parents = sorted({p for exe in executables for p in exe.parents
                      if p.is_relative_to(user_home)})
    caps = filters("subpath", [user_home, "/etc/codex", "/private/etc/codex"])
    allowed = filters("literal", sorted(executables)) + " " + filters("subpath", sorted(distributions))
    lines = ["(version 1)", "(allow default)", "(deny network*)",
             f"(deny file-read* {caps})", f"(allow file-read* {allowed})",
             "(deny file-write*)",
             f'(allow file-write* {filters("subpath", [fixture])} (literal "/dev/null"))']
    if parents:
        lines.insert(4, f'(allow file-read-metadata {filters("literal", parents)})')
    return "\n".join(lines) + "\n"


def prompt_texts(response):
    items = json.loads(response)
    return [part["text"] for item in items if item.get("type") == "message"
            for part in item.get("content", []) if part.get("type") == "input_text"]


def run_suite(out, codex, node, go):
    before = source_hashes()
    evidence = {"status": "FAIL", "platform": platform.platform(), "checks": [],
                "requirements": ["FR-004", "AC-02", "AC-09"],
                "scope": "Native Codex debug prompt assembly on an isolated Git project",
                "not_run": ["model turns and instruction adherence", "other Codex versions and platforms"]}
    git_env = {"PATH": "/usr/bin:/bin", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1"}
    go_env = {"PATH": os.environ["PATH"], "HOME": str(Path.home()), "GOENV": "off", "GOPROXY": "off",
              "GOSUMDB": "off", "GOTOOLCHAIN": "local", "GOWORK": "off"}
    def run(name, args, *, cwd, env, verify=None, expected=0):
        started = datetime.now(timezone.utc).isoformat()
        record = {"name": name, "command": [str(arg) for arg in args], "cwd": str(cwd),
                  "started_at": started, "expected_exit_code": expected, "status": "FAIL"}
        evidence["checks"].append(record)
        try:
            result = subprocess.run(args, cwd=cwd, env=env, capture_output=True, timeout=45)
        except (OSError, subprocess.TimeoutExpired) as err:
            record["error"] = type(err).__name__
            raise
        record.update(exit_code=result.returncode, stdout_bytes=len(result.stdout),
                      stdout_sha256=digest(result.stdout), stderr_bytes=len(result.stderr))
        if result.returncode != expected:
            raise RuntimeError(f"{name} exited {result.returncode}; expected {expected}")
        if verify:
            record.update(verify(result.stdout))
        record["status"] = "PASS"
        print(name, "PASS", flush=True)
        return result.stdout

    try:
        evidence["source_head"] = run("source-head", ["/usr/bin/git", "rev-parse", "HEAD"],
                                     cwd=ROOT, env=git_env).decode().strip()
        with tempfile.TemporaryDirectory(prefix="ombp-codex-loading-") as tmp:
            temp = Path(tmp).resolve()
            project = temp / "project"
            codex_home = temp / "codex-home"
            binary = temp / "ombp"
            profile = temp / "no-network.sb"
            plan = temp / "reviewed.json"
            ancestor = project / "services"
            module = ancestor / "backend"
            sibling = project / "other"
            for directory in (codex_home, module, sibling):
                directory.mkdir(parents=True)
            codex_env = dict(git_env, CODEX_HOME=str(codex_home), TMPDIR=str(temp), NO_COLOR="1")
            if node:
                codex_env["PATH"] = f"{node.parent}:{codex_env['PATH']}"
            policy = sandbox_policy(temp, codex, node)
            profile.write_text(policy)
            evidence["sandbox"] = policy.replace(str(temp), "$TEMP").replace(str(Path.home()), "$USER")
            native = [SANDBOX, "-f", profile, codex]
            evidence["codex_version"] = run("codex-version", [*native, "--version"],
                                               cwd=project, env=codex_env).decode().strip()
            run("build-ombp", [go, "build", "-trimpath", "-o", binary, "./cmd/ombp"], cwd=ROOT, env=go_env)
            evidence["ombp_version"] = json.loads(run("ombp-version", [binary, "version", "--format", "json"],
                                                     cwd=project, env=git_env))["data"]
            run("init-fixture-git", ["/usr/bin/git", "init", "-q", "--template="], cwd=project, env=git_env)
            for directory, name in [(project, "example.test/fixture"), (module, "example.test/backend"),
                                    (sibling, "example.test/other")]:
                (directory / "go.mod").write_text(f"module {name}\n\ngo 1.25.0\n")
            ancestor_text = "OMBP fixture ancestor instructions for services.\n"
            override_text = "OMBP fixture higher-priority override for backend.\n"
            (ancestor / "AGENTS.md").write_text(ancestor_text)
            run("review-plan", [binary, "init", project, "--plan-out", plan, "--format", "json"],
                cwd=project, env=git_env)
            run("apply-reviewed-plan", [binary, "init", project, "--apply", "--plan", plan, "--format", "json"],
                cwd=project, env=git_env)
            instructions = {name: (project / name).read_bytes() for name in
                            ("AGENTS.md", "services/AGENTS.md", "services/backend/AGENTS.md", "other/AGENTS.md")}
            instructions["services/backend/AGENTS.override.md"] = override_text.encode()
            evidence["instructions"] = {name: {"sha256": digest(body), "bytes": len(body)}
                                        for name, body in instructions.items()}

            def inspect(name, cwd, expected):
                def verify(stdout):
                    texts = prompt_texts(stdout)
                    assembled = "\n".join(texts)
                    visible = [(assembled.index(body.decode().strip()), path)
                               for path, body in instructions.items() if body.decode().strip() in assembled]
                    observed = [path for _, path in sorted(visible)]
                    if observed != expected:
                        raise RuntimeError(f"{name}: expected {expected}, observed {observed}")
                    return {"expected_instructions": expected, "observed_instructions": observed,
                            "match": "complete trimmed text in model-visible input items, in order"}

                run(name, [*native, "debug", "prompt-input", "--disable", "hooks", "--disable", "plugins",
                           "OMBP local instruction loading test."], cwd=cwd, env=codex_env, verify=verify)

            inspect("load-root", project, ["AGENTS.md"])
            inspect("load-module", module, ["AGENTS.md", "services/AGENTS.md", "services/backend/AGENTS.md"])
            inspect("load-sibling", sibling, ["AGENTS.md", "other/AGENTS.md"])
            (module / "AGENTS.override.md").write_text(override_text)
            inspect("load-module-override", module,
                    ["AGENTS.md", "services/AGENTS.md", "services/backend/AGENTS.override.md"])
            if any((project / name).read_bytes() != body for name, body in instructions.items()):
                raise RuntimeError("fixture instructions changed during Codex inspection")
        evidence["status"] = "PASS"
    except Exception as err:
        evidence["error"] = str(err)
        raise
    finally:
        after = source_hashes()
        evidence["source_stable"] = before == after
        evidence["source_hashes_before"] = before
        evidence["source_hashes_after"] = after
        if not evidence["source_stable"]:
            evidence["status"] = "FAIL"
        (out / "results.json").write_text(json.dumps(evidence, indent=2) + "\n")
    if not evidence["source_stable"]:
        raise RuntimeError("OMBP sources changed during validation")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True, help="new directory under docs/05-testing/evidence")
    args = parser.parse_args()
    if platform.system() != "Darwin" or not SANDBOX.is_file():
        parser.error("offline native suite requires macOS sandbox-exec")
    codex = shutil.which("codex")
    go = shutil.which("go")
    if not codex or not go:
        parser.error("install Codex and Go before running this suite")
    codex = Path(codex).absolute()
    node = shutil.which("node") if codex.resolve().suffix == ".js" else None
    if codex.resolve().suffix == ".js" and not node:
        parser.error("the Codex npm launcher requires Node.js")
    out = args.out.resolve()
    if out == EVIDENCE or not out.is_relative_to(EVIDENCE):
        parser.error("--out must be a new subdirectory under docs/05-testing/evidence")
    out.mkdir(parents=True, exist_ok=False)
    run_suite(out, codex, Path(node).absolute() if node else None, go)


if __name__ == "__main__":
    main()
