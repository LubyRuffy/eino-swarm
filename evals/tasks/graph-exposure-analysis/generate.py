"""Writes the exposure export into the workspace given as argv[1]. Deterministic.

Addresses come from the documentation ranges (RFC 5737), names from .test, so
nothing here can reach a real host.
"""

import json
import sys
from pathlib import Path

CLOUDS = ["Cloud Alpha", "Cloud Beta", "Cloud Gamma", "self-hosted"]
COUNTRIES = ["US", "DE", "SG", "BR", "JP"]
PRODUCTS = ["Gateway-X", "Panel-Y", "Store-Z", "Mail-Q"]


def records():
    """One row per exposed service; an address may expose several services."""
    out = []
    for i in range(30):
        ip = f"198.51.100.{i + 1}" if i < 18 else f"203.0.113.{i - 17}"
        for port in ([443] if i % 3 else [443, 8443]):
            out.append({
                "ip": ip,
                "port": port,
                "country": COUNTRIES[(i * i + i // 4) % len(COUNTRIES)],
                "cloud": CLOUDS[i % len(CLOUDS)],
                "product": PRODUCTS[(i * 7 + port) % len(PRODUCTS)],
                "host": f"svc{i % 11}.example-corp.test",
            })
    return out


def main(out: Path) -> None:
    rows = records()
    (out / "exposure.jsonl").write_text("".join(json.dumps(r) + "\n" for r in rows))
    (out / "company.md").write_text(
        "# Example Corp\n\nRegistered domains: example-corp.test. "
        "The export `exposure.jsonl` lists every service seen on the company's address space, one row per service.\n")


if __name__ == "__main__":
    main(Path(sys.argv[1]))
