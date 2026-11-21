"""Validate project task dependencies and documentation without changing state."""
from pathlib import Path
import json
import re

ROOT = Path(__file__).resolve().parents[1]
dag = json.loads((ROOT / "docs/implementation/task-dag.json").read_text())
nodes = {node["id"]: node for node in dag["nodes"]}
assert len(nodes) == len(dag["nodes"]) == 15
done, active = set(), set()


def visit(key):
    assert key in nodes, f"missing dependency: {key}"
    assert key not in active, f"cycle: {key}"
    if key in done:
        return
    active.add(key)
    for dep in nodes[key]["depends_on"]:
        visit(dep)
    task = ROOT / nodes[key]["task_path"]
    meta = json.loads((task / "task.json").read_text())
    assert meta["parent"] == Path(dag["master"]).name
    for name in ("prd.md", "design.md", "implement.md"):
        assert (task / name).stat().st_size > 80
    for name in ("implement.jsonl", "check.jsonl"):
        entries = [json.loads(line) for line in (task / name).read_text().splitlines() if line.strip()]
        assert entries, (key, name)
        for entry in entries:
            assert (ROOT / entry["file"]).is_file(), entry
    for path in nodes[key]["evidence"]:
        assert (ROOT / path).is_file(), (key, path)
    if nodes[key]["status"] == "IMPLEMENTED":
        assert nodes[key]["evidence"], f"no evidence: {key}"
        for path in nodes[key]["deliverables"]:
            assert (ROOT / path).exists(), (key, path)
    active.remove(key)
    done.add(key)


for node in nodes:
    visit(node)
links = 0
for document in [ROOT / "AGENTS.md", *list((ROOT / "docs").rglob("*.md"))]:
    text = document.read_text()
    assert len(re.findall(r"^```", text, re.M)) % 2 == 0, document
    for target in re.findall(r"\[[^\]]*\]\(([^)]+)\)", text):
        if "://" in target or target.startswith("#"):
            continue
        assert (document.parent / target.split("#")[0]).exists(), (document, target)
        links += 1
print(f"PASS: {len(nodes)} task contracts, acyclic dependencies, {links} local links")
