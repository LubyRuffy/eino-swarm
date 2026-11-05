"""Real child replacement at the screenshot boundary; never run screencapture."""
from contextlib import contextmanager
import json
from pathlib import Path
import stat
import subprocess
import sys
import unittest
from unittest import mock

import test_native_macos_readiness as fixture

smoke = fixture.smoke
REAL_RUN = subprocess.run
REAL_CHMOD = Path.chmod
REPLACE_PNG = """import json,os,pathlib,stat,sys
path=pathlib.Path(sys.argv[1]); temporary=path.with_name(path.name+'.replacement')
temporary.write_bytes(bytes.fromhex(sys.argv[2])); temporary.chmod(0o644)
os.replace(temporary,path)
print(json.dumps({'mode':stat.S_IMODE(path.stat().st_mode),'bytes':path.stat().st_size}))
sys.stderr.write('replacement screenshot child stderr')
sys.exit(int(sys.argv[3]))
"""


class ScreenshotEvidenceTests(unittest.TestCase):
    setUp = fixture.ReadinessTests.setUp
    row = fixture.ReadinessTests.row
    advance = fixture.ReadinessTests.advance
    patches = fixture.ReadinessTests.patches

    @contextmanager
    def boundary(self, *, final=False, capture_exit=0, writer=None):
        evidence = self.root / ("screenshots-" + str(len(list(self.root.glob("screenshots-*")))))
        evidence.mkdir(mode=0o700)
        context = dict(self.context, evidence=evidence)
        captures, actions, messages = [], [], []
        png = fixture.image_bytes()

        def native(command, **kwargs):
            self.advance(.01)
            if command[0] == "/usr/sbin/screencapture":
                self.assertEqual(command[:4], ["/usr/sbin/screencapture", "-x", "-l", "17"])
                image = Path(command[-1])
                self.assertTrue(image.is_file(), "screenshot placeholder must exist before the child")
                self.assertEqual(stat.S_IMODE(image.stat().st_mode), 0o600)
                is_target = image.name == ("smoke-window.png" if final else "initial.png")
                child = REAL_RUN([sys.executable, "-B", "-c", REPLACE_PNG, str(image), png.hex(),
                                  str(capture_exit if is_target else 0)],
                                 capture_output=True, text=True, timeout=kwargs["timeout"], umask=0o022)
                proof = json.loads(child.stdout)
                self.assertEqual(proof, {"mode": 0o644, "bytes": len(png)})
                captures.append(dict(argv=command, timeout=kwargs["timeout"], proof=proof))
                return subprocess.CompletedProcess(command, child.returncode, child.stdout, child.stderr)
            action = command[2]
            actions.append(action)
            row = self.row(expected_visible=bool(command[-1]))
            if action == "fill":
                messages.append(command[-1])
                row["filled"] = True
            elif action == "send":
                row["submitted"] = True
            if messages:
                row.update(area_values=[messages[0]], send_enabled=True)
            return subprocess.CompletedProcess(command, 0, json.dumps(row), "")

        def chmod(path, *args, **kwargs):
            if path.name == ("smoke-window.png" if final else "initial.png"):
                if writer == "denied":
                    raise PermissionError("private screenshot mode denied")
                if writer == "unchanged":
                    return None
            return REAL_CHMOD(path, *args, **kwargs)

        with self.patches() as stack:
            stack.enter_context(mock.patch.object(smoke, "request_json", side_effect=lambda origin, path, deadline:
                self.metadata if path == "/api/meta" else {"threads": []}))
            stack.enter_context(mock.patch.object(smoke.subprocess, "run", side_effect=native))
            stack.enter_context(mock.patch.object(smoke, "wait_trace", return_value=(
                {"turn": {"thread_id": "thread", "id": "turn"}}, "visible result")))
            stack.enter_context(mock.patch.object(smoke, "assert_workspace_outputs", return_value=[]))
            if writer:
                stack.enter_context(mock.patch.object(Path, "chmod", autospec=True, side_effect=chmod))
            yield context, png, captures, actions

    def assert_private_png(self, image, png):
        self.assertEqual(stat.S_IMODE(image.stat().st_mode), 0o600)
        self.assertEqual(image.read_bytes(), png)
        self.assertEqual(smoke.png_dimensions(image), [100, 100])

    def test_initial_capture_secures_the_file_replaced_by_the_real_child(self):
        with self.boundary() as (context, png, captures, actions):
            end = self.deadline.ends
            result = smoke.capture_window(context, self.deadline, self.row(), "initial.png")
            self.assert_private_png(Path(result["path"]), png)
            self.assertEqual(result["dimensions"], [100, 100])
            self.assertTrue(result["content_visible"])
            self.assertEqual(self.deadline.ends, end)
            self.assertEqual(len(captures), 1)
            self.assertLessEqual(captures[0]["timeout"], 2)
            self.assertEqual(actions, [])

    def test_final_run_smoke_secures_both_replaced_images_without_repeating_input(self):
        with self.boundary(final=True) as (context, png, captures, actions):
            end = self.deadline.ends
            result = smoke.run_smoke(context, deadline=self.deadline)
            self.assertTrue(result["passed"])
            self.assertEqual(result["screenshot_dimensions"], [100, 100])
            self.assertEqual(self.deadline.ends, end)
            self.assertEqual(len(captures), 2)
            self.assertEqual(actions, ["snapshot", "fill", "snapshot", "send", "snapshot"])
            for capture in captures:
                self.assert_private_png(Path(capture["argv"][-1]), png)

    def test_failed_mode_write_or_unchanged_mode_is_evidence_failure(self):
        for final in (False, True):
            for writer in ("denied", "unchanged"):
                with self.subTest(final=final, writer=writer), self.boundary(final=final, writer=writer) as (context, png, captures, actions):
                    with self.assertRaises(smoke.readiness.Failure) as caught:
                        if final:
                            smoke.run_smoke(context, deadline=self.deadline)
                        else:
                            smoke.capture_window(context, self.deadline, self.row(), "initial.png")
                    self.assertEqual(caught.exception.failure_kind, "native_evidence_failed")
                    self.assertEqual(actions.count("send"), int(final))

    def test_screenshot_command_failure_stays_primary_if_mode_write_also_fails(self):
        for final in (False, True):
            with self.subTest(final=final), self.boundary(final=final, capture_exit=9, writer="denied") as (context, png, captures, actions):
                with self.assertRaisesRegex(RuntimeError, "replacement screenshot child stderr") as caught:
                    if final:
                        smoke.run_smoke(context, deadline=self.deadline)
                    else:
                        smoke.capture_window(context, self.deadline, self.row(), "initial.png")
                if not final:
                    self.assertEqual(caught.exception.failure_kind, "native_pixels_failed")
                self.assertTrue(any("private screenshot" in note for note in getattr(caught.exception, "__notes__", [])))
                self.assertEqual(actions.count("send"), int(final))


if __name__ == "__main__":
    unittest.main()
