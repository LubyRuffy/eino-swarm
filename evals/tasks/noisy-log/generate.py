"""Writes a 20,000-line app.log into the workspace given as argv[1].

ERROR lines carry one of five codes. WARN lines mention the same codes as
retries, which must not be counted. Deterministic.
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


CODES = ["E101", "E205", "E307", "E412", "E550"]
COMPONENTS = ["api", "worker", "scheduler", "db-pool", "cache"]


def lines(seed: int = 7741, n: int = 20000):
    rng = Rng(seed)
    t = 6 * 3600  # 06:00:00
    for _ in range(n):
        t += rng.randint(0, 3)
        hh, mm, ss = t // 3600, (t // 60) % 60, t % 60
        stamp = f"2026-04-09T{hh:02d}:{mm:02d}:{ss:02d}Z"
        roll = rng.randint(1, 1000)
        comp = rng.choice(COMPONENTS)
        if roll <= 12:
            # Weighted so the codes have clearly different counts.
            code = CODES[min(rng.randint(0, 9) // 2, 4)] if rng.randint(0, 1) else CODES[rng.randint(0, 2)]
            yield f"{stamp} ERROR [{comp}] request failed code={code} id={rng.randint(10000, 99999)}"
        elif roll <= 40:
            code = rng.choice(CODES)
            yield f"{stamp} WARN  [{comp}] retrying after code={code} attempt={rng.randint(1, 3)}"
        elif roll <= 400:
            yield f"{stamp} DEBUG [{comp}] tick latency_ms={rng.randint(1, 900)}"
        else:
            yield f"{stamp} INFO  [{comp}] handled request status=200 bytes={rng.randint(100, 9000)}"


def main(out: Path) -> None:
    (out / "app.log").write_text("\n".join(lines()) + "\n")


if __name__ == "__main__":
    main(Path(sys.argv[1]))
