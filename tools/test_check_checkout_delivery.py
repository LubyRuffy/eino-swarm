"""The reported checkout and its served bundle must be checked together."""
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest


SCRIPT = Path(__file__).with_name("check_checkout_delivery.py")


class CheckoutDeliveryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.checkout = Path(self.temp.name) / "checkout"
        self.checkout.mkdir()
        self.git("init", "-q")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        (self.checkout / "README.md").write_text("first\n")
        self.git("add", "README.md")
        self.git("commit", "-qm", "fixture")
        (self.checkout / ".git" / "info" / "exclude").write_text("/frontend/dist/\n")
        self.required = self.git("rev-parse", "HEAD").stdout.strip()
        self.dist = self.checkout / "frontend" / "dist"
        (self.dist / "assets").mkdir(parents=True)
        (self.dist / "assets" / "app.js").write_bytes(b"export const current = true;\n")

    def git(self, *args):
        return subprocess.run(["git", "-C", str(self.checkout), *args],
                              check=True, capture_output=True, text=True)

    def check(self, checkout=None, required=None, url=None):
        command = [sys.executable, str(SCRIPT), "--checkout", str(checkout or self.checkout),
                   "--required-commit", required or self.required]
        if url:
            command += ["--url", url]
        result = subprocess.run(command, capture_output=True, text=True)
        self.assertTrue(result.stdout, result.stderr)
        return result.returncode, json.loads(result.stdout)

    @contextmanager
    def serve(self, source="/assets/app.js", body=b"export const current = true;\n",
              redirect=None, extra_module=None):
        requests = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                requests.append(self.path)
                if self.path == "/":
                    payload = ('<script type="module" src="' + source + '"></script>').encode()
                    if extra_module:
                        payload += ('<script type="module" src="' + extra_module + '"></script>').encode()
                else:
                    if redirect:
                        self.send_response(302)
                        self.send_header("Location", redirect)
                        self.end_headers()
                        return
                    payload = body
                self.send_response(200)
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *args):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            yield "http://127.0.0.1:" + str(server.server_port), requests
        finally:
            server.shutdown()
            thread.join()
            server.server_close()

    def test_committed_fix_in_dirty_actual_checkout_passes_without_native_claim(self):
        (self.checkout / "README.md").write_text("unrelated local edit\n")
        code, result = self.check()
        self.assertEqual(code, 0)
        self.assertTrue(result["checks"]["required_commit"]["ok"])
        self.assertTrue(result["dirty"])
        self.assertFalse(result["native_behavior_verified"])
        self.assertEqual(result["scope"], "source_and_assets_only")

    def test_clean_checkout_without_fix_fails(self):
        (self.checkout / "README.md").write_text("later\n")
        self.git("commit", "-am", "later", "-q")
        required = self.git("rev-parse", "HEAD").stdout.strip()
        self.git("checkout", "-q", "HEAD~1")
        code, result = self.check(required=required)
        self.assertNotEqual(code, 0)
        self.assertFalse(result["checks"]["required_commit"]["ok"])
        self.assertFalse(result["dirty"])

    def test_subdirectory_and_non_repository_are_rejected(self):
        for path in (self.dist, Path(self.temp.name)):
            with self.subTest(path=path):
                code, result = self.check(checkout=path)
                self.assertNotEqual(code, 0)
                self.assertFalse(result["checks"]["checkout_root"]["ok"])

    def test_unknown_commit_and_option_instead_of_sha_are_rejected(self):
        for sha in ("a" * 40, "--help"):
            with self.subTest(sha=sha):
                # Use = so argparse passes the malicious-looking value to validation.
                result = subprocess.run([sys.executable, str(SCRIPT), "--checkout", str(self.checkout),
                                         "--required-commit=" + sha], capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(json.loads(result.stdout)["checks"]["required_commit"]["ok"])

    def test_same_origin_module_matches_disk_bytes(self):
        with self.serve(source="./assets/app.js") as (url, requests):
            code, result = self.check(url=url)
        self.assertEqual(code, 0)
        asset = result["checks"]["frontend_assets"]["assets"][0]
        self.assertEqual(asset["served_sha256"], asset["checkout_sha256"])
        self.assertTrue(asset["ok"])
        self.assertEqual(requests, ["/", "/assets/app.js"])

    def test_every_module_must_match(self):
        (self.dist / "assets" / "second.js").write_bytes(b"different local build")
        with self.serve(extra_module="/assets/second.js") as (url, requests):
            code, result = self.check(url=url)
        self.assertNotEqual(code, 0)
        assets = result["checks"]["frontend_assets"]["assets"]
        self.assertEqual([asset["ok"] for asset in assets], [True, False])
        self.assertEqual(requests, ["/", "/assets/app.js", "/assets/second.js"])

    def test_stale_served_bundle_fails(self):
        with self.serve(body=b"stale") as (url, _):
            code, result = self.check(url=url)
        self.assertNotEqual(code, 0)
        self.assertFalse(result["checks"]["frontend_assets"]["assets"][0]["ok"])

    def test_cross_origin_traversal_and_encoded_traversal_are_not_requested(self):
        for source in ("http://example.invalid/app.js", "//example.invalid/app.js",
                       "/../outside.js", "/assets/%2e%2e/outside.js", "/assets/%5capp.js"):
            with self.subTest(source=source), self.serve(source=source) as (url, requests):
                code, result = self.check(url=url)
                self.assertNotEqual(code, 0)
                self.assertFalse(result["checks"]["frontend_assets"]["ok"])
                self.assertEqual(requests, ["/"])

    def test_redirect_is_not_followed(self):
        with self.serve(redirect="http://example.invalid/app.js") as (url, requests):
            code, result = self.check(url=url)
        self.assertNotEqual(code, 0)
        self.assertFalse(result["checks"]["frontend_assets"]["ok"])
        self.assertEqual(requests, ["/", "/assets/app.js"])

    def test_non_loopback_url_and_credentials_are_rejected_without_fetching(self):
        for url in ("http://example.invalid", "https://127.0.0.1", "http://user:secret@127.0.0.1"):
            with self.subTest(url=url):
                code, result = self.check(url=url)
                self.assertNotEqual(code, 0)
                self.assertFalse(result["checks"]["frontend_assets"]["ok"])
                self.assertNotIn("secret", json.dumps(result))

    def test_missing_module_or_local_asset_fails(self):
        for source in ("", "/assets/missing.js"):
            with self.subTest(source=source), self.serve(source=source) as (url, _):
                code, result = self.check(url=url)
                self.assertNotEqual(code, 0)
                self.assertFalse(result["checks"]["frontend_assets"]["ok"])

    def test_asset_symlink_cannot_escape_dist(self):
        outside = Path(self.temp.name) / "outside.js"
        outside.write_bytes(b"export const current = true;\n")
        (self.dist / "assets" / "linked.js").symlink_to(outside)
        with self.serve(source="/assets/linked.js") as (url, requests):
            code, result = self.check(url=url)
        self.assertNotEqual(code, 0)
        self.assertFalse(result["checks"]["frontend_assets"]["ok"])
        self.assertEqual(requests, ["/"])


if __name__ == "__main__":
    unittest.main()
