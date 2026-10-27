"""The csv-aggregate workspace, unchanged: only the replayed steer differs."""

import importlib.util
import sys
from pathlib import Path

_spec = importlib.util.spec_from_file_location(
    "csv_aggregate_generate",
    Path(__file__).resolve().parent.parent / "csv-aggregate" / "generate.py",
)
base = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(base)

if __name__ == "__main__":
    base.main(Path(sys.argv[1]))
