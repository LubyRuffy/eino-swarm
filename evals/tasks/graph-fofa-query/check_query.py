"""Grades query.txt in the workspace against the generator's truth: the query,
run by the workspace's own simulator, must select exactly the target hosts."""

import importlib.util
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main():
    gen = load("gen", HERE / "generate.py")
    sim = load("sim", HERE / "fixture" / "fofa_sim.py")
    rows = gen.records()
    # The product delivers files under the task results directory. A copy at
    # the workspace root is accepted too; the results copy wins when both exist.
    found = sorted(Path(".").glob(".zwai-tasks/**/results/query.txt"))
    direct = Path("query.txt")
    if direct.is_file():
        found.append(direct)
    if not found:
        print("query.txt was not written")
        return 1
    query = found[0].read_text().strip()
    try:
        got = set(sim.search(query, rows))
    except ValueError as exc:
        print(f"query does not run: {exc}")
        return 1
    want = {r["id"] for r in rows if r["target"]}
    if got != want:
        print(f"selected {len(got)} hosts: missing {len(want - got)}, extra {len(got - want)}")
        return 1
    print("exact")
    return 0


if __name__ == "__main__":
    sys.exit(main())
