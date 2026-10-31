import importlib.util
import json
from pathlib import Path
import unittest


spec = importlib.util.spec_from_file_location("android_account_native", Path(__file__).with_name("android-account-native.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class NativeLogMetadataTest(unittest.TestCase):
    def test_sdk_options_results_and_exception_payloads_are_not_retained(self):
        raw = b'''Capacitor: To native (Capacitor plugin): callbackId: 12, pluginId: Browser, methodName: open
Capacitor: callbackId: 13, pluginId: StreamBody, methodName: open, methodData: {"headers":{"Authorization":"Bearer private-token"},"url":"http://127.0.0.1/?ticket=secret","body":"private-payload"}
Capacitor/Console: native App.getInfo (#14)
Capacitor/Console: result App.getInfo (#14) {"data":"private-result"}
Capacitor: To native (Capacitor plugin): callbackId: 15, pluginId: AppLauncher, methodName: openUrl, methodData: {"url":"http://127.0.0.1/?cookie=secret"}
ActivityTaskManager: START com.capacitorjs.plugins.browser.BrowserControllerActivity https://private/?cookie=secret
java.lang.NullPointerException: private-payload
arbitrary private-token cookie=secret header=secret
'''
        retained = runner.native_log_metadata(raw)
        rows = json.loads(retained)
        self.assertEqual(rows[0], {"event": "native_call", "callback_id": "12", "plugin": "Browser", "method": "open"})
        self.assertIn({"event": "browser_controller_activity"}, rows)
        self.assertIn({"event": "exception", "category": "null_pointer"}, rows)
        self.assertTrue(any(row.get("event") == "result" and row.get("method") == "getInfo" for row in rows))
        self.assertIn({"event": "native_call", "callback_id": "15", "plugin": "AppLauncher", "method": "openUrl"}, rows)
        for sensitive in (b"private", b"Bearer", b"ticket", b"cookie", b"headers", b"http", b"payload", b"data"):
            self.assertNotIn(sensitive, retained)


if __name__ == "__main__":
    unittest.main()
