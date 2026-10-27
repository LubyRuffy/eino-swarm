#!/usr/bin/env python3
"""Publish one verified main batch; retain independent platform results."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import subprocess
import sys
import zipfile
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = Path(__file__).resolve().parents[1]
NOT_REQUIRED = {'not_required', 'not_required_by_behavior_change'}
DELIVERED = {'published', 'published_remote_verified', 'published_previous_release', 'delivered'} | NOT_REQUIRED


def delivery_complete(row):
    return all(row.get(platform) in DELIVERED for platform in ('android', 'macos', 'ios', 'server'))


def delivery_batch(delivery, version, source_sha=None):
    current = delivery.get('pending_new_batch', {})
    if current.get('version') == version and (source_sha is None or current.get('source_sha') == source_sha):
        return current, True
    previous = delivery.get('completed_batches', {}).get(version, {}).get('batch', {})
    if previous.get('version') == version and (source_sha is None or previous.get('source_sha') == source_sha):
        return previous, False
    return None, False


def batch_delivery_complete(delivery, batch):
    return (all(delivery['issues'][str(number)]['delivery_complete'] for number in batch['issues'])
            and all(extra.get('status') in DELIVERED
                    for extra in batch.get('additional_platforms', {}).values()))


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


def validate_macos_zip(path):
    """Finder needs both the app icon resource and its plist reference."""
    try:
        with zipfile.ZipFile(path) as archive:
            info = plistlib.loads(archive.read('zwai.app/Contents/Info.plist'))
            if info.get('CFBundleIconFile') != 'AppIcon.icns':
                raise ValueError('macOS installer does not select its Finder icon')
            icon = archive.read('zwai.app/Contents/Resources/AppIcon.icns')
            if len(icon) < 8 or icon[:4] != b'icns' or int.from_bytes(icon[4:8], 'big') != len(icon):
                raise ValueError('macOS installer has an invalid Finder icon')
            offset, large_png = 8, False
            while offset < len(icon):
                if offset + 8 > len(icon):
                    raise ValueError('macOS installer has an invalid Finder icon')
                size = int.from_bytes(icon[offset + 4:offset + 8], 'big')
                if size < 8 or offset + size > len(icon):
                    raise ValueError('macOS installer has an invalid Finder icon')
                if icon[offset:offset + 4] == b'ic10':
                    image = icon[offset + 8:offset + size]
                    large_png = (image[:8] == b'\x89PNG\r\n\x1a\n' and image[12:16] == b'IHDR'
                                 and int.from_bytes(image[16:20], 'big') == 1024
                                 and int.from_bytes(image[20:24], 'big') == 1024)
                offset += size
            if not large_png:
                raise ValueError('macOS installer has no 1024px Finder icon')
    except (KeyError, zipfile.BadZipFile, plistlib.InvalidFileException) as error:
        raise ValueError('macOS installer is missing a valid Finder icon or Info.plist') from error


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


def urgent_release_reason(value):
    if value is None:
        return None
    if not isinstance(value, str) or not value.strip():
        raise ValueError('Urgent release reason must not be blank')
    return value.strip()


def source_preflight(root, version, platforms=('macos', 'android', 'ios'), *, urgent_reason=None):
    urgent_reason = urgent_release_reason(urgent_reason)
    version_code(version)
    if run(['git', 'status', '--porcelain'], cwd=root):
        raise ValueError('Release source checkout must be clean')
    run(['git', 'fetch', 'origin', 'main', '--tags'], cwd=root)
    sha = run(['git', 'rev-parse', 'HEAD'], cwd=root)
    run(['git', 'merge-base', '--is-ancestor', sha, 'origin/main'], cwd=root)
    package = read_json(root / 'mobile/package.json')
    if any(p in platforms for p in ('android', 'ios')) and package['version'] != version:
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
        delivery = read_json(Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json')), {})
        frozen, _ = delivery_batch(delivery, version, sha)
        if frozen and frozen.get('version_locked'):
            workset = frozen.get('finalized_workset', {})
            if (frozen.get('ios') != 'published' or not frozen.get('issues')
                    or workset.get('source_sha') != sha or workset.get('active_issue') is not None
                    or set(workset.get('issues', [])) != set(frozen.get('issues', []))
                    or set(workset.get('platforms_to_release', [])) != set(frozen.get('platforms_to_release', []))
                    or not set(platforms) <= set(frozen.get('platforms_to_release', []))):
                raise ValueError('Frozen public batch has no matching finalized source/platform workset')
            # TestFlight can be the first public channel, before GitHub has a tag.
            from release_ios import IOSRelease, verify_public_receipt
            receipt = read_json(home() / 'releases' / f'{version}.json', {})
            if 'first_publication' in receipt:
                verify_public_receipt(version, sha, receipt,
                                      read_json(home() / 'ios-signing/testflight-releases.json', {}))
            else:
                IOSRelease(root, version, sha, receipt).verify_publication()
            return sha
        if run(['git', 'rev-parse', 'origin/main'], cwd=root) != sha:
            raise ValueError('New publication must use the latest fetched complete main')
        batch = read_json(Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json')), {})
        pending = batch.get('pending_new_batch', {})
        if (pending.get('version') != version or pending.get('source_sha') != sha
                or not pending.get('issues') or not (pending.get('qualified_for_new_release') or urgent_reason)):
            raise ValueError('New publication requires a verified, allocated batch')
        report = json.loads(run([sys.executable, str(ROOT / 'tools/audit_pending_batch.py'), '--state',
            str(Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json')))], cwd=root))
        timing_qualified = report.get('resume_allocated_version') or (
            urgent_reason and report.get('batch_version') == version and report.get('version_locked') is False)
        if (not timing_qualified or report.get('main_sha') != sha
                or set(pending['issues']) != set(report.get('unallocated_issues', []))
                or not set(platforms) <= set(report.get('platforms_to_release', []))):
            raise ValueError('Live batch audit does not qualify this exact main/source batch')
        if batch.get('active_issue') is not None:
            raise ValueError('Issue integration is still in progress; finalize the workset before publication')
        workset = pending.get('finalized_workset', {})
        if (workset.get('source_sha') != sha or workset.get('active_issue') is not None
                or set(workset.get('issues', [])) != set(report['unallocated_issues'])
                or set(workset.get('platforms_to_release', [])) != set(report['platforms_to_release'])):
            raise ValueError('New publication requires a finalized workset for this source and all required platforms')
        releases = [r for page in json.loads(run(['gh', 'api', '--paginate', '--slurp', f'repos/{repo}/releases'], cwd=root)) for r in page]
        if any(tuple(map(int, r['tag_name'].lstrip('v').split('.'))) >= tuple(map(int, version.split('.')))
               for r in releases if re.fullmatch(r'v?\d+\.\d+\.\d+', r['tag_name'])):
            raise ValueError('Release version must be strictly greater than published versions')
    return sha


def home():
    return Path(os.environ.get('ZWAI_HOME', Path.home() / '.zwai-swarm'))


def github_platform(root, version, source_sha, platform, state, *, urgent_reason=None):
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
        delivery = read_json(Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json')), {})
        batch, _ = delivery_batch(delivery, version, None if platform == 'android' else source_sha)
        native = (batch or {}).get('release_native_validation', {}) if platform == 'macos' else {}
        if platform == 'android' and ('release_android_validation' in (batch or {})
                or (batch or {}).get('android') == 'ready_for_publication'):
            asset = files[0]
            accepted = (batch or {}).get('release_android_validation')
            if (not isinstance(accepted, dict) or batch.get('source_sha') != source_sha
                    or accepted.get('status') != 'passed'
                    or accepted.get('version') != version or accepted.get('source_sha') != source_sha
                    or accepted.get('asset_name') != asset.name or not asset.is_file()
                    or sha256(asset) != accepted.get('asset_sha256')):
                raise ValueError('The accepted Android package is missing or differs from native-device evidence')
        elif platform == 'macos' and (native or (batch or {}).get('macos') == 'ready_for_publication'):
            asset = files[0]
            if (native.get('status') != 'passed' or native.get('version') != version
                    or native.get('source_sha') != source_sha or native.get('asset_name') != asset.name
                    or not asset.is_file() or sha256(asset) != native.get('asset_sha256')):
                raise ValueError('The accepted macOS package is missing or differs from native-window evidence')
        else:
            target = 'desktop-release' if platform == 'macos' else 'mobile-android-release'
            env = dict(os.environ, ANDROID_ARTIFACT='apk')
            run(['make', target, f'VERSION={version}'], cwd=root, env=env)
            if run(['git', 'status', '--porcelain'], cwd=root):
                raise ValueError('Build changed tracked source; refusing upload')
        if platform == 'macos':
            validate_macos_zip(files[0])
    if source_preflight(root, version, (platform,), urgent_reason=urgent_reason) != source_sha:
        raise ValueError('Source changed during platform build')
    run(['go', 'run', './internal/release/cmd', '-version', version, '-dir', str(root / 'bin'),
         '-platform', platform, '-target', source_sha], cwd=ROOT)
    return {'status': 'published', 'digests': {p.name: sha256(p) for p in files}}


def record_installer(version, source_sha, platform, result):
    path = Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json'))
    delivery = read_json(path, {})
    batch, current = delivery_batch(delivery, version, source_sha)
    if not batch:
        return
    batch[platform] = 'published'
    batch['version_locked'] = True
    if platform in batch.get('additional_platforms', {}):
        batch['additional_platforms'][platform]['status'] = 'published'
    if current:
        delivery['last_published_source_sha'] = source_sha
    for number in batch['issues']:
        row = delivery['issues'][str(number)]
        row['version'] = row['batch_version'] = version
        row['release_source_sha'] = source_sha
        if row.get(platform) not in NOT_REQUIRED:
            row[platform] = 'published'
            row[f'{platform}_delivery'] = dict(result, source_sha=source_sha, version=version)
        row['delivery_complete'] = delivery_complete(row)
    batch['publication'] = ('platform_delivery_verified' if batch_delivery_complete(delivery, batch)
                            else 'platform_delivery_in_progress')
    write_json(path, delivery)


def release_notes(root, version, state):
    if not any(state.get(p, {}).get('status') == 'published' for p in ('macos', 'android')):
        return
    body = json.loads(run(['gh', 'release', 'view', 'v' + version, '--json', 'body'], cwd=root))['body']
    start, end = '<!-- zwai-platform-delivery -->', '<!-- /zwai-platform-delivery -->'
    body = re.sub(re.escape(start) + r'.*?' + re.escape(end), '', body, flags=re.S).rstrip()
    required = batch_platforms(version, pending_only=False)
    rows = [f"- {p}: {state.get(p, {}).get('status', 'pending') if required is None or p in required else 'not_required'}"
            for p in ('macos', 'android', 'ios')]
    notes = root / 'bin' / 'release-notes.md'
    notes.parent.mkdir(parents=True, exist_ok=True)
    notes.write_text(body + '\n\n' + start + '\n' + '\n'.join(rows) + '\n' + end + '\n')
    run(['gh', 'release', 'edit', 'v' + version, '--notes-file', str(notes)], cwd=root)


def batch_platforms(version, pending_only):
    delivery = read_json(Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json')), {})
    batch, _ = delivery_batch(delivery, version)
    if not batch or not batch.get('issues'):
        return None
    excluded = DELIVERED if pending_only else NOT_REQUIRED
    return [platform for platform in ('macos', 'android', 'ios') if (
        any(delivery.get('issues', {}).get(str(number), {}).get(platform) not in excluded
            for number in batch['issues'])
        or batch.get('additional_platforms', {}).get(platform, {}).get('status', 'not_required') not in excluded)]


def selected_platforms(version, selected):
    if selected != 'all':
        return [selected]
    pending = batch_platforms(version, pending_only=True)
    return ['macos', 'android', 'ios'] if pending is None else pending


def platform_acceptance_gate(version, platform):
    delivery = read_json(Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json')), {})
    batch, _ = delivery_batch(delivery, version)
    if not batch:
        return
    extra = batch.get('additional_platforms', {}).get(platform, {})
    if extra.get('status', '').startswith('pending_native_'):
        raise ValueError(f'{platform} native acceptance is still pending for this batch')
    for number in batch.get('issues', []):
        status = delivery.get('issues', {}).get(str(number), {}).get(platform, '')
        if isinstance(status, str) and status.startswith('pending_native_'):
            raise ValueError(f'Issue #{number} {platform} native acceptance is still pending')


def execute(root, version, selected, state_path, check=False, *, urgent_reason=None):
    urgent_reason = urgent_release_reason(urgent_reason)
    platforms = selected_platforms(version, selected)
    source_sha = source_preflight(root, version, tuple(platforms), urgent_reason=urgent_reason)
    state = read_json(state_path, {'version': version, 'source_sha': source_sha})
    if state['version'] != version or state['source_sha'] != source_sha:
        raise ValueError('Release state belongs to another version/source SHA')
    if urgent_reason:
        state['urgent_reason'] = urgent_reason
    from release_ios import IOSRelease
    delivery = read_json(Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json')), {})
    frozen, _ = delivery_batch(delivery, version, source_sha)
    if (not check and frozen and frozen.get('version_locked') and frozen.get('ios') == 'published'
            and 'first_publication' not in state
            and not run(['git', 'ls-remote', 'origin', f'refs/tags/v{version}'], cwd=root)):
        state['first_publication'] = IOSRelease(root, version, source_sha, state).verify_publication()
        write_json(state_path, state)
    failures = []
    for platform in platforms:
        try:
            if not check:
                platform_acceptance_gate(version, platform)
            if platform == 'ios':
                ios = IOSRelease(root, version, source_sha, state, urgent_reason=urgent_reason)
                result = ios.publish(check=check)
            elif check:
                run(['go', 'run', './internal/release/cmd', '-version', version, '-check'], cwd=root)
                result = {'status': 'preflight_passed'}
            else:
                result = github_platform(root, version, source_sha, platform, state, urgent_reason=urgent_reason)
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
    parser.add_argument('--verify', action='store_true', help='local resumable checks/package only; never publish')
    parser.add_argument('--plan', action='store_true', help='print the local verification plan without running it')
    parser.add_argument('--evidence', type=Path, help='private verification directory outside the source checkout')
    parser.add_argument('--jobs', type=int, choices=(1, 2), default=2, help='bounded independent verification stages')
    parser.add_argument('--e2e-port', type=int, help='isolated loopback port for this verification run')
    parser.add_argument('--source-dir', type=Path, default=ROOT)
    parser.add_argument('--urgent-reason', type=urgent_release_reason,
                        help='explicit user-authorized urgent release; bypass timing policy only')
    args = parser.parse_args()
    version_code(args.version)
    if args.verify:
        if args.check or args.platform != 'macos' or (not args.plan and args.evidence is None):
            parser.error('--verify requires --platform macos and --evidence; it cannot be combined with --check')
        from release_verify import plan, verify
        if args.plan:
            print(json.dumps(plan(args.source_dir, args.version, args.evidence or Path('/private/evidence'), args.jobs), indent=2))
            return 0
        return verify(args.source_dir, args.version, args.evidence, jobs=args.jobs, e2e_port=args.e2e_port)
    if args.plan or args.evidence is not None or args.e2e_port is not None:
        parser.error('--plan, --evidence and --e2e-port require --verify')
    directory = home() / 'releases'
    directory.mkdir(parents=True, exist_ok=True)
    with (directory / 'release.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('Another release is active; refusing overlapping publication') from None
        return execute(args.source_dir.resolve(), args.version, args.platform,
                       directory / f'{args.version}.json', args.check, urgent_reason=args.urgent_reason)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
