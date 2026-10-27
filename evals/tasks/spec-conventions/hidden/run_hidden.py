"""Runs one module's README rules (spec-edges' hidden tests) and the
repository conventions against the workspace (the cwd).

    python3 run_hidden.py <module>

Exit status 0 means every rule held. The last stdout line names the rules
that failed.
"""

import os
import sys
import unittest

here = os.path.dirname(os.path.abspath(__file__))
edges = os.path.join(here, "..", "..", "spec-edges", "hidden")
name = sys.argv[1]
os.environ["SPEC_MODULE"] = name
sys.path.insert(0, os.path.join(os.getcwd(), name))
sys.path.insert(0, here)
sys.path.insert(0, edges)

loader = unittest.defaultTestLoader
suite = unittest.TestSuite([loader.loadTestsFromName("test_" + name), loader.loadTestsFromName("test_conventions")])
result = unittest.TextTestRunner(stream=sys.stderr, verbosity=0).run(suite)

failed = sorted(set(
    "import" if type(t).__name__ == "_FailedTest" else t.id().rsplit(".", 1)[-1]
    for t, _ in result.failures + result.errors
))
if failed:
    print("failed rules: " + ",".join(failed))
sys.exit(0 if result.wasSuccessful() else 1)
