"""Runs one module's hidden rule tests against the workspace (the cwd).

    python3 run_hidden.py <module>

Exit status 0 means every rule held. The last stdout line names the rules
that failed, which is what a grader's one-line detail needs.
"""

import os
import sys
import unittest

here = os.path.dirname(os.path.abspath(__file__))
name = sys.argv[1]
sys.path.insert(0, os.path.join(os.getcwd(), name))
sys.path.insert(0, here)

suite = unittest.defaultTestLoader.loadTestsFromName("test_" + name)
result = unittest.TextTestRunner(stream=sys.stderr, verbosity=0).run(suite)

# A rule module that cannot import (the agent never wrote the file) reaches
# the runner as a synthetic failed test, not as an exception. A failed
# subTest reports its parent rule, so each rule is named once.
failed = sorted(set(
    "import" if type(t).__name__ == "_FailedTest" else getattr(t, "test_case", t)._testMethodName
    for t, _ in result.failures + result.errors
))
if failed:
    print("failed rules: " + ",".join(failed))
sys.exit(0 if result.wasSuccessful() else 1)
