#!/usr/bin/env python3
"""Check a reported checkout and its served assets; native behavior needs acceptance."""
import argparse
import hashlib
from html.parser import HTMLParser
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.parse import unquote, urljoin, urlsplit
from urllib.request import build_opener, HTTPRedirectHandler, ProxyHandler


class ModuleScripts(HTMLParser):
    def __init__(self):
        super().__init__()
        self.sources = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "script" and attrs.get("type") == "module" and attrs.get("src"):
            self.sources.append(attrs["src"])


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, newurl):
        return None


def git(checkout, *args):
    return subprocess.run(["git", "-C", str(checkout), *args], check=True,
                          capture_output=True, text=True, timeout=10,
                          env={**os.environ, "GIT_OPTIONAL_LOCKS": "0"}).stdout.strip()


def origin(url):
    return url.scheme, url.hostname, url.port or 80


def check_assets(checkout, url):
    parsed = urlsplit(url)
    if (parsed.scheme != "http" or parsed.username is not None or parsed.password is not None
            or parsed.path not in ("", "/") or parsed.query or parsed.fragment):
        raise ValueError("expected_loopback_http_origin")
    try:
        loopback = ipaddress.ip_address(parsed.hostname).is_loopback
    except ValueError:
        loopback = False
    if not loopback:
        raise ValueError("expected_numeric_loopback_address")
    base = url.rstrip("/") + "/"
    # Ignore environment proxies and reject redirects before they can leave loopback.
    opener = build_opener(ProxyHandler({}), NoRedirect())

    def read(target):
        with opener.open(target, timeout=5) as response:
            return response.read()

    parser = ModuleScripts()
    parser.feed(read(base).decode("utf-8"))
    if not parser.sources:
        raise ValueError("no_module_scripts")
    dist = (checkout / "frontend" / "dist").resolve()
    assets = []
    for source in parser.sources:
        raw_path = unquote(urlsplit(source).path)
        if "\\" in raw_path or ".." in raw_path.split("/"):
            raise ValueError("unsafe_asset_path")
        target = urlsplit(urljoin(base, source))
        if origin(target) != origin(parsed) or target.username is not None or target.password is not None:
            raise ValueError("cross_origin_asset")
        path = unquote(target.path).lstrip("/")
        if not path or "\\" in path or any(part in (".", "..") for part in path.split("/")):
            raise ValueError("unsafe_asset_path")
        local = (dist / path).resolve()
        if not local.is_relative_to(dist):
            raise ValueError("asset_outside_dist")
        checkout_hash = hashlib.sha256(local.read_bytes()).hexdigest()
        served_hash = hashlib.sha256(read(target.geturl())).hexdigest()
        assets.append({"path": path, "checkout_sha256": checkout_hash,
                       "served_sha256": served_hash, "ok": checkout_hash == served_hash})
    return {"ok": all(asset["ok"] for asset in assets), "assets": assets}


def check(checkout, required_commit, url=None):
    checkout = Path(checkout).resolve()
    checks = {"checkout_root": {"ok": False}, "required_commit": {"ok": False}}
    result = {"checkout": str(checkout), "checks": checks, "ok": False,
              "scope": "source_and_assets_only", "native_behavior_verified": False}
    try:
        root = Path(git(checkout, "rev-parse", "--show-toplevel")).resolve()
        checks["checkout_root"] = {"ok": root == checkout, "git_root": str(root)}
        result["head"] = git(checkout, "rev-parse", "HEAD")
        result["dirty"] = bool(git(checkout, "status", "--porcelain"))
    except (OSError, subprocess.SubprocessError):
        checks["checkout_root"]["error"] = "not_a_working_checkout"
    if checks["checkout_root"]["ok"]:
        try:
            if not re.fullmatch(r"[0-9a-fA-F]{7,40}", required_commit):
                raise ValueError("invalid_commit_sha")
            commit = git(checkout, "rev-parse", "--verify", required_commit + "^{commit}")
            git(checkout, "merge-base", "--is-ancestor", commit, "HEAD")
            checks["required_commit"] = {"ok": True, "sha": commit}
        except ValueError as error:
            checks["required_commit"]["error"] = str(error)
        except (OSError, subprocess.SubprocessError):
            checks["required_commit"]["error"] = "commit_missing_from_head"
    if url is not None:
        try:
            if not checks["checkout_root"]["ok"]:
                raise ValueError("checkout_root_required")
            checks["frontend_assets"] = check_assets(checkout, url)
        except ValueError as error:
            checks["frontend_assets"] = {"ok": False, "error": str(error)}
        except OSError:
            checks["frontend_assets"] = {"ok": False, "error": "asset_read_failed"}
    result["ok"] = all(item["ok"] for item in checks.values())
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--checkout", required=True, type=Path)
    parser.add_argument("--required-commit", required=True)
    parser.add_argument("--url", help="optional numeric loopback HTTP origin")
    args = parser.parse_args()
    result = check(args.checkout, args.required_commit, args.url)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
