"""Offline semantic contracts; creates only private temporary fixtures."""
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

import importlib.util
_source = Path(__file__).resolve().parents[1] / "evals/tasks/nuwa-source-reconcile/check_decision.py"
_spec = importlib.util.spec_from_file_location("reconcile_grader", _source)
grader = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(grader)


def write(path, value, raw=False):
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        stream.write(value if raw else json.dumps(value))


def hashes(root):
    return {p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(root.rglob("*")) if p.is_file() and not p.is_symlink()}


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()


class SemanticGraderTests(unittest.TestCase):
    def setUp(self):
        self.private = tempfile.TemporaryDirectory()
        self.addCleanup(self.private.cleanup)
        root = Path(self.private.name)
        self.fixture, self.workspace = root / "trusted", root / "workspace"
        self.records = {
            "engine.json": dict(component="engine", revision=4, status="accepted", passed_tests=40),
            "renderer.json": dict(component="renderer", revision=7, status="accepted", passed_tests=24),
            "phone-old.json": dict(component="phone", revision=2, status="pending", passed_tests=10),
            "phone-current.json": dict(component="phone", revision=3, status="accepted", passed_tests=12),
        }
        for name, record in self.records.items():
            write(self.fixture / "records" / name, record)
        write(self.fixture / "draft.md", "Unverified draft claims.\n", raw=True)
        shutil.copytree(self.fixture, self.workspace)
        self.answer = dict(ready=True, total_passed_tests=76, components={
            "engine": dict(revision=4, status="accepted", evidence="records/engine.json"),
            "renderer": dict(revision=7, status="accepted", evidence="records/renderer.json"),
            "phone": dict(revision=3, status="accepted", evidence="records/phone-current.json"),
        })
        self.save_answer()

    def save_answer(self, answer=None):
        write(self.workspace / "decision.json", self.answer if answer is None else answer)

    def grade(self, **kwargs):
        return grader.evaluate(self.fixture, self.workspace, **kwargs)

    def assert_rejected(self, answer):
        self.save_answer(answer)
        report = self.grade()
        self.assertFalse(report["passed"], report)

    def test_map_and_extra_fields_are_accepted(self):
        self.answer["explanation"] = "Additional explanatory material is allowed."
        self.answer["components"]["engine"]["passed_tests"] = 40
        self.save_answer()
        self.assertTrue(self.grade()["passed"])

    def test_unique_component_array_without_optional_passed_tests_is_accepted(self):
        self.answer["components"] = [dict(component=name, **entry) for name, entry in self.answer["components"].items()]
        self.save_answer()
        self.assertTrue(self.grade()["passed"])

    def test_equivalent_finite_numbers_are_accepted(self):
        self.answer["total_passed_tests"] = 76.0
        self.answer["components"]["engine"]["revision"] = 4.0
        self.save_answer()
        self.assertTrue(self.grade()["passed"])

    def test_bools_never_count_as_revision_or_total_numbers(self):
        for field in ("revision", "total_passed_tests"):
            answer = copy.deepcopy(self.answer)
            if field == "revision":
                answer["components"]["engine"][field] = True
            else:
                answer[field] = True
            with self.subTest(field=field):
                self.assert_rejected(answer)

    def test_bool_true_cannot_pass_even_when_expected_number_is_one(self):
        for root in (self.fixture, self.workspace):
            for path in (root / "records").glob("*.json"):
                path.unlink()
            write(root / "records/one.json", dict(component="one", revision=1, status="accepted", passed_tests=1))
        valid = dict(ready=True, total_passed_tests=1,
                     components={"one": dict(revision=1, status="accepted", evidence="records/one.json")})
        self.save_answer(valid)
        self.assertTrue(self.grade()["passed"])
        for answer in ({**valid, "total_passed_tests": True},
                       {**valid, "components": {"one": {**valid["components"]["one"], "revision": True}}}):
            self.assert_rejected(answer)

    def test_ready_is_an_actual_boolean(self):
        for value in (1, "true", None, False):
            with self.subTest(value=value):
                self.assert_rejected({**self.answer, "ready": value})

    def test_duplicate_json_keys_and_nonfinite_constants_are_rejected(self):
        raw = json.dumps(self.answer)
        for value in ('{"ready":false,"ready":true,"total_passed_tests":76,"components":{}}',
                      raw.replace('"revision": 4', '"revision": 3, "revision": 4'),
                      raw.replace('"total_passed_tests": 76', '"total_passed_tests": NaN'),
                      raw.replace('"total_passed_tests": 76', '"total_passed_tests": Infinity')):
            with self.subTest(value=value[:60]):
                write(self.workspace / "decision.json", value, raw=True)
                self.assertFalse(self.grade()["passed"])

    def test_missing_extra_and_duplicate_components_are_rejected(self):
        missing = copy.deepcopy(self.answer)
        missing["components"].pop("phone")
        extra = copy.deepcopy(self.answer)
        extra["components"]["unknown"] = dict(revision=1, status="accepted", evidence="records/engine.json")
        duplicate = copy.deepcopy(self.answer)
        duplicate["components"] = [dict(component=name, **entry) for name, entry in self.answer["components"].items()]
        duplicate["components"].append(copy.deepcopy(duplicate["components"][0]))
        for answer in (missing, extra, duplicate):
            with self.subTest(answer=answer):
                self.assert_rejected(answer)

    def test_each_component_requires_revision_status_and_evidence(self):
        for field in ("revision", "status", "evidence"):
            answer = copy.deepcopy(self.answer)
            answer["components"]["renderer"].pop(field)
            with self.subTest(field=field):
                self.assert_rejected(answer)

    def test_obsolete_revision_wrong_status_evidence_total_and_readiness_are_rejected(self):
        variants = []
        for field, value in (("revision", 2), ("status", "pending"), ("evidence", "records/phone-old.json"),
                             ("evidence", "/records/phone-current.json"), ("evidence", "../trusted/records/phone-current.json")):
            answer = copy.deepcopy(self.answer)
            answer["components"]["phone"][field] = value
            variants.append(answer)
        variants.extend([{**self.answer, "total_passed_tests": 60}, {**self.answer, "ready": False}])
        for answer in variants:
            with self.subTest(answer=answer):
                self.assert_rejected(answer)

    def test_relative_dot_path_preserves_evidence_meaning(self):
        self.answer["components"]["engine"]["evidence"] = "./records/engine.json"
        self.save_answer()
        self.assertTrue(self.grade()["passed"])

    def test_source_bytes_are_protected_even_when_json_meaning_is_unchanged(self):
        path = self.workspace / "records/phone-current.json"
        write(path, path.read_text() + "\n", raw=True)
        self.assertFalse(self.grade()["passed"])

    def test_deleted_source_and_source_symlink_are_rejected(self):
        path = self.workspace / "records/engine.json"
        path.unlink()
        self.assertFalse(self.grade()["passed"])
        path.symlink_to(self.fixture / "records/engine.json")
        self.assertFalse(self.grade()["passed"])

    def test_workspace_sources_cannot_redefine_expected_answer(self):
        changed = dict(self.records["phone-current.json"], revision=99, passed_tests=999)
        write(self.workspace / "records/phone-current.json", changed)
        self.answer["components"]["phone"]["revision"] = 99
        self.answer["total_passed_tests"] = 1063
        self.save_answer()
        self.assertFalse(self.grade()["passed"])

    def test_fixture_drives_readiness_and_sum_instead_of_fixed_answer_constants(self):
        changed = dict(self.records["phone-current.json"], revision=8, status="pending", passed_tests=5)
        write(self.fixture / "records/phone-current.json", changed)
        write(self.workspace / "records/phone-current.json", changed)
        self.answer["components"]["phone"].update(revision=8, status="pending")
        self.answer.update(ready=False, total_passed_tests=69)
        self.save_answer()
        self.assertTrue(self.grade()["passed"])

    def test_draft_is_not_subject_to_an_unrequested_unchanged_guard(self):
        write(self.workspace / "draft.md", "Reconciled draft.\n", raw=True)
        self.assertTrue(self.grade()["passed"])

    def test_baseline_and_fixture_sha_proof_are_verified(self):
        baseline = hashes(self.fixture)
        report = self.grade(baseline=baseline, fixture_sha256=digest(baseline))
        self.assertTrue(report["passed"])
        self.assertTrue(report["baseline_verified"])
        self.assertTrue(report["fixture_sha256_verified"])

    def test_wrong_baseline_and_wrong_fixture_proof_are_configuration_errors(self):
        baseline = hashes(self.fixture)
        modified = {**baseline, "records/engine.json": "0" * 64}
        for kwargs in (dict(baseline=modified), dict(baseline=baseline, fixture_sha256="0" * 64)):
            with self.subTest(kwargs=kwargs), self.assertRaises(grader.TrustedFixtureError):
                self.grade(**kwargs)

    def test_bad_or_ambiguous_trusted_fixture_is_configuration_error(self):
        conflict = dict(self.records["phone-current.json"], status="pending")
        write(self.fixture / "records/phone-conflict.json", conflict)
        with self.assertRaises(grader.TrustedFixtureError):
            self.grade()

    def test_missing_or_invalid_decision_is_semantic_failure(self):
        path = self.workspace / "decision.json"
        path.unlink()
        self.assertFalse(self.grade()["passed"])
        write(path, "not json", raw=True)
        self.assertFalse(self.grade()["passed"])

    def test_cli_emits_json_and_distinguishes_semantic_and_fixture_failures(self):
        command = [sys.executable, "-B", str(Path(grader.__file__)), "--fixture", str(self.fixture), "--workspace", str(self.workspace)]
        good = subprocess.run(command, capture_output=True, text=True, timeout=5)
        self.assertEqual(good.returncode, 0, good.stderr)
        self.assertTrue(json.loads(good.stdout)["passed"])
        self.save_answer({**self.answer, "ready": False})
        bad = subprocess.run(command, capture_output=True, text=True, timeout=5)
        self.assertEqual(bad.returncode, 1)
        self.assertFalse(json.loads(bad.stdout)["passed"])
        invalid = subprocess.run(command[:-3] + [str(self.fixture / "missing"), "--workspace", str(self.workspace)],
                                 capture_output=True, text=True, timeout=5)
        self.assertEqual(invalid.returncode, 2)
        self.assertFalse(json.loads(invalid.stdout)["passed"])


if __name__ == "__main__":
    unittest.main()
