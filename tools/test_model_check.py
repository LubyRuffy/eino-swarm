"""Tests for tools/model_check.py.

The case table is the acceptance record of the protocol models: if a case
named an invariant or switch its module no longer declares, TLC would fail on
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


def declared_constants(model: mc.Model) -> set[str]:
    block = re.search(r"^CONSTANTS\n(.*?)\n\n", model.spec.read_text(), re.S | re.M).group(1)
    return set(re.findall(r"^\s+(\w+)", block, re.M))


def defined_operators(model: mc.Model) -> set[str]:
    return set(re.findall(r"^(\w+)(?:\(.*?\))? ==", model.spec.read_text(), re.M))


def cases_of(model: mc.Model) -> list[mc.Case]:
    return [c for c in mc.CASES if c.model == model.name]


class CaseTable(unittest.TestCase):
    def test_every_case_belongs_to_a_model(self):
        self.assertLessEqual({c.model for c in mc.CASES}, set(mc.MODELS))
        for model in mc.MODELS.values():
            self.assertTrue(cases_of(model), model.name)

    def test_every_switch_and_invariant_exists_in_the_model(self):
        for model in mc.MODELS.values():
            self.assertEqual(declared_constants(model),
                             set(model.switches) | {name for name, _ in model.constants}, model.name)
            ops = defined_operators(model)
            for case in cases_of(model):
                for inv in case.invariants():
                    self.assertIn(inv, ops, case.name)
                self.assertLessEqual(case.switches, set(model.switches), case.name)

    def test_every_switch_is_shown_to_break_an_invariant(self):
        # A switch no case turns on is an untested claim about the protocol.
        for model in mc.MODELS.values():
            exercised = set().union(*(c.switches for c in cases_of(model) if c.violates))
            self.assertEqual(exercised, set(model.switches), model.name)

    def test_every_safety_invariant_is_shown_to_catch_something(self):
        for model in mc.MODELS.values():
            caught = {c.violates for c in cases_of(model) if c.violates}
            self.assertEqual(caught, set(model.safety) - {"TypeOK"}, model.name)

    def test_the_current_design_is_checked_against_every_safety_invariant(self):
        for model in mc.MODELS.values():
            current = [c for c in cases_of(model) if not c.switches and not c.witness]
            self.assertEqual(len(current), 1, model.name)
            self.assertIsNone(current[0].expected())
            self.assertEqual(current[0].invariants(), model.safety)

    def test_every_witness_is_checked_alone_on_the_current_design(self):
        for model in mc.MODELS.values():
            witnesses = [c for c in cases_of(model) if c.witness]
            self.assertTrue(witnesses, model.name)
            defined = {op for op in defined_operators(model) if op.startswith("Never")}
            self.assertEqual({c.witness for c in witnesses}, defined, model.name)
            for c in witnesses:
                self.assertFalse(c.switches, c.name)
                self.assertEqual(c.invariants(), (c.witness,))

    # A historical case is only checkable against code someone can still
    # check out: a published commit or release tag.
    def test_case_names_are_unique_and_historical_cases_name_their_source(self):
        names = [c.name for c in mc.CASES]
        self.assertEqual(len(names), len(set(names)))
        for c in mc.CASES:
            if c.source.startswith("historical"):
                self.assertRegex(c.source, r"\b[0-9a-f]{7}\b|\bv\d+\.\d+\.\d+\b", c.name)


class Config(unittest.TestCase):
    def test_config_sets_every_switch_and_only_the_case_invariants(self):
        model = mc.MODELS["lifecycle"]
        case = mc.Case("x", "s", frozenset({"FinishUngated"}), "WorkHasLiveOwner")
        cfg = mc.render_config(case)
        self.assertIn("  FinishUngated = TRUE\n", cfg)
        for name in set(model.switches) - {"FinishUngated"}:
            self.assertIn(f"  {name} = FALSE\n", cfg)
        self.assertIn("  MaxTurns = 2\n", cfg)
        # A seeded case checks its finding alone, so the run is not decided by
        # which counterexample a TLC worker happens to reach first.
        self.assertTrue(cfg.endswith("INVARIANTS TypeOK WorkHasLiveOwner\n"))
        current = mc.render_config(mc.Case("c", "s"))
        self.assertTrue(current.endswith("INVARIANTS " + " ".join(model.safety) + "\n"))
        witness = mc.render_config(mc.Case("w", "s", witness="NeverParkedCheckpoint"))
        self.assertTrue(witness.endswith("INVARIANTS NeverParkedCheckpoint\n"))

    def test_config_uses_the_case_model(self):
        cfg = mc.render_config(mc.Case("s", "s", frozenset({"MarkerUnstored"}), "ConvergedView", model="stream"))
        self.assertIn("  MarkerUnstored = TRUE\n", cfg)
        self.assertIn("  MarkerAfterUnlock = FALSE\n", cfg)
        self.assertIn("  MaxSeq = 5\n", cfg)
        self.assertNotIn("MaxTurns", cfg)

    def test_scheduling_config_freezes_all_authority_guards(self):
        cases = {case.name: case for case in mc.CASES}
        cfg = mc.render_config(cases["schedule-claimed-cancel-ignored"])
        self.assertIn("  ClaimedCancelIgnored = TRUE\n", cfg)
        self.assertIn("  CancelDoesNotRevoke = FALSE\n", cfg)
        self.assertIn("  RetargetIgnoresStatus = FALSE\n", cfg)
        self.assertIn("  MaxAttempts = 2\n", cfg)
        self.assertNotIn("MaxSeq", cfg)
        self.assertTrue(cfg.endswith("INVARIANTS TypeOK ClaimedCancellationStopsReport\n"))

    def test_consent_timeline_config_is_independent_of_schedule_lifecycle(self):
        cases = {case.name: case for case in mc.CASES}
        cfg = mc.render_config(cases["consent-duplicate-delete-policy"])
        self.assertIn("  DuplicateDeleteAppendsDeny = TRUE\n", cfg)
        self.assertIn("  HumanCancelDoesNotWithdraw = FALSE\n", cfg)
        self.assertIn("  MaxHumanEvents = 5\n", cfg)
        self.assertNotIn("MaxAttempts", cfg)
        self.assertTrue(cfg.endswith("INVARIANTS TypeOK LaterAllowanceSurvivesDuplicate\n"))


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

    def test_completed_state_count_does_not_report_a_progress_suffix(self):
        output = ("Progress(9): 585,489 states generated, 150,365 distinct states found, 82,601 states left on queue.\n"
                  "Model checking completed. No error has been found.\n"
                  "10114342 states generated, 1893136 distinct states found, 0 states left on queue.\n")
        self.assertEqual(mc.parse_output(output), mc.Outcome(True, None, 1893136, None))
        self.assertEqual(mc.parse_output(output.split("Model checking")[0]).distinct, 150365)

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


class RunningCase(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp)
        self.case = mc.Case("case", "test receipt")

    def test_independent_checks_do_not_share_java_standard_module_scratch_files(self):
        seen = set()

        def run(command, **kwargs):
            roots = [arg.split("=", 1)[1] for arg in command if arg.startswith("-Djava.io.tmpdir=")]
            self.assertEqual(len(roots), 1, "each JVM needs its own standard-module extraction directory")
            scratch = Path(roots[0])
            self.assertTrue(scratch.is_absolute())
            self.assertTrue(scratch.is_dir())
            self.assertEqual(scratch.parent, kwargs["cwd"].resolve())
            self.assertNotIn(scratch, seen)
            seen.add(scratch)
            return mock.Mock(returncode=0, stdout=PASS_OUT, stderr="")

        with mock.patch.object(mc.subprocess, "run", side_effect=run):
            for name in ("first-check", "second-check"):
                outcome, _ = mc.run_case(mc.Case(name, "independent JVM"), Path("jar"), self.tmp)
                self.assertTrue(outcome.passed)
        self.assertEqual(len(seen), 2)

    def test_complete_streams_are_private_and_saved_before_interpretation(self):
        def run(*args, **kwargs):
            self.assertNotIn("umask", kwargs)
            self.assertNotIn("env", kwargs)
            (kwargs["cwd"] / "states").mkdir()
            return mock.Mock(returncode=0, stdout=PASS_OUT, stderr="complete stderr\n")

        parse = mc.parse_output
        def verify_saved(text):
            self.assertEqual((self.tmp / "case" / "tlc.out").read_text(), text)
            return parse(text)

        with mock.patch.object(mc.subprocess, "run", side_effect=run), \
                mock.patch.object(mc, "parse_output", side_effect=verify_saved):
            outcome, log = mc.run_case(self.case, Path("jar"), self.tmp)
        self.assertTrue(outcome.passed)
        self.assertEqual(log.read_text(), PASS_OUT + "complete stderr\n")
        self.assertFalse((log.parent / "states").exists())
        for path in (log, log.parent / "case.cfg"):
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_timeout_retains_both_original_streams_without_accepting_partial_output(self):
        failure = mc.subprocess.TimeoutExpired("java", 2, output=PASS_OUT.encode(), stderr=b"original stderr\xff\n")
        with mock.patch.object(mc.subprocess, "run", side_effect=failure):
            outcome, log = mc.run_case(self.case, Path("jar"), self.tmp, timeout=2)
        self.assertFalse(outcome.passed)
        self.assertIn("timed out after 2s", outcome.error)
        self.assertTrue(log.read_text().startswith(PASS_OUT + "original stderr\ufffd\n"))
        self.assertEqual(log.stat().st_mode & 0o777, 0o600)
        self.assertIsNotNone(mc.judge(self.case, outcome))

    def test_nonzero_process_exit_cannot_be_accepted_from_completion_prose(self):
        with mock.patch.object(mc.subprocess, "run", return_value=mock.Mock(returncode=66, stdout=PASS_OUT, stderr="fatal JVM error")):
            outcome, log = mc.run_case(self.case, Path("jar"), self.tmp)
        self.assertFalse(outcome.passed)
        self.assertIn("status 66", outcome.error)
        self.assertIn("fatal JVM error", log.read_text())

    def test_captured_stream_shapes(self):
        self.assertEqual(mc.captured_text(None), "")
        self.assertEqual(mc.captured_text("text"), "text")
        self.assertEqual(mc.captured_text(b"bytes"), "bytes")


@unittest.skipUnless(available_jar(), "tla2tools.jar and java are not installed; run make model-check")
class WithTLC(unittest.TestCase):
    def test_parallel_checks_extract_standard_modules_into_their_own_directories(self):
        from concurrent.futures import ThreadPoolExecutor

        jar = available_jar()
        out = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, out)
        cases = [mc.Case("parallel-lifecycle", "private standard modules", model="lifecycle"),
                 mc.Case("parallel-stream", "private standard modules", model="stream")]
        with ThreadPoolExecutor(max_workers=2) as executor:
            outcomes = list(executor.map(lambda case: mc.run_case(case, jar, out), cases))
        for outcome, log in outcomes:
            self.assertTrue(outcome.passed, log.read_text()[-2000:])
            # SANY removes its extracted files on JVM exit; the completed
            # parser's durable path proves which private copy it consumed.
            scratch = (log.parent / "java-tmp").resolve()
            self.assertIn("Parsing file " + str(scratch / "Naturals.tla"), log.read_text())

    # Each model's current design and its historical reproductions; the full
    # table runs through `make model-check`.
    def test_current_design_passes_and_history_is_found_again(self):
        jar = available_jar()
        out = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, out)
        by_name = {c.name: c for c in mc.CASES}
        for name in ("current", "pre-d77e8d4", "pre-f341e05", "stream-current",
                     "stream-v0.1.45-lost-marker", "stream-v0.1.45-seq-reuse",
                     "schedule-current", "schedule-pre-cancel-authority",
                     "schedule-claimed-cancel-ignored", "schedule-manager-cancel-masks-human",
                     "schedule-retarget-after-terminal", "schedule-reach-cancel-storage-refusal",
                     "consent-current", "consent-pre-cancel-authority"):
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
