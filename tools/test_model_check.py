"""Tests for tools/model_check.py.

The case table is the acceptance record of the lifecycle model: if a case
named an invariant or switch the module no longer declares, TLC would fail on
the config and the record would silently stop meaning anything. The TLC run
itself is exercised only when tla2tools.jar and Java are already present; it
never downloads.
"""

from __future__ import annotations

import os
import re
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import model_check as mc  # noqa: E402

SPEC_TEXT = mc.SPEC.read_text()


def declared_constants() -> set[str]:
    block = re.search(r"^CONSTANTS\n(.*?)\n\n", SPEC_TEXT, re.S | re.M).group(1)
    return set(re.findall(r"^\s+(\w+)", block, re.M))


def defined_operators() -> set[str]:
    return set(re.findall(r"^(\w+)(?:\(.*?\))? ==", SPEC_TEXT, re.M))


class CaseTable(unittest.TestCase):
    def test_every_switch_and_invariant_exists_in_the_model(self):
        self.assertEqual(declared_constants(), set(mc.SWITCHES) | {"MaxTurns"})
        ops = defined_operators()
        for case in mc.CASES:
            for inv in case.invariants():
                self.assertIn(inv, ops, case.name)
            self.assertLessEqual(case.switches, set(mc.SWITCHES), case.name)

    def test_every_switch_is_shown_to_break_an_invariant(self):
        # A switch no case turns on is an untested claim about the protocol.
        exercised = set().union(*(c.switches for c in mc.CASES if c.violates))
        self.assertEqual(exercised, set(mc.SWITCHES))

    def test_the_current_design_is_checked_against_every_safety_invariant(self):
        current = [c for c in mc.CASES if not c.switches and not c.witness]
        self.assertEqual(len(current), 1)
        self.assertIsNone(current[0].expected())
        self.assertEqual(current[0].invariants(), mc.SAFETY)

    def test_every_witness_is_checked_alone_on_the_current_design(self):
        witnesses = [c for c in mc.CASES if c.witness]
        self.assertTrue(witnesses)
        defined = {op for op in defined_operators() if op.startswith("Never")}
        self.assertEqual({c.witness for c in witnesses}, defined)
        for c in witnesses:
            self.assertFalse(c.switches, c.name)
            self.assertEqual(c.invariants(), (c.witness,))

    def test_case_names_are_unique_and_historical_cases_name_their_commit(self):
        names = [c.name for c in mc.CASES]
        self.assertEqual(len(names), len(set(names)))
        for c in mc.CASES:
            if c.source.startswith("historical"):
                self.assertRegex(c.source, r"\b[0-9a-f]{7}\b", c.name)


class Config(unittest.TestCase):
    def test_config_sets_every_switch_and_only_the_case_invariants(self):
        case = mc.Case("x", "s", frozenset({"FinishUngated"}), "WorkHasLiveOwner")
        cfg = mc.render_config(case)
        self.assertIn("  FinishUngated = TRUE\n", cfg)
        for name in set(mc.SWITCHES) - {"FinishUngated"}:
            self.assertIn(f"  {name} = FALSE\n", cfg)
        self.assertIn(f"  MaxTurns = {mc.MAX_TURNS}\n", cfg)
        self.assertTrue(cfg.endswith("INVARIANTS " + " ".join(mc.SAFETY) + "\n"))
        witness = mc.render_config(mc.Case("w", "s", witness="NeverParkedCheckpoint"))
        self.assertTrue(witness.endswith("INVARIANTS NeverParkedCheckpoint\n"))


PASS_OUT = """Model checking completed. No error has been found.
852 states generated, 504 distinct states found, 0 states left on queue.
"""
VIOLATION_OUT = """Error: Invariant CheckpointHasOwner is violated.
Error: The behavior up to this point is:
State 1: <Initial predicate>
21 states generated, 19 distinct states found, 3 states left on queue.
"""
PARSE_ERROR_OUT = """Error: TLC threw an unexpected exception.
The exception was a tlc2.tool.ConfigFileException
"""


class Judging(unittest.TestCase):
    def test_parse_output_reads_pass_violation_and_failure(self):
        self.assertEqual(mc.parse_output(PASS_OUT), mc.Outcome(True, None, 504, None))
        self.assertEqual(mc.parse_output(VIOLATION_OUT), mc.Outcome(False, "CheckpointHasOwner", 19, None))
        broken = mc.parse_output(PARSE_ERROR_OUT)
        self.assertEqual(broken.error, "Error: TLC threw an unexpected exception.")
        self.assertEqual(mc.parse_output("").error, "TLC did not finish")

    def test_judge_requires_the_exact_expected_outcome(self):
        current = mc.Case("c", "s")
        bug = mc.Case("b", "s", frozenset({"FinishUngated"}), "WorkHasLiveOwner")
        passed = mc.Outcome(True, None, 1, None)
        self.assertIsNone(mc.judge(current, passed))
        self.assertIn("expected no violation", mc.judge(current, mc.Outcome(False, "StopIsFinal", 1, None)))
        self.assertIsNone(mc.judge(bug, mc.Outcome(False, "WorkHasLiveOwner", 1, None)))
        # Breaking a different invariant is not the recorded finding.
        self.assertIn("reported StopIsFinal", mc.judge(bug, mc.Outcome(False, "StopIsFinal", 1, None)))
        self.assertIn("found no error", mc.judge(bug, passed))
        self.assertEqual(mc.judge(bug, mc.Outcome(False, None, None, "Error: x")), "Error: x")


