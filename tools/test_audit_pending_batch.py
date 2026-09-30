"""Closing an accepted fix must not cancel its outstanding publication."""
import contextlib
import io
import json
from pathlib import Path
import runpy
import unittest
from datetime import datetime, timedelta, timezone
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("audit_pending_batch.py")


class PendingBatchTests(unittest.TestCase):
    def audit(self, numbers, *, closed=True, delivered=False, locked=False,
              missing_fix=False, integrated=True, extra_numbers=(),
              extra_accepted=True, alias=False, assigned_numbers=(),
              commits=0, age_hours=1, platform="ios", preallocated=False,
              additional_platforms=None, extra_platform="macos", extra_integrated=True):
        accepted_at = (datetime.now(timezone.utc) - timedelta(hours=age_hours)).isoformat()
        state = {
            "repository": "owner/project", "integration_checkout": "/clean/main",
            "last_published_source_sha": "base-sha",
            "pending_new_batch": {"issues": numbers, "version_locked": locked,
                                  "version": "1.0.0" if locked or preallocated else None},
            "next_unallocated_batch": {"issues": list(numbers) + list(extra_numbers),
                "additional_platforms": additional_platforms or {}},
            "issues": {str(n): {
                "code": "verified_integrated_and_pushed",
                "fix_sha": None if missing_fix else f"fix-{n}",
                "android": "published", "macos": "not_required",
                "ios": "published" if delivered else "waiting_next_day",
                "server": "not_required",
                "accepted_at": accepted_at,
            } for n in numbers},
        }
        for n in extra_numbers:
            state["issues"][str(n)] = {
                "code": "verified_integrated_and_pushed" if extra_accepted else "implementing",
                "fix_sha": f"fix-{n}",
                "macos": "pending_batch" if extra_platform == "macos" else "not_required",
                "android": "pending_batch" if extra_platform == "mobile" else "not_required",
                "ios": "pending_batch" if extra_platform == "mobile" else "not_required",
                "server": "not_required",
                "count_as_new_release_issue": not alias,
                "accepted_at": accepted_at,
            }
        for n in assigned_numbers:
            state["issues"][str(n)]["batch_version"] = "1.0.0"
        ios = {"pending_changes": [{"issue": n, "status": "published" if delivered
                                    else "waiting_for_next_ios_batch"} for n in numbers]}
        actual_read = Path.read_text

        def read(path, *args, **kwargs):
            if path.name == "delivery-state.json":
                return json.dumps(state)
            if path.name == "testflight-releases.json":
                return json.dumps(ios)
            return actual_read(path, *args, **kwargs)

        def command(args, **kwargs):
            if args[0] == "gh":
                rows = [{"number": n, "state": "closed" if closed else "open"}
                        for n in list(numbers) + list(extra_numbers)]
                if "state=open" in args[-1]:
                    rows = [r for r in rows if r["state"] == "open"]
                return json.dumps([rows])
            if args[:3] == ["git", "rev-list", "--first-parent"]:
                return str(commits) + "\n"
            self.assertEqual(args, ["git", "rev-parse", "HEAD"])
            return "main-sha\n"

        def ancestry(args, **kwargs):
            self.assertEqual(args[:3], ["git", "merge-base", "--is-ancestor"])
            if not extra_integrated and "desktop-sha" in args:
                raise SystemExit("desktop commit is not integrated")
            if not integrated:
                raise SystemExit("fix is not integrated")

        output = io.StringIO()
        with patch.object(Path, "read_text", read), \
                patch("subprocess.check_output", side_effect=command), \
                patch("subprocess.run", side_effect=ancestry), \
                patch("sys.argv", [str(SCRIPT)]), contextlib.redirect_stdout(output):
            runpy.run_path(str(SCRIPT), run_name="__main__")
        return json.loads(output.getvalue())

    def test_closed_fix_keeps_its_pending_platform(self):
        result = self.audit([1])
        self.assertEqual(result["issues"], [1])
        self.assertEqual(result["closed_pending_issues"], [1])
        self.assertFalse(result["allocate_new_version"])

    def test_four_commits_qualify_even_with_one_pending_issue(self):
        result = self.audit([1], commits=4)
        self.assertEqual(result["pending_commit_count"], 4)
        self.assertTrue(result["allocate_new_version"])

    def test_allocated_candidate_without_public_platform_still_passes_first_publish_gate(self):
        result = self.audit([1], assigned_numbers=[1], commits=4, preallocated=True)
        self.assertEqual(result["unallocated_issues"], [1])
        self.assertFalse(result["allocate_new_version"])
        self.assertTrue(result["resume_allocated_version"])

    def test_three_commits_under_a_day_do_not_qualify(self):
        result = self.audit([1, 2, 3], commits=3)
        self.assertEqual(result["distinct_code_accepted_pending_count"], 3)
        self.assertFalse(result["allocate_new_version"])

    def test_oldest_pending_change_qualifies_after_a_day(self):
        result = self.audit([1], commits=1, age_hours=25)
        self.assertTrue(result["allocate_new_version"])
        self.assertIn("ios", result["platforms_to_release"])

    def test_locked_batch_recovers_without_another_version(self):
        result = self.audit([1, 2, 3], locked=True, commits=5, age_hours=25)
        self.assertEqual(result["issues"], [1, 2, 3])
        self.assertEqual(result["batch_version"], "1.0.0")
        self.assertFalse(result["allocate_new_version"])

    def test_delivered_fix_is_not_counted_again(self):
        self.assertEqual(self.audit([1], delivered=True)["issues"], [])

    def test_missing_fix_evidence_is_rejected_even_when_closed(self):
        with self.assertRaisesRegex(SystemExit, "missing fix SHA"):
            self.audit([1], missing_fix=True)

    def test_unintegrated_fix_is_rejected_even_when_closed(self):
        with self.assertRaisesRegex(SystemExit, "not integrated"):
            self.audit([1], integrated=False)

    def test_open_fixes_are_still_counted(self):
        self.assertEqual(self.audit([1, 2], closed=False)["issues"], [1, 2])

    def test_mac_only_fix_is_not_lost_behind_a_locked_ios_batch(self):
        result = self.audit([1, 2, 3, 4, 5, 6], locked=True, extra_numbers=[7])
        self.assertEqual(result["issues"], [1, 2, 3, 4, 5, 6, 7])
        self.assertEqual(result["unallocated_issues"], [7])
        self.assertFalse(result["allocate_new_version"])

    def test_locked_issues_do_not_count_towards_the_next_version(self):
        result = self.audit([1, 2, 3], locked=True, extra_numbers=[4, 5])
        self.assertEqual(result["distinct_unallocated_pending_count"], 2)
        self.assertFalse(result["allocate_new_version"])

    def test_next_batch_can_qualify_while_old_ios_is_pending(self):
        result = self.audit([1, 2, 3], locked=True, extra_numbers=[4, 5, 6], commits=4)
        self.assertEqual(result["unallocated_issues"], [4, 5, 6])
        self.assertTrue(result["allocate_new_version"])

    def test_complete_main_desktop_change_is_included_without_mislabeling_phone_issues(self):
        result = self.audit([], extra_numbers=[47, 48], commits=4, extra_platform="mobile",
            additional_platforms={"macos": {"source_shas": ["desktop-sha"],
                                           "status": "pending_native_window_acceptance"}})
        self.assertEqual(result["unallocated_issues"], [47, 48])
        self.assertEqual(result["platforms_to_release"], ["macos", "android", "ios"])
        self.assertTrue(result["allocate_new_version"])

    def test_extra_platform_commit_must_be_in_complete_main(self):
        with self.assertRaisesRegex(SystemExit, "desktop commit is not integrated"):
            self.audit([], extra_numbers=[47], commits=4, extra_platform="mobile",
                additional_platforms={"macos": {"source_shas": ["desktop-sha"], "status": "pending"}},
                extra_integrated=False)

    def test_assigned_rows_stay_allocated_when_batch_index_moves(self):
        result = self.audit([], extra_numbers=[1, 2, 3, 4], assigned_numbers=[1, 2, 3])
        self.assertEqual(result["allocated_pending_issues"], [1, 2, 3])
        self.assertEqual(result["unallocated_issues"], [4])
        self.assertFalse(result["allocate_new_version"])

    def test_three_mac_only_fixes_qualify_without_ios_pending_rows(self):
        result = self.audit([], extra_numbers=[1, 2, 3], commits=4)
        self.assertEqual(result["unallocated_issues"], [1, 2, 3])
        self.assertTrue(result["allocate_new_version"])

    def test_unaccepted_extra_work_cannot_qualify(self):
        result = self.audit([], extra_numbers=[1, 2, 3], extra_accepted=False)
        self.assertEqual(result["issues"], [])
        self.assertFalse(result["allocate_new_version"])

    def test_alias_reports_do_not_become_new_distinct_fixes(self):
        result = self.audit([], extra_numbers=[1, 2, 3], alias=True)
        self.assertEqual(result["issues"], [])

if __name__ == "__main__":
    unittest.main()
