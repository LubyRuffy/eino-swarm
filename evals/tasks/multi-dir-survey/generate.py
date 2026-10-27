"""Writes services/<name>/ trees into the workspace given as argv[1].

Each service states its serving port and owning team in a different kind of
file, next to stale ports, metrics ports and earlier owners. Deterministic.
"""

import sys
from pathlib import Path

FILLER = [
    ("docs/architecture.md", "# Architecture\n\nRequests pass through the gateway, then this service, then storage.\n"),
    ("docs/runbook.md", "# Runbook\n\n1. Check the dashboard.\n2. Restart one replica at a time.\n3. Page the owners if errors persist.\n"),
    ("docs/glossary.md", "# Glossary\n\n- replica: one running copy\n- shard: a slice of the data\n"),
    ("scripts/lint.sh", "#!/bin/sh\nset -e\necho lint ok\n"),
    ("tests/README.md", "Integration tests run against a local stack.\n"),
    (".editorconfig", "root = true\n[*]\nindent_style = space\n"),
]


def service_files(name: str) -> dict:
    if name == "auth":
        return {
            "README.md": "# auth\n\nIssues and verifies session tokens.\n",
            "OWNERS": "team: identity\nescalation: identity-oncall\n",
            "deploy/values.yaml": "replicas: 3\nport: 9101\nmetrics_port: 10101\n",
            "CHANGELOG.md": "## 2.0\n- Moved the service off port 8080.\n## 1.4\n- Ownership moved from team platform.\n",
        }
    if name == "billing":
        return {
            "README.md": "# billing\n\nInvoices and payment retries.\n",
            "docs/ops.md": "# Operations\n\nOwning team: payments\nPrevious owner: finance-tools (until 2025)\n",
            "config/server.toml": "[server]\nlisten_port = 9204\n\n[metrics]\nport = 10204\n",
            "CHANGELOG.md": "## 3.1\n- listen_port changed from 9200.\n",
        }
    if name == "search":
        return {
            "README.md": "# search\n\nMaintained by the discovery team.\nOriginally prototyped by the platform team.\n",
            "Dockerfile": "FROM scratch\nCOPY search /search\nEXPOSE 9310\nEXPOSE 10310\nENTRYPOINT [\"/search\"]\n",
            ".env": "PORT=9310\nMETRICS_PORT=10310\n",
            "CHANGELOG.md": "## 0.9\n- Serving port was 9300 before this release.\n",
        }
    if name == "notify":
        return {
            "README.md": "# notify\n\nSends email and push notifications.\n",
            "CODEOWNERS": "* @org/messaging\n/docs/ @org/tech-writers\n",
            "src/main.go": "package main\n\nconst listenPort = 9420\nconst metricsPort = 10420\n\nfunc main() {}\n",
            "CHANGELOG.md": "## 1.2\n- Dropped the legacy 7420 listener.\n",
        }
    raise ValueError(name)


SERVICES = ("auth", "billing", "search", "notify")


def main(out: Path) -> None:
    for name in SERVICES:
        root = out / "services" / name
        files = dict(FILLER)
        files.update(service_files(name))
        for rel, body in files.items():
            path = root / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(body)


if __name__ == "__main__":
    main(Path(sys.argv[1]))
