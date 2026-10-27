#!/usr/bin/env python3
"""Read-only, offline release-input checks; never install, build or run tests.

``collect_preflight`` is also used by the timed release orchestrator. A passed
receipt describes the sampled checkout; consumers must recheck before mutation.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import time


CORE_MODULES = ('react', 'react-dom', 'typescript', 'vite', 'vitest', '@playwright/test')
INPUT_FILES = ('go.mod', 'go.sum', 'frontend/package.json', 'frontend/package-lock.json',
               'mobile/package.json', 'mobile/package-lock.json')


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def within(path, root):
    return path == root or root in path.parents


def offline_environment():
    """Do not inherit Node injection or npm credentials/configuration."""
    # These selection inputs are also inherited by release_stages.py. Keep the
    # compiler/SDK probe on the very same developer selection as the build.
    keys = ('PATH', 'HOME', 'TMPDIR', 'TMP', 'TEMP', 'SYSTEMROOT', 'COMSPEC',
            'DEVELOPER_DIR', 'SDKROOT')
    env = {key: os.environ[key] for key in keys if key in os.environ}
    env.update(npm_config_offline='true', npm_config_yes='false',
               npm_config_ignore_scripts='true', npm_config_userconfig=os.devnull,
               PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD='1', FORCE_COLOR='0',
               GIT_OPTIONAL_LOCKS='0', GOTOOLCHAIN='local', GOPROXY='off',
               GOSUMDB='off', GOENV='off', GOWORK='off')
    return env


def command(args, root, timeout=30, *, env=None):
    """Bound only our command tree; retain hashes, never raw diagnostic text."""
    started = time.monotonic()
    try:
        process = subprocess.Popen(args, cwd=root, env=offline_environment() if env is None else env,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=os.name == 'posix')
    except OSError:
        return {'status': 'unavailable', 'returncode': None, 'stdout': '', 'stderr': '',
                'elapsed_seconds': time.monotonic() - started}
    joined = False
    try:
        try:
            stdout, stderr = process.communicate(timeout=timeout)
            status = 'passed' if process.returncode == 0 else 'failed'
        except subprocess.TimeoutExpired:
            stdout, stderr = stop_owned_command(process)
            status = 'timeout'
        joined = True
    finally:
        if not joined:
            original_exception = sys.exc_info()[0] is not None
            try:
                stop_owned_command(process)
            except BaseException:
                if not original_exception:
                    raise
    return {'status': status, 'returncode': process.returncode,
            'stdout': stdout, 'stderr': stderr,
            'elapsed_seconds': time.monotonic() - started}


def stop_owned_command(process):
    """Cancel only this new session, drain its pipes and reap its Popen child."""
    if os.name == 'posix':
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    elif process.poll() is None:
        process.kill()
    return process.communicate()


def toolchain_override_check():
    unsupported = [name for name in ('CC', 'CXX', 'GOROOT') if os.environ.get(name, '').strip()]
    if os.environ.get('GOTOOLCHAIN', '').strip() not in ('', 'local'):
        unsupported.append('GOTOOLCHAIN')
    return check('build_environment', not unsupported, 'unsupported_toolchain_override' if unsupported
                 else 'default_toolchain_only', variables=unsupported)


def command_evidence(result):
    return {key: result[key] for key in ('status', 'returncode', 'elapsed_seconds')} | {
        'stdout_sha256': hashlib.sha256(result['stdout'].encode()).hexdigest(),
        'stderr_sha256': hashlib.sha256(result['stderr'].encode()).hexdigest()}


def check(name, passed, reason, **details):
    return {'name': name, 'status': 'passed' if passed else 'blocked',
            'reason': reason, **details}


def source_check(root, expected_sha):
    if not re.fullmatch(r'[0-9a-f]{40}|[0-9a-f]{64}', expected_sha):
        return check('source', False, 'full_source_sha_required')
    # Go's automatic VCS metadata is not reliable for the linked-worktree
    # layout seen in the delayed release. Require a standalone exact checkout.
    if not (root / '.git').is_dir() or (root / '.git').is_symlink():
        return check('source', False, 'standalone_git_directory_required')
    queries = {
        'top': ['git', 'rev-parse', '--show-toplevel'],
        'head': ['git', 'rev-parse', '--verify', 'HEAD^{commit}'],
        'status': ['git', 'status', '--porcelain=v1', '--untracked-files=all'],
        'common': ['git', 'rev-parse', '--git-common-dir'],
        'gitdir': ['git', 'rev-parse', '--git-dir'],
    }
    results = {key: command([args[0], '-c', 'core.fsmonitor=false', *args[1:]], root)
               for key, args in queries.items()}
    if any(result['status'] != 'passed' for result in results.values()):
        return check('source', False, 'git_identity_unavailable')
    values = {key: result['stdout'].strip() for key, result in results.items()}
    if Path(values['top']).resolve() != root:
        return check('source', False, 'checkout_root_mismatch')
    if (root / values['common']).resolve() != (root / values['gitdir']).resolve():
        return check('source', False, 'linked_worktree_vcs_risk')
    if values['head'] != expected_sha:
        return check('source', False, 'source_sha_mismatch', observed_sha=values['head'])
    if values['status']:
        return check('source', False, 'checkout_not_clean', source_sha=values['head'])
    return check('source', True, 'clean_exact_standalone_commit', source_sha=values['head'])


def dependency_links(project, root):
    modules = project / 'node_modules'
    if modules.is_symlink():
        return check('dependency_links', False, 'dependency_root_symlink')
    if not modules.is_dir():
        return check('dependency_links', False, 'dependencies_missing')
    count = 0
    def inventory_error(error):
        raise error
    try:
        for current, dirs, files in os.walk(modules, followlinks=False, onerror=inventory_error):
            for name in dirs + files:
                path = Path(current) / name
                if not path.is_symlink():
                    continue
                count += 1
                try:
                    target = path.resolve(strict=True)
                except (OSError, RuntimeError):
                    return check('dependency_links', False, 'dependency_link_unresolved',
                                 path=str(path.relative_to(root)))
                if not within(target, root):
                    return check('dependency_links', False, 'dependency_link_outside_checkout',
                                 path=str(path.relative_to(root)))
                if target.is_dir() and within(path.parent.resolve(), target):
                    return check('dependency_links', False, 'dependency_link_to_ancestor',
                                 path=str(path.relative_to(root)))
    except OSError:
        return check('dependency_links', False, 'dependency_inventory_unreadable')
    return check('dependency_links', True, 'links_owned_by_checkout', symlinks=count)


RESOLVE_SCRIPT = r'''
const fs = require('node:fs');
const {createRequire} = require('node:module');
const req = createRequire(process.argv[1]);
const names = JSON.parse(process.argv[2]);
const results = names.map(name => {
  try { return {name, path: fs.realpathSync(req.resolve(name))}; }
  catch (_) { return {name, error: 'module_not_resolved'}; }
});
process.stdout.write(JSON.stringify(results));
'''


LOAD_SCRIPT = r'''
const fs = require('node:fs');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const root = fs.realpathSync(process.argv[1]);
const entries = JSON.parse(process.argv[2]);
(async () => {
  const results = [];
  for (const entry of entries) {
    try {
      const actual = fs.realpathSync(entry.path);
      const relative = path.relative(root, actual);
      if (relative === '..' || relative.startsWith('..' + path.sep) || path.isAbsolute(relative)) {
        results.push({name: entry.name, error: 'entry_outside_checkout'});
        continue;
      }
      await import(pathToFileURL(actual).href);
      results.push({name: entry.name, path: actual, loaded: true});
    } catch (_) { results.push({name: entry.name, error: 'module_load_failed'}); }
  }
  process.stdout.write(JSON.stringify(results));
})().catch(() => { process.exitCode = 1; });
'''


def dependency_check(project, root, node, core_modules=CORE_MODULES):
    try:
        manifest = json.loads((project / 'package.json').read_text())
        lock = json.loads((project / 'package-lock.json').read_text())
    except (OSError, ValueError):
        return check('dependencies', False, 'package_or_lockfile_unreadable')
    names = sorted(set(manifest.get('dependencies', {})) | set(manifest.get('devDependencies', {})))
    packages, missing, cli, core_clis, bin_paths = {}, [], None, {}, {}
    for name in names:
        # Package names are data, and must not turn into filesystem traversal.
        if not re.fullmatch(r'(?:@[A-Za-z0-9_.-]+/)?[A-Za-z0-9_.-]+', name) or name in ('.', '..'):
            return check('dependencies', False, 'invalid_package_name')
        path = project / 'node_modules' / name / 'package.json'
        try:
            if not within(path.resolve(strict=True), root):
                return check('dependencies', False, 'package_manifest_outside_checkout', package=name)
            package = json.loads(path.read_text())
            version = package['version']
            locked = lock.get('packages', {}).get('node_modules/' + name, {}).get('version')
            if not isinstance(version, str) or not locked or locked != version:
                return check('dependencies', False, 'installed_version_mismatch', package=name)
            bins = package.get('bin', {})
            if isinstance(bins, str):
                bins = {name.rsplit('/', 1)[-1]: bins}
            if not isinstance(bins, dict):
                return check('dependencies', False, 'invalid_package_bin', package=name)
            bin_hashes = {}
            for executable, relative in bins.items():
                if not isinstance(executable, str) or not re.fullmatch(r'[A-Za-z0-9_.-]+', executable):
                    return check('dependencies', False, 'invalid_package_bin', package=name)
                target = (path.parent / relative).resolve(strict=True)
                if not within(target, root) or not target.is_file():
                    return check('dependencies', False, 'package_bin_outside_checkout', package=name)
                link = project / 'node_modules' / '.bin' / executable
                if not link.is_file() or link.resolve(strict=True) != target:
                    return check('dependencies', False, 'package_bin_missing_or_wrong', package=name)
                if os.name == 'posix' and not os.access(target, os.X_OK):
                    return check('dependencies', False, 'package_bin_not_executable', package=name)
                if name == '@playwright/test' and executable == 'playwright':
                    cli = target
                if name == 'vitest' and executable == 'vitest':
                    core_clis[name] = target
                bin_hashes[executable] = digest(target)
                bin_paths[name, executable] = target
            packages[name] = {'version': version, 'manifest_sha256': digest(path),
                              'bin_sha256': bin_hashes}
        except (OSError, ValueError, KeyError, TypeError, RuntimeError):
            missing.append(name)
    if missing:
        return check('dependencies', False, 'declared_dependencies_missing_or_invalid', packages=missing)
    if not node:
        return check('dependencies', False, 'node_unavailable')
    cores = [name for name in core_modules if name in names]
    result = command([node, '-e', RESOLVE_SCRIPT, str(project / 'package.json'), json.dumps(cores)], project)
    try:
        resolved = json.loads(result['stdout']) if result['status'] == 'passed' else None
        if not isinstance(resolved, list) or len(resolved) != len(cores):
            raise ValueError()
        for expected, row in zip(cores, resolved):
            if row.get('name') != expected or 'error' in row:
                return check('dependencies', False, 'core_module_not_resolved', package=expected)
            target = Path(row['path'])
            if not target.is_absolute() or not within(target.resolve(strict=True), root):
                return check('dependencies', False, 'core_module_outside_checkout', package=expected)
    except (ValueError, KeyError, TypeError, OSError, RuntimeError):
        return check('dependencies', False, 'module_resolution_unavailable', evidence=command_evidence(result))
    # Resolution alone does not establish that an entry parses or can load its
    # imports. Load the verified real files; Vitest's worker-only API instead
    # gets its package-owned CLI's read-only --version path.
    try:
        entry_hashes = {row['name']: digest(row['path']) for row in resolved}
    except OSError:
        return check('dependencies', False, 'core_entry_unreadable')
    imports = [row for row in resolved if row['name'] != 'vitest']
    loaded = command([node, '-e', LOAD_SCRIPT, str(root), json.dumps(imports)], project)
    modes = {}
    try:
        rows = json.loads(loaded['stdout']) if loaded['status'] == 'passed' else None
        if not isinstance(rows, list) or len(rows) != len(imports):
            raise ValueError()
        for expected, row in zip(imports, rows):
            if row.get('name') != expected['name'] or row.get('loaded') is not True or row.get('path') != expected['path']:
                return check('dependencies', False, 'core_module_load_failed',
                             package=expected['name'], evidence=command_evidence(loaded))
            modes[row['name']] = 'dynamic_import_of_verified_entry'
    except (ValueError, KeyError, TypeError):
        return check('dependencies', False, 'core_module_load_unavailable', evidence=command_evidence(loaded))
    if 'vitest' in cores:
        executable = core_clis.get('vitest')
        if not executable:
            return check('dependencies', False, 'core_cli_missing', package='vitest')
        version = command([node, str(executable), '--version'], project, timeout=15)
        expected_version = r'\bvitest/' + re.escape(packages['vitest']['version']) + r'(?:\s|$)'
        if version['status'] != 'passed' or not re.search(expected_version, version['stdout']):
            return check('dependencies', False, 'core_cli_probe_failed', package='vitest',
                         evidence=command_evidence(version))
        modes['vitest'] = 'local_cli_version_only_worker_api_not_imported'
    try:
        if any(digest(row['path']) != entry_hashes[row['name']] for row in resolved):
            return check('dependencies', False, 'core_entry_changed_during_probe')
        if any(digest(path) != packages[name]['bin_sha256'][executable]
               for (name, executable), path in bin_paths.items()):
            return check('dependencies', False, 'dependency_bin_changed_during_probe')
    except OSError:
        return check('dependencies', False, 'core_entry_disappeared_during_probe')
    return check('dependencies', True, 'declared_packages_core_load_and_cli_verified',
                 packages=packages, resolved_modules={row['name']: str(Path(row['path']).relative_to(root))
                                                     for row in resolved},
                 core_entry_sha256=entry_hashes, core_load_modes=modes,
                 playwright_cli=str(cli.relative_to(root)) if cli else None,
                 integrity_scope='declared_manifests_bins_core_entries_and_lockfile')


def e2e_collection(project, root, node, dependency_result):
    cli = dependency_result.get('playwright_cli')
    if not node or not cli:
        return check('e2e_collection', False, 'local_playwright_cli_missing')
    # Direct local CLI only: npx is intentionally never used, even with flags.
    result = command([node, str(root / cli), 'test', '--list', '--reporter=json'], project, timeout=60)
    try:
        report = json.loads(result['stdout']) if result['status'] == 'passed' else None
        if not isinstance(report, dict) or report.get('errors'):
            raise ValueError()
        def count_specs(suite):
            return len(suite.get('specs', [])) + sum(count_specs(child) for child in suite.get('suites', []))
        count = sum(count_specs(suite) for suite in report.get('suites', []))
        if count == 0:
            return check('e2e_collection', False, 'no_e2e_tests_collected', evidence=command_evidence(result))
    except (ValueError, TypeError, AttributeError):
        return check('e2e_collection', False, 'e2e_collection_failed', evidence=command_evidence(result))
    return check('e2e_collection', True, 'local_cli_collected_tests_without_execution',
                 collected_specs=count, evidence=command_evidence(result))


def input_fingerprints(root):
    return {name: {'sha256': digest(root / name), 'bytes': (root / name).stat().st_size}
            for name in INPUT_FILES if (root / name).is_file()}


def macos_toolchain_fingerprint(root, xcrun=None, developer_selector=None):
    """Read selected clang/SDK without compilation, downloads or installs."""
    xcrun = xcrun or shutil.which('xcrun')
    developer_selector = developer_selector or shutil.which('xcode-select')
    if not xcrun or not developer_selector:
        return {'available': False, 'reason': 'macos_selection_tools_missing'}
    environment = offline_environment()
    probes = {}
    queries = {'clang': [xcrun, '--no-cache', '--find', 'clang'],
               'sdk_path': [xcrun, '--no-cache', '--show-sdk-path'],
               'sdk_version': [xcrun, '--no-cache', '--show-sdk-version'],
               'developer_dir': [developer_selector, '-p']}
    for name, args in queries.items():
        result = command(args, root, timeout=15, env=environment)
        probes[name] = result
        if result['status'] != 'passed':
            return {'available': False, 'reason': 'macos_selection_probe_failed',
                    'failed_probe': name, 'evidence': command_evidence(result)}
    try:
        paths = {}
        for name in ('clang', 'sdk_path', 'developer_dir'):
            path = Path(probes[name]['stdout'].strip())
            if not path.is_absolute():
                raise ValueError()
            paths[name] = path.resolve(strict=True)
        if not paths['clang'].is_file() or not paths['sdk_path'].is_dir() or not paths['developer_dir'].is_dir():
            raise ValueError()
        sdk_version = probes['sdk_version']['stdout'].strip()
        if not re.fullmatch(r'\d+(?:\.\d+){0,3}', sdk_version):
            raise ValueError()
        version = command([str(paths['clang']), '--version'], root, timeout=15, env=environment)
        if version['status'] != 'passed':
            return {'available': False, 'reason': 'selected_clang_version_unavailable',
                    'evidence': command_evidence(version)}
        settings = {name: digest(paths['sdk_path'] / name)
                    for name in ('SDKSettings.json', 'SDKSettings.plist')
                    if (paths['sdk_path'] / name).is_file()}
        selection = {key: environment.get(key) for key in ('DEVELOPER_DIR', 'SDKROOT')}
        return {'available': True, 'reason': 'selected_clang_and_sdk_verified',
                'clang_path': str(paths['clang']), 'clang_binary_sha256': digest(paths['clang']),
                'clang_version_sha256': hashlib.sha256((version['stdout'] + '\0' + version['stderr']).encode()).hexdigest(),
                'developer_dir': str(paths['developer_dir']), 'sdk_path': str(paths['sdk_path']),
                'sdk_version': sdk_version, 'sdk_settings_sha256': settings,
                'selection_environment_sha256': hashlib.sha256(json.dumps(selection, sort_keys=True).encode()).hexdigest(),
                'evidence': {name: command_evidence(result) for name, result in probes.items()}}
    except (OSError, RuntimeError, ValueError):
        return {'available': False, 'reason': 'macos_selection_identity_invalid'}


def default_compiler_fingerprints(root, go):
    """Bind Go's default CC/CXX to the same PATH inherited by fixed stages."""
    result = command([go, 'env', 'CC', 'CXX'], root, timeout=15)
    names = result['stdout'].strip().splitlines()
    if result['status'] != 'passed' or len(names) != 2:
        return {name: {'available': False, 'reason': 'default_compiler_names_unavailable'}
                for name in ('cc', 'cxx')}
    tools = {}
    for role, name in zip(('cc', 'cxx'), names):
        found = shutil.which(name, path=offline_environment().get('PATH', os.defpath))
        if not found:
            tools[role] = {'available': False, 'reason': 'default_compiler_missing'}
            continue
        path = Path(found).resolve()
        tools[role] = {'available': True, 'default_name': name, 'path': str(path),
                       'binary_sha256': digest(path)}
    return tools