class Jar(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp)

    def test_explicit_and_environment_paths_must_exist(self):
        jar = self.tmp / "t.jar"
        jar.write_bytes(b"x")
        self.assertEqual(mc.find_jar(str(jar), False), jar)
        with mock.patch.dict(os.environ, {"TLA2TOOLS_JAR": str(jar)}):
            self.assertEqual(mc.find_jar(None, False), jar)
        with self.assertRaises(SystemExit):
            mc.find_jar(str(self.tmp / "missing.jar"), False)

    def test_cache_is_never_fetched_without_consent_and_must_match_the_pin(self):
        cache = self.tmp / "cache" / "tla2tools.jar"
        with mock.patch.object(mc, "CACHE_JAR", cache), mock.patch.dict(os.environ, {}, clear=True), \
                mock.patch.object(mc.urllib.request, "urlopen") as urlopen:
            with self.assertRaisesRegex(SystemExit, "--fetch"):
                mc.find_jar(None, False)
            urlopen.assert_not_called()
            cache.parent.mkdir(parents=True)
            cache.write_bytes(b"tampered")
            with self.assertRaisesRegex(SystemExit, "SHA-256"):
                mc.find_jar(None, False)
            with mock.patch.object(mc, "TLA_SHA256", mc.sha256(cache)):
                self.assertEqual(mc.find_jar(None, False), cache)

    def test_fetch_keeps_only_a_download_that_matches_the_pin(self):
        import io

        cache = self.tmp / "cache" / "tla2tools.jar"
        body = b"jar bytes"
        with mock.patch.object(mc, "CACHE_JAR", cache), mock.patch.dict(os.environ, {}, clear=True), \
                mock.patch.object(mc.urllib.request, "urlopen", side_effect=lambda *a, **k: io.BytesIO(body)):
            with self.assertRaisesRegex(SystemExit, "does not match"):
                mc.find_jar(None, True)
            self.assertFalse(cache.exists())
            self.assertFalse(cache.with_suffix(".part").exists())
            import hashlib

            with mock.patch.object(mc, "TLA_SHA256", hashlib.sha256(body).hexdigest()):
                self.assertEqual(mc.find_jar(None, True), cache)
            self.assertEqual(cache.read_bytes(), body)


def available_jar() -> Path | None:
    if not shutil.which("java"):
        return None
    for candidate in (os.environ.get("TLA2TOOLS_JAR"), str(mc.CACHE_JAR)):
        if candidate and Path(candidate).is_file():
            return Path(candidate)
    return None


@unittest.skipUnless(available_jar(), "tla2tools.jar and java are not installed; run make model-check")
class WithTLC(unittest.TestCase):
    # The current design and the two historical reproductions; the full table
    # runs through `make model-check`.
    def test_current_design_passes_and_history_is_found_again(self):
        jar = available_jar()
        out = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, out)
        by_name = {c.name: c for c in mc.CASES}
        for name in ("current", "pre-d77e8d4", "pre-f341e05"):
            outcome, log = mc.run_case(by_name[name], jar, out)
            self.assertIsNone(mc.judge(by_name[name], outcome), log.read_text()[-2000:])
            self.assertTrue((out / name / "case.cfg").is_file())
            self.assertFalse((out / name / "states").exists())


class Cli(unittest.TestCase):
    def test_list_and_unknown_cases(self):
        with mock.patch("builtins.print") as printed:
            self.assertEqual(mc.main(["--list"]), 0)
        self.assertEqual(printed.call_count, len(mc.CASES))
        with mock.patch("sys.stderr"), self.assertRaises(SystemExit):
            mc.main(["no-such-case"])

    def test_a_case_with_the_wrong_outcome_fails_the_run_and_keeps_its_output(self):
        out = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, out)

        def fake_run(case, jar, out_dir, timeout=600):
            log = out_dir / case.name / "tlc.out"
            log.parent.mkdir(parents=True)
            log.write_text(PASS_OUT)
            return mc.parse_output(PASS_OUT), log

        with mock.patch.object(mc, "find_jar", return_value=Path("jar")), \
                mock.patch.object(mc, "run_case", side_effect=fake_run), \
                mock.patch("builtins.print") as printed:
            self.assertEqual(mc.main(["current", "pre-d77e8d4", "--out", str(out)]), 1)
        text = "\n".join(str(c.args[0]) for c in printed.call_args_list)
        self.assertIn("ok   current", text)
        self.assertIn("FAIL pre-d77e8d4", text)
        self.assertIn("1/2 cases as expected", text)
        self.assertTrue((out / "pre-d77e8d4" / "tlc.out").is_file())


if __name__ == "__main__":
    unittest.main()
