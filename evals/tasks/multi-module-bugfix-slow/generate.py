"""The multi-module-bugfix packages with a test suite that takes SLOW_S
seconds to start, so every run of a package's tests costs real wall time.
The sources, defects and tests are the sibling task's.
"""

import importlib.util
import sys
from pathlib import Path

SLOW_S = 20

_spec = importlib.util.spec_from_file_location(
    "multi_module_bugfix_generate",
    Path(__file__).resolve().parent.parent / "multi-module-bugfix" / "generate.py",
)
base = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(base)


def main(out: Path) -> None:
    base.main(out, slow_s=SLOW_S)


if __name__ == "__main__":
    main(Path(sys.argv[1]))
