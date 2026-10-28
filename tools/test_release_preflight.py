"""Private repositories and tiny Node packages exercise the actual boundaries."""
import contextlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest
from unittest.mock import Mock, patch

import release_preflight as preflight


@unittest.skipUnless(shutil.which('git') and shutil.which('node'), 'git and Node fixtures required')
class ReleasePreflightTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='release-preflight-test-')
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name).resolve()
        self.root = self.base / 'checkout'
        self.project = self.root / 'frontend'
        self.modules = self.project / 'node_modules'
        self.modules.mkdir(parents=True)
        self.node = shutil.which('node')
        # These are Node/Git fixtures, not repeated Xcode SDK probes. Dedicated
        # tests below exercise selection with real private files and mock tools.
        self.real_macos_probe = preflight.macos_toolchain_fingerprint
        macos = patch.object(preflight, 'macos_toolchain_fingerprint', return_value={
            'available': True, 'reason': 'private_fixture_selection'})
        macos.start()
        self.addCleanup(macos.stop)
        self.git('init', '-q')
        (self.root / '.gitignore').write_text('**/node_modules/\n')
        self.manifest = {'name': 'fixture', 'dependencies': {'react': '1.0.0'},
                         'devDependencies': {'@playwright/test': '1.0.0'}}
        self.lock = {'lockfileVersion': 3, 'packages': {}}
        self.package('react')
        self.package('@playwright/test', cli=True)
        self.write_manifests()
        self.commit()

    def git(self, *args):
        return subprocess.check_output(['git', '-c', 'user.name=Fixture', '-c',
            'user.email=fixture@example.invalid', *args], cwd=self.root, text=True,
            stderr=subprocess.DEVNULL).strip()

    def commit(self):
        self.git('add', '.')
        self.git('commit', '-qm', 'private fixture')
        self.sha = self.git('rev-parse', 'HEAD')

    def package(self, name, cli=False):
        directory = self.modules / name
        directory.mkdir(parents=True)
        data = {'name': name, 'version': '1.0.0', 'main': 'index.js'}
        (directory / 'index.js').write_text('module.exports = {};\n')
        if cli:
            data['bin'] = {'playwright': 'cli.js'}
            target = directory / 'cli.js'
            # The fixture refuses any invocation that could execute tests. The
            # real Node process checks arguments, environment and JSON handling.
            target.write_text("""#!/usr/bin/env node
if (process.argv.slice(2).join(' ') !== 'test --list --reporter=json') process.exit(61);
if (process.env.npm_config_offline !== 'true' || process.env.npm_config_yes !== 'false') process.exit(62);
if (process.env.NODE_OPTIONS || process.env.NODE_PATH || process.env.PREFLIGHT_TEST_SECRET) process.exit(63);
process.stdout.write(JSON.stringify({suites:[{specs:[{title:'collect only'}]}],errors:[]}));
""")
            target.chmod(0o755)
            (self.modules / '.bin').mkdir(exist_ok=True)
            (self.modules / '.bin' / 'playwright').symlink_to('../@playwright/test/cli.js')
        (directory / 'package.json').write_text(json.dumps(data))
        self.lock['packages']['node_modules/' + name] = {'version': '1.0.0'}

    def write_manifests(self):
        (self.project / 'package.json').write_text(json.dumps(self.manifest))
        (self.project / 'package-lock.json').write_text(json.dumps(self.lock))

    def dependencies(self):
        return preflight.dependency_check(self.project, self.root, self.node)

    def test_node_itself_resolves_external_package_but_preflight_refuses(self):
        external = self.base / 'external'
        shutil.move(self.modules / 'react', external)
        (self.modules / 'react').symlink_to(external, target_is_directory=True)
        result = preflight.command([self.node, '-e', preflight.RESOLVE_SCRIPT,
            str(self.project / 'package.json'), '["react"]'], self.project)
        loaded = Path(json.loads(result['stdout'])[0]['path'])
        self.assertFalse(preflight.within(loaded, self.root))
        self.assertEqual(preflight.dependency_links(self.project, self.root)['reason'],
                         'dependency_link_outside_checkout')
        self.assertEqual(self.dependencies()['reason'], 'package_manifest_outside_checkout')

    def test_absolute_nested_node_modules_link_to_itself_is_refused(self):
        (self.modules / 'node_modules').symlink_to(self.modules, target_is_directory=True)
        self.assertEqual(preflight.dependency_links(self.project, self.root)['reason'],
                         'dependency_link_to_ancestor')

    def test_relative_ancestor_link_and_broken_link_are_refused(self):
        link = self.modules / 'node_modules'
        link.symlink_to('.')
        self.assertEqual(preflight.dependency_links(self.project, self.root)['reason'],
                         'dependency_link_to_ancestor')
        link.unlink()
        link.symlink_to('missing')
        self.assertEqual(preflight.dependency_links(self.project, self.root)['reason'],
                         'dependency_link_unresolved')

    def test_symlinked_dependency_root_is_refused(self):
        replacement = self.root / 'other-modules'
        self.modules.rename(replacement)
        self.modules.symlink_to(replacement)
        self.assertEqual(preflight.dependency_links(self.project, self.root)['reason'],
                         'dependency_root_symlink')

    def test_internal_cli_link_and_core_resolution_pass(self):
        self.assertEqual(preflight.dependency_links(self.project, self.root)['status'], 'passed')
        result = self.dependencies()
        self.assertEqual(result['status'], 'passed', result)
        self.assertEqual(result['resolved_modules']['react'], 'frontend/node_modules/react/index.js')
        self.assertEqual(result['playwright_cli'], 'frontend/node_modules/@playwright/test/cli.js')
        self.assertEqual(result['core_load_modes']['react'], 'dynamic_import_of_verified_entry')
        self.assertEqual(result['core_entry_sha256']['react'],
                         preflight.digest(self.modules / 'react/index.js'))

    def test_existing_broken_react_entry_cannot_pass_resolution_alone(self):
        (self.modules / 'react/index.js').write_text('const private_failure = ; // never-report-this')
        result = self.dependencies()
        self.assertEqual(result['reason'], 'core_module_load_failed')
        self.assertEqual(result['package'], 'react')
        self.assertNotIn('never-report-this', json.dumps(result))

    def test_vitest_uses_local_cli_without_importing_worker_only_api(self):
        self.package('vitest')
        self.manifest['devDependencies']['vitest'] = '1.0.0'
        self.write_manifests()
        directory = self.modules / 'vitest'
        (directory / 'index.js').write_text('throw new Error("worker API cannot load here");')
        manifest = json.loads((directory / 'package.json').read_text())
        manifest['bin'] = {'vitest': 'cli.js'}
        (directory / 'package.json').write_text(json.dumps(manifest))
        executable = directory / 'cli.js'
        executable.write_text('if(process.argv.slice(2).join(" ")!=="--version")process.exit(1); process.stdout.write("vitest/1.0.0");')
        executable.chmod(0o755)
        (self.modules / '.bin/vitest').symlink_to('../vitest/cli.js')
        result = self.dependencies()
        self.assertEqual(result['status'], 'passed', result)
        self.assertEqual(result['core_load_modes']['vitest'],
                         'local_cli_version_only_worker_api_not_imported')
        executable.write_text('process.stderr.write("private-error");process.exit(8);')
        result = self.dependencies()
        self.assertEqual(result['reason'], 'core_cli_probe_failed')
        self.assertNotIn('private-error', json.dumps(result))

    def test_missing_declared_dependency_blocks_without_install(self):
        shutil.rmtree(self.modules / 'react')
        with patch.object(preflight, 'command', wraps=preflight.command) as run:
            result = self.dependencies()
        self.assertEqual(result['reason'], 'declared_dependencies_missing_or_invalid')
        self.assertEqual(result['packages'], ['react'])
        run.assert_not_called()

    def test_package_version_and_bin_must_match_local_lock_and_target(self):
        self.lock['packages']['node_modules/react']['version'] = '2.0.0'
        self.write_manifests()
        self.assertEqual(self.dependencies()['reason'], 'installed_version_mismatch')
        self.lock['packages']['node_modules/react']['version'] = '1.0.0'
        self.write_manifests()
        (self.modules / '.bin' / 'playwright').unlink()
        self.assertEqual(self.dependencies()['reason'], 'package_bin_missing_or_wrong')

    def test_core_entry_cannot_escape_even_with_local_manifest(self):
        entry = self.modules / 'react' / 'index.js'
        entry.unlink()
        external = self.base / 'entry.js'
        external.write_text('module.exports = {};')
        entry.symlink_to(external)
        self.assertEqual(self.dependencies()['reason'], 'core_module_outside_checkout')

    def test_clean_exact_standalone_source_is_required(self):
        self.assertEqual(preflight.source_check(self.root, self.sha)['status'], 'passed')
        self.assertEqual(preflight.source_check(self.root, 'abc')['reason'], 'full_source_sha_required')
        self.assertEqual(preflight.source_check(self.root, '0' * 40)['reason'], 'source_sha_mismatch')
        (self.root / 'untracked.txt').write_text('dirty')
        self.assertEqual(preflight.source_check(self.root, self.sha)['reason'], 'checkout_not_clean')

    def test_actual_linked_worktree_is_rejected_before_go_build(self):
        linked = self.base / 'linked'
        self.git('worktree', 'add', '--detach', str(linked), self.sha)
        self.assertEqual(preflight.source_check(linked, self.sha)['reason'],
                         'standalone_git_directory_required')

    def test_local_cli_collects_only_and_drops_inherited_node_injection(self):
        with patch.dict(os.environ, {'NODE_OPTIONS': '--no-warnings', 'NODE_PATH': '/untrusted',
                                     'PREFLIGHT_TEST_SECRET': 'never-report-this'}):
            result = preflight.e2e_collection(self.project, self.root, self.node, self.dependencies())
        self.assertEqual(result['status'], 'passed', result)
        self.assertEqual(result['collected_specs'], 1)
        self.assertNotIn('never-report-this', json.dumps(result))

    def test_empty_or_broken_collection_cannot_pass(self):
        cli = self.modules / '@playwright/test/cli.js'
        for output, reason in [('{"suites":[]}', 'no_e2e_tests_collected'),
                               ('not-json', 'e2e_collection_failed'),
                               ('{"errors":[{}]}', 'e2e_collection_failed')]:
            cli.write_text('process.stdout.write(' + json.dumps(output) + ');')
            result = preflight.e2e_collection(self.project, self.root, self.node, self.dependencies())
            self.assertEqual(result['reason'], reason)

    def test_collection_failure_preserves_only_hashes(self):
        cli = self.modules / '@playwright/test/cli.js'
        cli.write_text('process.stderr.write("never-report-this"); process.exit(7);')
        result = preflight.e2e_collection(self.project, self.root, self.node, self.dependencies())
        self.assertEqual(result['status'], 'blocked')
        self.assertEqual(result['evidence']['returncode'], 7)
        self.assertNotIn('never-report-this', json.dumps(result))

    def test_lockfile_fingerprint_changes_and_cli_is_read_only(self):
        before = preflight.input_fingerprints(self.root)
        self.lock['changed'] = True
        self.write_manifests()
        after = preflight.input_fingerprints(self.root)
        self.assertNotEqual(before['frontend/package-lock.json']['sha256'],
                            after['frontend/package-lock.json']['sha256'])
        self.commit()
        tracked_before = self.git('status', '--porcelain')
        index_before = preflight.digest(self.root / '.git/index')
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = preflight.main(['--checkout', str(self.root), '--source-sha', self.sha, '--check-only'])
        result = json.loads(output.getvalue())
        self.assertEqual(code, 0, result)
        self.assertEqual(result['status'], 'passed')
        self.assertEqual(self.git('status', '--porcelain'), tracked_before)
        self.assertEqual(preflight.digest(self.root / '.git/index'), index_before)

    def test_unclean_source_never_loads_e2e_config(self):
        (self.root / 'untracked.txt').write_text('dirty')
        with patch.object(preflight, 'e2e_collection') as collect:
            result = preflight.collect_preflight(self.root, self.sha)
        collect.assert_not_called()
        self.assertEqual(result['status'], 'blocked')

    def test_timeout_is_bounded_and_command_output_stays_private(self):
        result = preflight.command([self.node, '-e', 'setInterval(()=>{},1000)'], self.root, timeout=.05)
        self.assertEqual(result['status'], 'timeout')
        self.assertLess(result['elapsed_seconds'], 2)
        self.assertNotIn('stdout', preflight.command_evidence(result))

    def test_offline_environment_disables_go_toolchain_download_and_injection(self):
        with patch.dict(os.environ, {'GOTOOLCHAIN': 'auto', 'GOPROXY': 'https://example.invalid',
                                     'NODE_OPTIONS': '--require /untrusted', 'NPM_TOKEN': 'secret'}):
            env = preflight.offline_environment()
        self.assertEqual(env['GOTOOLCHAIN'], 'local')
        self.assertEqual(env['GOPROXY'], 'off')
        self.assertEqual(env['npm_config_offline'], 'true')
        self.assertNotIn('NODE_OPTIONS', env)
        self.assertNotIn('NPM_TOKEN', env)

    def test_toolchain_never_starts_npm_or_npx(self):
        with patch.object(preflight, 'command', wraps=preflight.command) as run:
            result = preflight.toolchain_fingerprints(self.root)
        self.assertTrue(result['node']['available'])
        self.assertTrue(result['npm']['available'])
        self.assertEqual(result['npm']['probe'], 'installed_npm_descriptor')
        for call in run.call_args_list:
            self.assertNotIn(Path(call.args[0][0]).name, ('npm', 'npm-cli.js', 'npx', 'npx-cli.js'))

    def test_binding_ignores_timing_collection_and_diagnostic_changes(self):
        initial = preflight.collect_preflight(self.root, self.sha)
        final = preflight.collect_preflight(self.root, self.sha, collect_e2e=False)
        self.assertEqual(initial['binding_sha256'], final['binding_sha256'])
        final['elapsed_seconds'] = 9999
        for row in final['toolchain'].values():
            row['elapsed_seconds'] = 111
            row['stdout_sha256'] = 'variable diagnostic'
        final['checks'].append({'name': 'e2e_collection', 'status': 'passed',
                                'evidence': {'elapsed_seconds': 42}})
        self.assertEqual(initial['binding_sha256'], preflight.stable_fingerprint(final))
        self.assertNotIn('elapsed_seconds', json.dumps(initial['binding']))

    def test_binding_changes_when_toolchain_lock_or_dependency_changes(self):
        initial = preflight.collect_preflight(self.root, self.sha, collect_e2e=False)
        for target, key in [(initial['toolchain']['node'], 'binary_sha256'),
                            (initial['inputs']['frontend/package-lock.json'], 'sha256'),
                            (initial['binding']['dependencies']['frontend']['packages']['react'],
                             'manifest_sha256')]:
            before = preflight.stable_fingerprint(initial)
            old = target[key]
            target[key] = 'changed'
            self.assertNotEqual(before, preflight.stable_fingerprint(initial))
            target[key] = old

    def test_binding_changes_for_core_or_bin_bytes_with_same_manifest(self):
        initial = preflight.collect_preflight(self.root, self.sha, collect_e2e=False)
        react_manifest = preflight.digest(self.modules / 'react/package.json')
        (self.modules / 'react/index.js').write_text('module.exports = {changed: true};')
        changed = preflight.collect_preflight(self.root, self.sha, collect_e2e=False)
        self.assertEqual(react_manifest, preflight.digest(self.modules / 'react/package.json'))
        self.assertNotEqual(initial['binding_sha256'], changed['binding_sha256'])
        executable = self.modules / '@playwright/test/cli.js'
        executable.write_text(executable.read_text() + '\n// changed CLI bytes\n')
        final = preflight.collect_preflight(self.root, self.sha, collect_e2e=False)
        self.assertNotEqual(changed['binding_sha256'], final['binding_sha256'])

    def macos_fixture(self, selected):
        developer = self.base / ('Developer-' + selected)
        sdk = developer / 'SDKs/MacOSX.sdk'
        sdk.mkdir(parents=True)
        (sdk / 'SDKSettings.json').write_text('{"Version":"27.0"}')
        clang = developer / 'Toolchains/usr/bin/clang'
        clang.parent.mkdir(parents=True)
        clang.write_bytes(('private clang ' + selected).encode())
        return developer, sdk, clang

    def selected_macos(self, developer, sdk, clang, sdk_version='27.0'):
        def probe(args, root, timeout=30, *, env=None):
            self.assertEqual(env.get('DEVELOPER_DIR'), str(developer))
            self.assertEqual(env.get('SDKROOT'), str(sdk))
            self.assertEqual(env['GOTOOLCHAIN'], 'local')
            if '--find' in args:
                output = str(clang)
            elif '--show-sdk-path' in args:
                output = str(sdk)
            elif '--show-sdk-version' in args:
                output = sdk_version
            elif '-p' in args:
                output = str(developer)
            elif args == [str(clang), '--version']:
                output = 'Apple clang version 18.0.0 (private fixture)'
            else:
                self.fail('Unexpected macOS probe command')
            return {'status': 'passed', 'returncode': 0, 'stdout': output + '\n',
                    'stderr': '', 'elapsed_seconds': .001}
        with patch.dict(os.environ, {'DEVELOPER_DIR': str(developer), 'SDKROOT': str(sdk)}), \
                patch.object(preflight, 'command', side_effect=probe):
            return self.real_macos_probe(self.root, '/private/mock/xcrun', '/private/mock/xcode-select')

    def macos_binding(self, selected):
        return preflight.stable_fingerprint({'checkout': str(self.root),
            'source': {'status': 'passed', 'reason': 'fixture', 'source_sha': self.sha},
            'expected_source_sha': self.sha, 'inputs': {}, 'checks': [],
            'toolchain': {'macos': selected}})

    def test_selected_compiler_or_sdk_path_change_invalidates_binding(self):
        first = self.selected_macos(*self.macos_fixture('first'))
        second = self.selected_macos(*self.macos_fixture('second'))
        self.assertTrue(first['available'])
        self.assertNotEqual(first['clang_path'], second['clang_path'])
        self.assertNotEqual(first['sdk_path'], second['sdk_path'])
        self.assertNotEqual(self.macos_binding(first), self.macos_binding(second))

    def test_compiler_bytes_sdk_version_and_sdk_settings_invalidate_binding(self):
        developer, sdk, clang = self.macos_fixture('fixed-path')
        initial = self.selected_macos(developer, sdk, clang)
        clang.write_bytes(b'new compiler bytes at same path')
        compiler_changed = self.selected_macos(developer, sdk, clang)
        self.assertNotEqual(self.macos_binding(initial), self.macos_binding(compiler_changed))
        version_changed = self.selected_macos(developer, sdk, clang, sdk_version='27.1')
        self.assertNotEqual(self.macos_binding(compiler_changed), self.macos_binding(version_changed))
        (sdk / 'SDKSettings.json').write_text('{"Version":"27.1"}')
        settings_changed = self.selected_macos(developer, sdk, clang, sdk_version='27.1')
        self.assertNotEqual(self.macos_binding(version_changed), self.macos_binding(settings_changed))

    def test_missing_or_timed_out_macos_tools_never_pass(self):
        with patch.object(preflight.shutil, 'which', return_value=None):
            self.assertFalse(self.real_macos_probe(self.root)['available'])
        timeout = {'status': 'timeout', 'returncode': -9, 'stdout': '',
                   'stderr': 'private-tool-error', 'elapsed_seconds': 15}
        with patch.object(preflight, 'command', return_value=timeout):
            result = self.real_macos_probe(self.root, '/private/mock/xcrun', '/private/mock/xcode-select')
        self.assertFalse(result['available'])
        self.assertEqual(result['reason'], 'macos_selection_probe_failed')
        self.assertNotIn('private-tool-error', json.dumps(result))

    def test_missing_dependencies_or_dirty_source_never_probe_macos(self):
        shutil.rmtree(self.modules)
        with patch.object(preflight, 'macos_toolchain_fingerprint') as macos:
            result = preflight.collect_preflight(self.root, self.sha)
        macos.assert_not_called()
        self.assertEqual(result['status'], 'blocked')
        self.assertEqual(result['toolchain']['macos']['reason'],
                         'selection_probe_deferred' if preflight.sys.platform == 'darwin' else 'not_macos')
        (self.root / 'dirty.txt').write_text('untracked source')
        with patch.object(preflight, 'macos_toolchain_fingerprint') as macos:
            result = preflight.collect_preflight(self.root, self.sha)
        macos.assert_not_called()
        self.assertEqual(result['source']['reason'], 'checkout_not_clean')

    def test_custom_toolchain_overrides_block_without_echoing_values(self):
        for name in ('CC', 'CXX', 'GOROOT', 'GOTOOLCHAIN'):
            with self.subTest(name=name), patch.dict(os.environ, {name: 'private-custom-value'}):
                result = preflight.toolchain_override_check()
                self.assertEqual(result['status'], 'blocked')
                self.assertIn(name, result['variables'])
                self.assertNotIn('private-custom-value', json.dumps(result))
        with patch.dict(os.environ, {name: '' for name in ('CC', 'CXX', 'GOROOT')} | {'GOTOOLCHAIN': 'local'}):
            self.assertEqual(preflight.toolchain_override_check()['status'], 'passed')
        with patch.dict(os.environ, {'CC': 'private-custom-value'}), \
                patch.object(preflight, 'dependency_check') as dependencies, \
                patch.object(preflight, 'macos_toolchain_fingerprint') as macos:
            result = preflight.collect_preflight(self.root, self.sha)
        self.assertEqual(result['status'], 'blocked')
        dependencies.assert_not_called()
        macos.assert_not_called()

    def test_default_compiler_path_and_bytes_belong_to_binding(self):
        directory = self.base / 'compiler-bin'
        directory.mkdir()
        for name in ('fixture-cc', 'fixture-cxx'):
            target = directory / name
            target.write_text('private compiler bytes')
            target.chmod(0o755)
        output = {'status': 'passed', 'returncode': 0, 'stdout': 'fixture-cc\nfixture-cxx\n',
                  'stderr': '', 'elapsed_seconds': .001}
        with patch.dict(os.environ, {'PATH': str(directory)}), patch.object(preflight, 'command', return_value=output):
            first = preflight.default_compiler_fingerprints(self.root, '/private/mock/go')
            (directory / 'fixture-cc').write_text('changed compiler bytes')
            second = preflight.default_compiler_fingerprints(self.root, '/private/mock/go')
        report = {'checkout': str(self.root), 'source': {}, 'expected_source_sha': self.sha,
                  'inputs': {}, 'checks': [], 'toolchain': first}
        before = preflight.stable_fingerprint(report)
        report['toolchain'] = second
        self.assertNotEqual(before, preflight.stable_fingerprint(report))
        second['cc']['path'] += '-new-selection'
        self.assertNotEqual(preflight.stable_fingerprint({**report, 'toolchain': first}),
                            preflight.stable_fingerprint(report))

    @unittest.skipUnless(os.name == 'posix', 'owned process-group cancellation is POSIX')
    def test_keyboard_interrupt_and_system_exit_kill_and_reap_owned_node(self):
        real_popen = subprocess.Popen
        for original in (KeyboardInterrupt('private cancellation'), SystemExit(17)):
            ready = self.base / ('ready-' + type(original).__name__)
            owned = []
            def start(*args, **kwargs):
                process = real_popen(*args, **kwargs)
                owned.append(process)
                communicate = process.communicate
                interrupted = False
                def interrupt(*values, **options):
                    nonlocal interrupted
                    if not interrupted:
                        interrupted = True
                        deadline = time.monotonic() + 3
                        while not ready.exists() and time.monotonic() < deadline:
                            time.sleep(.01)
                        self.assertTrue(ready.exists())
                        raise original
                    return communicate(*values, **options)
                process.communicate = interrupt
                return process
            script = 'require("node:fs").writeFileSync(process.argv[1],"ready");setInterval(()=>{},1000);'
            with patch.object(preflight.subprocess, 'Popen', side_effect=start):
                with self.assertRaises(type(original)) as caught:
                    preflight.command([self.node, '-e', script, str(ready)], self.root)
            self.assertIs(caught.exception, original)
            self.assertIsNotNone(owned[0].returncode)
            self.assertEqual(owned[0].wait(), -9)

    def test_cleanup_failure_preserves_original_cancellation(self):
        original = KeyboardInterrupt('private original')
        process = Mock()
        process.communicate.side_effect = original
        with patch.object(preflight.subprocess, 'Popen', return_value=process), \
                patch.object(preflight, 'stop_owned_command', side_effect=OSError('cleanup failed')):
            with self.assertRaises(KeyboardInterrupt) as caught:
                preflight.command([self.node, '--version'], self.root)
        self.assertIs(caught.exception, original)


if __name__ == '__main__':
    unittest.main()
