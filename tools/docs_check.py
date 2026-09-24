#!/usr/bin/env python3
"""Check feature/contract identity, evidence, and required documentation."""

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REQUIRED = (
    "README.md", "ARCHITECTURE.md", "AGENTS.md", "CHANGELOG.md",
    "docs/API.md", "docs/CLI.md", "docs/CONFIG.md", "docs/DATA_MODEL.md",
    "docs/TESTING.md", "docs/FEATURES.md", "docs/CONTRACTS.md",
)


def fail(message):
    raise SystemExit(message)


for name in REQUIRED:
    if not (ROOT / name).is_file():
        fail(f"required document missing: {name}")

features_text = (ROOT / "docs/FEATURES.md").read_text()
contracts_text = (ROOT / "docs/CONTRACTS.md").read_text()
feature_ids = re.findall(r"^### (F-\d+)\b", features_text, re.M)
contract_ids = re.findall(r"^## (C-\d+)\b", contracts_text, re.M)
if len(feature_ids) != len(set(feature_ids)) or len(contract_ids) != len(set(contract_ids)):
    fail("duplicate feature or contract definition")
if not feature_ids or not contract_ids:
    fail("feature and contract definitions are required")

tree_ids = re.findall(r"^\s*- `(F-\d+)`", features_text, re.M)
if len(tree_ids) != len(set(tree_ids)):
    fail("duplicate feature tree ID")
if set(feature_ids) != {item for item in tree_ids if item in feature_ids}:
    fail("feature leaf definitions and tree entries differ")

for feature_id in feature_ids:
    section = features_text.split(f"### {feature_id} ", 1)[1].split("\n### ", 1)[0]
    if "入口：" not in section or "实现证据：" not in section:
        fail(f"{feature_id} lacks entry or evidence")
    refs = re.findall(r"`(C-\d+)`", section)
    if not refs or any(ref not in contract_ids for ref in refs):
        fail(f"{feature_id} has missing contract references")

for contract_id in contract_ids:
    section = contracts_text.split(f"## {contract_id} ", 1)[1].split("\n## ", 1)[0]
    refs = re.findall(r"`(F-\d+)`", section)
    if not refs or any(ref not in feature_ids for ref in refs):
        fail(f"{contract_id} has missing feature references")
    if "实现 `" not in section:
        fail(f"{contract_id} lacks implementation evidence")

for name in ("docs/FEATURES.md", "docs/CONTRACTS.md"):
    path = ROOT / name
    body = path.read_text()
    for target in re.findall(r"\[[^]]+\]\(([^)]+)\)", body):
        if target.startswith(("http:", "https:", "#")):
            continue
        if not (path.parent / target.split("#", 1)[0]).exists():
            fail(f"broken Markdown link in {name}: {target}")
    for source in re.findall(r"`([^`]+(?:/[^`]+)+)`", body):
        if source.startswith(("/api", "zwai-", "F-", "C-")):
            continue
        if source.startswith("<") or " " in source or ":" in source:
            continue
        if not (ROOT / source).exists():
            fail(f"missing evidence path in {name}: {source}")

print(f"docs-check: {len(feature_ids)} features, {len(contract_ids)} contracts, paths and links OK")