def toolchain_fingerprints(root, *, include_macos=True):
    tools = {}
    for name, args in (('git', ['--version']), ('node', ['--version']),
                       ('npm', ['--version']), ('go', ['version'])):
        found = shutil.which(name)
        if not found:
            tools[name] = {'available': False}
            continue
        path = Path(found).resolve()
        if name == 'npm':
            # Even npm --version can create logs in the user's cache. Identify
            # its installed entry and descriptor without starting npm at all.
            candidates = (path.parent.parent / 'package.json',
                          path.parent / 'node_modules/npm/package.json')
            npm = None
            for descriptor in candidates:
                try:
                    value = json.loads(descriptor.read_text())
                    if value.get('name') == 'npm' and re.fullmatch(r'\d+\.\d+\.\d+(?:-[\w.-]+)?', value['version']):
                        npm = value
                        break
                except (OSError, ValueError, KeyError, TypeError):
                    pass
            tools[name] = {'available': npm is not None, 'path': str(path),
                           'binary_sha256': digest(path), 'probe': 'installed_npm_descriptor',
                           'version': npm['version'] if npm else None}
            continue
        result = command([str(path), *args], root, timeout=15)
        # Version text can contain arbitrary executable output; retain its digest.
        tools[name] = {'available': result['status'] == 'passed', 'path': str(path),
                       'binary_sha256': digest(path), **command_evidence(result)}
        version = re.search(r'\d+\.\d+(?:\.\d+)?', result['stdout'])
        tools[name]['version'] = version.group(0) if version else None
    tools['python'] = {'available': True, 'path': str(Path(sys.executable).resolve()),
                       'version': sys.version.split()[0], 'binary_sha256': digest(sys.executable)}
    for name in ('codesign', 'xcrun', 'xcode-select', 'ditto', 'gh'):
        found = shutil.which(name)
        tools[name] = ({'available': True, 'path': str(Path(found).resolve()),
                        'binary_sha256': digest(found)} if found else {'available': False})
    tools['macos'] = ({'available': False, 'reason': 'not_macos'} if sys.platform != 'darwin' else
                      macos_toolchain_fingerprint(root) if include_macos else
                      {'available': False, 'reason': 'selection_probe_deferred'})
    if tools.get('go', {}).get('available'):
        tools.update(default_compiler_fingerprints(root, tools['go']['path']))
    else:
        tools.update({name: {'available': False, 'reason': 'go_unavailable'} for name in ('cc', 'cxx')})
    return tools


