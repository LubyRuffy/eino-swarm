"""Writes data/temps.csv into the workspace given as argv[1]. Deterministic."""

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


CITIES = {"oslo": 20, "lisbon": 160, "cairo": 260, "lima": 190, "perth": 230}


def rows(seed: int = 3318):
    rng = Rng(seed)
    for day in range(1, 31):
        for city, base in CITIES.items():
            tenths = base + rng.randint(-45, 45)
            yield f"2026-06-{day:02d}", city, f"{tenths / 10:.1f}"


def main(out: Path) -> None:
    (out / "data").mkdir(parents=True, exist_ok=True)
    lines = ["date,city,temp_c"] + [",".join(r) for r in rows()]
    (out / "data" / "temps.csv").write_text("\n".join(lines) + "\n")


if __name__ == "__main__":
    main(Path(sys.argv[1]))
