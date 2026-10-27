"""Writes sales.csv into the workspace given as argv[1]. Deterministic."""

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


def rows(seed: int = 20260301, n: int = 240):
    rng = Rng(seed)
    regions = ["north", "south", "east", "west"]
    products = {"P-100": 1250, "P-200": 899, "P-300": 4500, "P-400": 199, "P-500": 2375}
    names = list(products)
    for i in range(n):
        day = 1 + i % 28
        product = rng.choice(names)
        yield {
            "date": f"2026-02-{day:02d}",
            "region": rng.choice(regions),
            "product": product,
            "units": rng.randint(1, 40),
            "unit_price": f"{products[product] / 100:.2f}",
        }


def main(out: Path) -> None:
    lines = ["date,region,product,units,unit_price"]
    for r in rows():
        lines.append(f"{r['date']},{r['region']},{r['product']},{r['units']},{r['unit_price']}")
    (out / "sales.csv").write_text("\n".join(lines) + "\n")


if __name__ == "__main__":
    main(Path(sys.argv[1]))