def stable_inputs(report):
    """Cache/recheck identity independent of command timing and E2E collection."""
    dependency_keys = ('status', 'reason', 'packages', 'resolved_modules', 'playwright_cli',
                       'core_entry_sha256', 'core_load_modes', 'integrity_scope')
    dependencies = {row['project']: {key: row[key] for key in dependency_keys if key in row}
                    for row in report.get('checks', []) if row['name'] == 'dependencies'}
    tool_keys = ('available', 'path', 'binary_sha256', 'version', 'clang_path',
                 'clang_binary_sha256', 'clang_version_sha256', 'developer_dir',
                 'sdk_path', 'sdk_version', 'sdk_settings_sha256', 'selection_environment_sha256',
                 'default_name')
    return {'schema_version': 1, 'checkout': report['checkout'],
            'source': {key: report['source'].get(key) for key in ('status', 'reason', 'source_sha')},
            'expected_source_sha': report['expected_source_sha'],
            'inputs': report['inputs'], 'dependencies': dependencies,
            'toolchain': {name: {key: row[key] for key in tool_keys if key in row}
                          for name, row in report['toolchain'].items()}}


def stable_fingerprint(report):
    encoded = json.dumps(stable_inputs(report), sort_keys=True, separators=(',', ':')).encode()
    return hashlib.sha256(encoded).hexdigest()


