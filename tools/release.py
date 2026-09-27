#!/usr/bin/env python3
"""Publish one verified main batch; retain independent platform results."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = Path(__file__).resolve().parents[1]


def read_json(path, default=None):
    return json.loads(Path(path).read_text()) if Path(path).exists() else default


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix('.tmp')
    with temp.open('w') as stream:
        os.chmod(temp, 0o600)
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write('\n')
    temp.replace(path)


def sha256(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def version_code(version):
    if not re.fullmatch(r'\d+\.\d+\.\d+', version):
        raise ValueError('VERSION must be major.minor.patch')
    major, minor, patch = map(int, version.split('.'))
    code = major * 10000 + minor * 100 + patch
    if minor >= 100 or patch >= 100 or not 0 < code <= 2100000000:
        raise ValueError('Version cannot map uniquely to the existing mobile build-number scheme')
    return str(code)


def run(args, *, cwd=ROOT, env=None):
    # Do not print argv: altool's API identifiers and signing arguments stay private.
    result = subprocess.run(args, cwd=cwd, env=env, text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError(f'{Path(args[0]).name} failed (exit {result.returncode}); no success recorded')
    return (result.stdout or result.stderr).strip()


def source_preflight(root, version):
    version_code(version)
    if run(['git', 'status', '--porcelain'], cwd=root):
        raise ValueError('Release source checkout must be clean')
    run(['git', 'fetch', 'origin', 'main', '--tags'], cwd=root)
    sha = run(['git', 'rev-parse', 'HEAD'], cwd=root)
    run(['git', 'merge-base', '--is-ancestor', sha, 'origin/main'], cwd=root)
    package = read_json(root / 'mobile/package.json')
    if package['version'] != version:
        raise ValueError('VERSION must match the committed mobile/package.json')
    repo = run(['gh', 'repo', 'view', '--json', 'nameWithOwner', '-q', '.nameWithOwner'], cwd=root)
    module = re.search(r'^module github.com/(\S+)', (root / 'go.mod').read_text(), re.M)
    if not module or module[1] != repo:
        raise ValueError('GitHub repository does not match the source module')
    tags = run(['git', 'ls-remote', 'origin', f'refs/tags/v{version}', f'refs/tags/v{version}^{{}}'], cwd=root)
    if tags:
        tag_sha = tags.splitlines()[-1].split()[0]
        if tag_sha != sha:
            raise ValueError('Published version is frozen at another source SHA; use its isolated checkout')
    else:
        if run(['git', 'rev-parse', 'origin/main'], cwd=root) != sha:
            raise ValueError('New publication must use the latest fetched complete main')
        batch = read_json(Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json')), {})
        pending = batch.get('pending_new_batch', {})
        if (pending.get('version') != version or pending.get('source_sha') != sha
                or len(set(pending.get('issues', []))) < 3 or not pending.get('qualified_for_new_release')):
            raise ValueError('New publication requires a verified, allocated batch of at least three distinct Issues')
        report = json.loads(run([sys.executable, str(ROOT / 'tools/audit_pending_batch.py'), '--state',
            str(Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json')))], cwd=root))
        if not report.get('allocate_new_version') or report.get('main_sha') != sha or not set(pending['issues']) <= set(report.get('issues', [])):
            raise ValueError('Live batch audit does not qualify this exact main/source batch')
        releases = [r for page in json.loads(run(['gh', 'api', '--paginate', '--slurp', f'repos/{repo}/releases'], cwd=root)) for r in page]
        if any(tuple(map(int, r['tag_name'].lstrip('v').split('.'))) >= tuple(map(int, version.split('.')))
               for r in releases if re.fullmatch(r'v?\d+\.\d+\.\d+', r['tag_name'])):
            raise ValueError('Release version must be strictly greater than published versions')
    return sha


def home():
    return Path(os.environ.get('ZWAI_HOME', Path.home() / '.zwai-swarm'))


def github_platform(root, version, source_sha, platform, state):
    if platform == 'android':
        files = [root / 'bin' / f'zwai-{version}-android.apk']
    else:
        machine = run(['uname', '-m'])
        arch = {'arm64': 'arm64', 'x86_64': 'amd64'}.get(machine)
        if not arch:
            raise ValueError('Unsupported macOS release architecture')
        files = [root / 'bin' / f'zwai-{version}-darwin-{arch}.zip']
    remote = run(['gh', 'api', '--paginate', '--slurp', 'repos/{owner}/{repo}/releases'], cwd=root)
    matches = [r for page in json.loads(remote) for r in page if r['tag_name'] == 'v' + version]
    remote_assets = {a['name']: a for a in matches[0]['assets']} if matches else {}
    if all(p.name in remote_assets for p in files):
        for p in files:
            digest = remote_assets[p.name].get('digest')
            if not digest or not digest.startswith('sha256:'):
                raise ValueError('Published installer has no remote digest; verify it before retry')
            if not p.exists():
                run(['gh', 'release', 'download', 'v' + version, '--pattern', p.name, '--dir', str(p.parent)], cwd=root)
            if digest != 'sha256:' + sha256(p):
                raise ValueError('Retained installer differs from published asset; refusing rebuild/overwrite')
    else:
        target = 'desktop-release' if platform == 'macos' else 'mobile-android-release'
        env = dict(os.environ, ANDROID_ARTIFACT='apk')
        run(['make', target, f'VERSION={version}'], cwd=root, env=env)
        if run(['git', 'status', '--porcelain'], cwd=root):
            raise ValueError('Build changed tracked source; refusing upload')
    if source_preflight(root, version) != source_sha:
        raise ValueError('Source changed during platform build')
    run(['go', 'run', './internal/release/cmd', '-version', version, '-dir', str(root / 'bin'),
         '-platform', platform, '-target', source_sha], cwd=ROOT)
    return {'status': 'published', 'digests': {p.name: sha256(p) for p in files}}


def record_installer(version, source_sha, platform, result):
    path = Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json'))
    delivery = read_json(path, {})
    batch = delivery.get('pending_new_batch', {})
    if batch.get('source_sha') != source_sha or batch.get('version') != version:
        return
    batch[platform] = 'published'
    for number in batch['issues']:
        row = delivery['issues'][str(number)]
        row[platform] = 'published'
        row[f'{platform}_delivery'] = dict(result, source_sha=source_sha, version=version)
    write_json(path, delivery)


def release_notes(root, version, state):
    if not any(state.get(p, {}).get('status') == 'published' for p in ('macos', 'android')):
        return
    body = json.loads(run(['gh', 'release', 'view', 'v' + version, '--json', 'body'], cwd=root))['body']
    start, end = '<!-- zwai-platform-delivery -->', '<!-- /zwai-platform-delivery -->'
    body = re.sub(re.escape(start) + r'.*?' + re.escape(end), '', body, flags=re.S).rstrip()
    rows = [f"- {p}: {state.get(p, {}).get('status', 'pending')}" for p in ('macos', 'android', 'ios')]
    notes = root / 'bin' / 'release-notes.md'
    notes.parent.mkdir(parents=True, exist_ok=True)
    notes.write_text(body + '\n\n' + start + '\n' + '\n'.join(rows) + '\n' + end + '\n')
    run(['gh', 'release', 'edit', 'v' + version, '--notes-file', str(notes)], cwd=root)


def execute(root, version, selected, state_path, check=False):
    source_sha = source_preflight(root, version)
    state = read_json(state_path, {'version': version, 'source_sha': source_sha})
    if state['version'] != version or state['source_sha'] != source_sha:
        raise ValueError('Release state belongs to another version/source SHA')
    from release_ios import IOSRelease
    platforms = ['macos', 'android', 'ios'] if selected == 'all' else [selected]
    failures = []
    for platform in platforms:
        try:
            if platform == 'ios':
                ios = IOSRelease(root, version, source_sha, state)
                result = ios.publish(check=check)
            elif check:
                run(['go', 'run', './internal/release/cmd', '-version', version, '-check'], cwd=root)
                result = {'status': 'preflight_passed'}
            else:
                result = github_platform(root, version, source_sha, platform, state)
            if not check:
                state[platform] = result
                if platform != 'ios' and result['status'] == 'published':
                    record_installer(version, source_sha, platform, result)
            if result['status'] not in ('published', 'preflight_passed'):
                failures.append(platform)
            print(f'{platform}: {result["status"]}', flush=True)
        except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
            failures.append(platform)
            # Preserve previously published evidence; failed verification is separate.
            state.setdefault(platform, {}).setdefault('status', 'failed')
            state[platform]['last_error'] = str(error)
            print(f'{platform}: {error}', file=sys.stderr, flush=True)
        finally:
            if not check:
                state['updated_at'] = datetime.now(ZoneInfo('Asia/Shanghai')).isoformat()
                write_json(state_path, state)
    if not check:
        try:
            release_notes(root, version, state)
        except (OSError, ValueError, RuntimeError) as error:
            failures.append('release_notes')
            state['release_notes_error'] = str(error)
            print(f'release_notes: {error}', file=sys.stderr, flush=True)
        write_json(state_path, state)
    return 1 if failures else 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--platform', choices=['all', 'macos', 'android', 'ios'], default='all')
    parser.add_argument('--check', action='store_true', help='read-only source, signing and remote checks')
    parser.add_argument('--source-dir', type=Path, default=ROOT)
    args = parser.parse_args()
    version_code(args.version)
    directory = home() / 'releases'
    directory.mkdir(parents=True, exist_ok=True)
    with (directory / 'release.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('Another release is active; refusing overlapping publication') from None
        return execute(args.source_dir.resolve(), args.version, args.platform,
                       directory / f'{args.version}.json', args.check)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
