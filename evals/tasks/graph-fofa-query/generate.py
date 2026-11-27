"""Writes the corpus, the article and the seeded draft into the workspace
given as argv[1]. Deterministic. The grader imports records() for the truth.
"""

import json
import sys
from pathlib import Path

TARGET_TITLE = "Aurora Gateway - Sign in"
TARGET_HEADER = "aurora-gw/"


def records():
    """Targets expose the product in two different places; decoys share single
    words or merely write about it, so neither the draft nor an easy rewrite
    selects exactly the targets."""
    out = []
    n = 0

    def add(target, **fields):
        nonlocal n
        n += 1
        out.append({"id": f"host-{n:02d}", "target": target, "title": "", "body": "", "header": "", "port": 443, **fields})

    for i in range(7):
        add(True, title=TARGET_TITLE, body="<form>login</form>", header="Server: nginx")
    for i in range(5):
        add(True, title="Console", body="<div>console</div>", header=f"Server: {TARGET_HEADER}2.{i}")
    for i in range(6):
        add(False, title="Aurora Borealis Tours", body="northern lights trips", header="Server: nginx")
    for i in range(5):
        add(False, title="504 Gateway Timeout", body="upstream timed out", header="Server: nginx")
    for i in range(6):
        add(False, title="Review blog", body="we tried Aurora Gateway last week and liked it", header="Server: apache")
    for i in range(6):
        add(False, title="Console", body="<div>console</div>", header="Server: aurora-cdn/1.0")
    return out


def main(out: Path) -> None:
    rows = [{k: v for k, v in r.items() if k != "target"} for r in records()]
    (out / "corpus.jsonl").write_text("".join(json.dumps(r) + "\n" for r in rows))
    (out / "draft_query.txt").write_text('title="Aurora"\n')
    (out / "article.txt").write_text(
        "Researchers describe the Aurora Gateway admin product. Its sign-in page is titled "
        "\"Aurora Gateway - Sign in\"; installs that hide the page can still be told apart by the "
        "`Server` response header, which starts with `aurora-gw/` followed by a version. "
        "Unrelated sites also use the word Aurora, and blogs mention the product in passing.\n")


if __name__ == "__main__":
    main(Path(sys.argv[1]))