def collect_preflight(root, expected_source_sha, *, projects=('frontend',), collect_e2e=True):
    """Return a credential-free receipt; missing inputs block without installing."""
    root = Path(root).resolve()
    started = time.monotonic()
    source = source_check(root, expected_source_sha)
    environment_check = toolchain_override_check()
    checks = [source, environment_check]
    tools = toolchain_fingerprints(root, include_macos=False)
    required = ['git', 'node', 'npm'] + (['go', 'cc', 'cxx'] if (root / 'go.mod').is_file() else [])
    checks.append(check('toolchain', all(tools[name]['available'] for name in required),
                        'required_tools_checked', required=required))
    for name in projects:
        project = root / name
        if not within(project.resolve(), root):
            checks.append(check('project', False, 'project_outside_checkout'))
            continue
        links = dependency_links(project, root)
        links['project'] = name
        checks.append(links)
        if links['status'] != 'passed':
            continue
        if source['status'] != 'passed' or environment_check['status'] != 'passed':
            continue
        dependencies = dependency_check(project, root, tools['node'].get('path'))
        dependencies['project'] = name
        checks.append(dependencies)
        if collect_e2e and source['status'] == 'passed' and dependencies['status'] == 'passed':
            collected = e2e_collection(project, root, tools['node'].get('path'), dependencies)
            collected['project'] = name
            checks.append(collected)
    # xcrun can invoke xcodebuild internally and be expensive even for a read.
    # A cheap refusal must never pay that cost or load further build tools.
    if sys.platform == 'darwin' and all(row['status'] == 'passed' for row in checks):
        tools['macos'] = macos_toolchain_fingerprint(root)
        checks.append(check('macos_toolchain', tools['macos']['available'],
                            tools['macos']['reason']))
    report = {'schema_version': 1,
              'status': 'passed' if all(c['status'] == 'passed' for c in checks) else 'blocked',
              'checkout': str(root), 'expected_source_sha': expected_source_sha,
              'source': source, 'checks': checks, 'inputs': input_fingerprints(root),
              'toolchain': tools, 'elapsed_seconds': time.monotonic() - started,
              'scope': 'read_only_offline_preflight', 'e2e_collection_requested': collect_e2e}
    report['binding'] = stable_inputs(report)
    report['binding_sha256'] = stable_fingerprint(report)
    return report


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--checkout', type=Path, required=True)
    parser.add_argument('--source-sha', required=True)
    parser.add_argument('--project', action='append')
    parser.add_argument('--check-only', action='store_true', help='Read-only mode (also the only mode).')
    parser.add_argument('--skip-e2e-collection', action='store_true')
    args = parser.parse_args(argv)
    result = collect_preflight(args.checkout, args.source_sha,
                               projects=tuple(args.project or ['frontend']),
                               collect_e2e=not args.skip_e2e_collection)
    print(json.dumps(result, sort_keys=True, indent=2))
    return 0 if result['status'] == 'passed' else 2


if __name__ == '__main__':
    raise SystemExit(main())
