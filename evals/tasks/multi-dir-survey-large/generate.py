"""Writes services/<name>/ trees into the workspace given as argv[1].

Twelve services, each stating its serving port and owning team in a few
files, next to stale ports, metrics ports and earlier owners, inside about
fifty kilobytes of unrelated documentation. Reading a whole service is
cheap; reading all twelve in one context is not, which is the point.
The statements follow multi-dir-survey; only the scale differs.
Deterministic.
"""

import sys
from pathlib import Path


class Rng:
    def __init__(self, seed: int):
        self.s = seed

    def next(self) -> int:
        self.s = (self.s * 6364136223846793005 + 1442695040888963407) % (1 << 64)
        return self.s >> 33

    def randint(self, lo: int, hi: int) -> int:
        return lo + self.next() % (hi - lo + 1)

    def choice(self, seq):
        return seq[self.next() % len(seq)]


# name, serving port, owning team. Teams and former owners are disjoint from
# the words the filler is built from, so a team name is only ever evidence.
SERVICES = (
    ("catalog", 9112, "discovery"),
    ("checkout", 9237, "payments"),
    ("ledger", 9341, "treasury"),
    ("media", 9458, "imaging"),
    ("gateway", 9503, "edge"),
    ("inventory", 9629, "logistics"),
    ("scheduler", 9714, "orchestration"),
    ("reports", 9826, "analytics"),
    ("profile", 9932, "identity"),
    ("ingest", 9047, "pipeline"),
    ("mailer", 9163, "messaging"),
    ("audit", 9279, "compliance"),
)
FORMER_OWNERS = ("platform", "infra", "devex", "legacy", "tools")
FILLER_FILES_PER_SERVICE = 20

SUBJECTS = ["The scheduler", "A replica", "The request path", "Each shard", "The retry loop", "An operator",
            "The cache layer", "A deploy", "The health probe", "The queue consumer", "A rollout", "The index"]
VERBS = ["drains", "reloads", "verifies", "rotates", "compacts", "replays", "throttles", "snapshots",
         "validates", "batches", "warms", "evicts"]
OBJECTS = ["its pending work", "the local state", "stale entries", "the configuration", "connection pools",
           "the write-ahead log", "idle sessions", "expired tokens", "the dashboard counters", "temporary files",
           "outbound requests", "the lookup table"]
TAILS = ["before the next window opens.", "once the previous step has settled.", "when the load is low.",
         "and records the outcome in the audit trail.", "without blocking readers.",
         "and reports progress every few minutes."]
ASIDES = [
    "Example only: `curl localhost:8080/health` works on a developer machine.",
    "The database in the examples listens on 5432.",
    "The cache in the examples listens on 6379.",
    "Dashboards refresh every 30 seconds and keep 14 days of history.",
    "Ask in the docs channel if a page here looks out of date.",
    "The handbook describes who may approve a rollback.",
    "Version 2 of this page replaced the diagrams with prose.",
]


def paragraph(rng: Rng) -> str:
    sentences = []
    for _ in range(rng.randint(4, 7)):
        sentences.append(f"{rng.choice(SUBJECTS)} {rng.choice(VERBS)} {rng.choice(OBJECTS)} {rng.choice(TAILS)}")
    sentences.append(rng.choice(ASIDES))
    return " ".join(sentences)


def filler(name: str, seed: int) -> dict:
    rng = Rng(seed)
    files = {}
    for i in range(FILLER_FILES_PER_SERVICE):
        body = [f"# {name} notes {i + 1:02d}", ""]
        for _ in range(rng.randint(4, 6)):
            body += [paragraph(rng), ""]
        files[f"docs/notes/{i + 1:02d}.md"] = "\n".join(body)
    files["scripts/lint.sh"] = "#!/bin/sh\nset -e\necho lint ok\n"
    files["tests/README.md"] = "Integration tests run against a local stack.\n"
    files[".editorconfig"] = "root = true\n[*]\nindent_style = space\n"
    return files


def statements(index: int, name: str, port: int, team: str, former: str) -> dict:
    stale, metrics = port - 100, port + 1000
    style = index % 4
    if style == 0:
        return {
            "README.md": f"# {name}\n\nServes the {name} API.\n",
            "OWNERS": f"team: {team}\nescalation: {team}-oncall\n",
            "deploy/values.yaml": f"replicas: 3\nport: {port}\nmetrics_port: {metrics}\n",
            "CHANGELOG.md": f"## 2.0\n- Moved the service off port {stale}.\n## 1.4\n- Ownership moved from team {former}.\n",
        }
    if style == 1:
        return {
            "README.md": f"# {name}\n\nServes the {name} API.\n",
            "docs/ops.md": f"# Operations\n\nOwning team: {team}\nPrevious owner: {former}-tools (until 2025)\n",
            "config/server.toml": f"[server]\nlisten_port = {port}\n\n[metrics]\nport = {metrics}\n",
            "CHANGELOG.md": f"## 3.1\n- listen_port changed from {stale}.\n",
        }
    if style == 2:
        return {
            "README.md": f"# {name}\n\nMaintained by the {team} team.\nOriginally prototyped by the {former} team.\n",
            "Dockerfile": f"FROM scratch\nCOPY {name} /{name}\nEXPOSE {port}\nEXPOSE {metrics}\nENTRYPOINT [\"/{name}\"]\n",
            ".env": f"PORT={port}\nMETRICS_PORT={metrics}\n",
            "CHANGELOG.md": f"## 0.9\n- Serving port was {stale} before this release.\n",
        }
    return {
        "README.md": f"# {name}\n\nServes the {name} API.\n",
        "CODEOWNERS": f"* @org/{team}\n/docs/ @org/tech-writers\n",
        "src/main.go": f"package main\n\nconst listenPort = {port}\nconst metricsPort = {metrics}\n\nfunc main() {{}}\n",
        "CHANGELOG.md": f"## 1.2\n- Dropped the legacy {stale + 2000} listener.\n",
    }


def main(out: Path) -> None:
    for i, (name, port, team) in enumerate(SERVICES):
        files = filler(name, 1000 + i)
        files.update(statements(i, name, port, team, FORMER_OWNERS[i % len(FORMER_OWNERS)]))
        root = out / "services" / name
        for rel, body in files.items():
            path = root / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(body)


if __name__ == "__main__":
    main(Path(sys.argv[1]))
