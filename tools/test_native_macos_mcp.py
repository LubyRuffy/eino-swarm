import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
spec = importlib.util.spec_from_file_location("native_macos_mcp", Path(__file__).with_name("native_macos_mcp.py"))
mcp = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mcp)


class NativeMCPAssertions(unittest.TestCase):
    @staticmethod
    def window():
        return {"pid": 42, "case_verified": True, "frontmost": True, "has_web_area": True,
                "title": "zwai", "window_count": 1, "window_id": 7, "window_owner_pid": 42,
                "screen_capture_allowed": True, "windows": [{"id": 7, "owner_pid": 42}],
                "rect": [10, 20, 1200, 800], "origin": "http://127.0.0.1:1234",
                "web_urls": ["http://127.0.0.1:1234/?shell=desktop&pid=42"]}

    def test_settings_without_a_composer_still_requires_real_window_and_origin(self):
        mcp.assert_settings_window({**self.window(), "textareas": 0}, 42)
        mcp.assert_settings_window({**self.window(), "textareas": 3}, 42)
        for change in ({"pid": True}, {"window_owner_pid": 99}, {"window_count": 2},
                       {"case_verified": False}, {"frontmost": False}, {"screen_capture_allowed": False},
                       {"windows": []}, {"rect": [0, 0, 0, 800]}, {"rect": [0, 0, float('nan'), 2]},
                       {"web_urls": ["http://127.0.0.1:1234/?shell=desktop&pid=99"]}):
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                mcp.assert_settings_window({**self.window(), **change}, 42)

    def test_native_inputs_need_one_exact_enabled_control(self):
        button = {"AXRole": "AXButton", "AXTitle": "echo", "AXEnabled": True}
        self.assertEqual(mcp.catalog_control({"tree": [button]}), "echo")
        self.assertIsNone(mcp.exact_control({"tree": [{**button, "AXEnabled": False}]}, "echo"))
        self.assertIsNone(mcp.exact_control({"tree": [{**button, "AXTitle": "echo twice"}]}, "echo"))
        self.assertIsNone(mcp.catalog_control({"tree": [{**button, "AXRole": "AXStaticText"}]}))
        with self.assertRaisesRegex(RuntimeError, "ambiguous"):
            mcp.exact_control({"tree": [button, dict(button)]}, "echo")
        with self.assertRaisesRegex(RuntimeError, "button"):
            mcp.catalog_control({"tree": [{**button, "AXRole": "AXRadioButton"}]} )

    def test_schema_requires_expanded_fields_not_just_the_tool_name(self):
        with self.assertRaisesRegex(RuntimeError, "expanded"):
            mcp.assert_schema({"tree": [{"AXValue": "echo Input schema"}]})
        mcp.assert_schema({"tree": [{"AXValue": 'Input schema {"properties":{"text":{}},"required":["text"]}'}]})

    def test_unsupported_native_role_never_authorizes_a_press(self):
        row = {"tree": [{"AXRole": "AXStaticText", "AXTitle": "echo", "AXEnabled": True}]}
        with patch.object(mcp, "wait_view", side_effect=TimeoutError("unavailable")), patch.object(mcp, "helper") as helper:
            with self.assertRaises(TimeoutError):
                mcp.press({}, None, "echo")
        helper.assert_not_called()

    def test_control_failure_and_timeout_are_not_replayed(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "helper"
            binary.write_bytes(b"frozen helper")
            context = {"app_pid": 42, "controls": binary,
                       "controls_sha256": mcp.hashlib.sha256(binary.read_bytes()).hexdigest()}
            result = SimpleNamespace(returncode=2, stdout="first refusal", stderr="original diagnostic")
            for failure in (None, subprocess.TimeoutExpired([str(binary)], 8, output=b"partial original")):
                with self.subTest(failure=failure), patch.object(mcp.smoke, "assert_process"), patch.object(mcp, "snapshot", return_value={"tree": [{"AXRole": "AXRadioButton", "AXTitle": "MCP", "AXEnabled": True}]}), patch.object(
                        mcp.smoke, "observed_command", return_value=result, side_effect=failure) as observed:
                    with self.assertRaises(subprocess.SubprocessError):
                        mcp.helper(context, None, "controls", "press", "MCP")
                    self.assertEqual(observed.call_count, 1)
            binary.write_bytes(b"changed helper")
            with patch.object(mcp.smoke, "assert_process"), patch.object(mcp.smoke, "observed_command") as observed:
                with self.assertRaisesRegex(RuntimeError, "changed"):
                    mcp.helper(context, None, "controls", "press", "MCP")
                observed.assert_not_called()

    def test_observed_image_admits_only_decodable_nonblank_existing_writer_results(self):
        with tempfile.TemporaryDirectory() as temporary:
            context = {"evidence": Path(temporary), "app_pid": 42}
            for stats in ({"buckets": 1, "dominant_fraction": 1}, {"buckets": 30, "dominant_fraction": .999}):
                with patch.object(mcp.evidence, "save_window"), patch.object(mcp, "helper", return_value=stats):
                    with self.assertRaisesRegex(RuntimeError, "blank"):
                        mcp.save_window(context, None, "native", self.window())


if __name__ == "__main__":
    unittest.main()
