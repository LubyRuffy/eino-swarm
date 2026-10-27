import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import next_release_version as next_release


class NextReleaseVersionTests(unittest.TestCase):
    def test_compares_numeric_versions_instead_of_lexicographic_tags(self):
        result = next_release.choose_next_version(["v0.1.9", "v0.1.44"], ["v0.1.43"], [])
        self.assertEqual(result["version"], "0.1.45")
        self.assertEqual(result["highest_reserved"], "0.1.44")

    def test_draft_releases_and_unpublished_allocations_reserve_their_versions(self):
        allocated = next_release.allocated_versions({
            "pending_new_batch": {"version": "0.1.46", "version_locked": False},
            "completed_batches": {"0.1.44": {"batch": {"version": "0.1.44"}}},
            "next_unallocated_batch": {"version": "0.1.47"},
        })
        result = next_release.choose_next_version(["v0.1.44"], ["v0.1.45"], allocated)
        self.assertEqual(result["version"], "0.1.48")

    def test_ignores_nonrelease_refs_and_canonicalizes_duplicate_numeric_versions(self):
        result = next_release.choose_next_version(
            ["main", "v0.1.44-rc1", "V00.01.044", "0.1.44"], [], [])
        self.assertEqual(result["reserved_versions"], ["0.1.44"])
        self.assertEqual(result["version"], "0.1.45")

    def test_carries_patch_and_minor_at_the_existing_version_code_boundary(self):
        for previous, expected in [("0.1.99", "0.2.0"), ("0.99.99", "1.0.0")]:
            with self.subTest(previous=previous):
                self.assertEqual(next_release.choose_next_version([previous], [], [])["version"], expected)

    def test_refuses_unknown_baseline_and_unsupported_numeric_versions(self):
        with self.assertRaisesRegex(ValueError, "No confirmed"):
            next_release.choose_next_version(["development"], [], [])
        with self.assertRaisesRegex(ValueError, "build-number"):
            next_release.choose_next_version(["1.2.100"], [], [])
        with self.assertRaisesRegex(ValueError, "build-number"):
            next_release.choose_next_version(["210000.0.99"], [], [])

    def test_refuses_malformed_allocated_state_instead_of_reusing_its_version(self):
        cases = [[], {"pending_new_batch": []}, {"pending_new_batch": {"version": "bad"}},
                 {"completed_batches": []}, {"completed_batches": {"0.1.44": []}},
                 {"completed_batches": {"0.1.44": {"batch": []}}}]
        for state in cases:
            with self.subTest(state=state), self.assertRaises(ValueError):
                next_release.allocated_versions(state)

    def test_actual_command_adapter_reads_origin_and_all_release_pages_without_mutation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            state_path = root / "delivery.json"
            content = json.dumps({"pending_new_batch": {"version": "0.1.46"}})
            state_path.write_text(content)
            calls = []

            def command(args, cwd):
                self.assertEqual(cwd, root)
                calls.append(args)
                if args == ["git", "ls-remote", "--tags", "origin"]:
                    return "a" * 40 + "\trefs/tags/v0.1.44\n" + "b" * 40 + "\trefs/tags/v0.1.44^{}\n"
                if args[:3] == ["gh", "repo", "view"]:
                    return "example/repo\n"
                self.assertEqual(args, ["gh", "api", "--paginate", "--slurp", "repos/example/repo/releases"])
                return json.dumps([[{"tag_name": "v0.1.44"}], [{"tag_name": "v0.1.45", "draft": True}]])

            result = next_release.inspect_versions(root, state_path, command)
            self.assertEqual(result["version"], "0.1.47")
            self.assertEqual(state_path.read_text(), content)
            self.assertEqual(len(calls), 3)
            self.assertEqual(list(root.iterdir()), [state_path])

    def test_refuses_malformed_remote_or_github_responses(self):
        cases = [("bad tag line", "example/repo", "[]"),
                 ("", "malformed", "[]"),
                 ("", "example/repo", "{}"),
                 ("", "example/repo", "[{}]"),
                 ("", "example/repo", "[[{}]]")]
        for tags, repo, releases in cases:
            with self.subTest(cases=(tags, repo, releases)):
                responses = iter([tags, repo, releases])
                with self.assertRaises(ValueError):
                    next_release.inspect_versions(Path("."), Path("unused"), lambda *_: next(responses))

    def test_cli_only_prints_a_suggestion_and_keeps_a_read_only_check_available(self):
        result = {"version": "0.1.45", "highest_reserved": "0.1.44", "reserved_versions": ["0.1.44"]}
        for options, expected in [([], json.dumps(result)), (["--plain"], "0.1.45")]:
            with self.subTest(options=options), patch.object(next_release, "inspect_versions", return_value=result) as inspect:
                output = io.StringIO()
                with contextlib.redirect_stdout(output):
                    self.assertEqual(next_release.main(["--source-dir", ".", "--state", "private.json", *options]), 0)
                self.assertEqual(output.getvalue().strip(), expected)
                self.assertEqual(inspect.call_args.args[:2], (Path.cwd(), Path("private.json")))

    def test_subprocess_adapter_preserves_the_selected_checkout(self):
        with patch.object(next_release.subprocess, "check_output", return_value="result") as execute:
            self.assertEqual(next_release.command(["git", "ls-remote"], Path("selected")), "result")
            execute.assert_called_once_with(["git", "ls-remote"], cwd=Path("selected"), text=True)


if __name__ == "__main__":
    unittest.main()
