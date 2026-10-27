"""The spec-conventions workspace, unchanged: only the prompt differs.
The README files, basic tests and hidden rule tests are the sibling task's.
"""

import importlib.util
import sys
from pathlib import Path

_spec = importlib.util.spec_from_file_location(
    "spec_conventions_generate",
    Path(__file__).resolve().parent.parent / "spec-conventions" / "generate.py",
)
base = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(base)


def main(out: Path) -> None:
    base.main(out)


if __name__ == "__main__":
    main(Path(sys.argv[1]))
