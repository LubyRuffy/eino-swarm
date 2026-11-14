"""The real smoke entry must configure its keyless case before app launch."""
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import sys
import tempfile
import unittest
from unittest import mock

SCRIPT = Path(__file__).with_name('native_macos_smoke.py')
spec = importlib.util.spec_from_file_location('smoke_local_fixture', SCRIPT)
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class LocalSmokeTests(unittest.TestCase):
    def test_fixture_does_not_replace_existing_case_configuration(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'config.yaml'
            path.write_text('preserve this configuration')
            with self.assertRaises(FileExistsError):
                smoke.prepare_local_fixture(None, {}, {'ZWAI_NATIVE_DATA': directory}, [], None, None)
            self.assertEqual(path.read_text(), 'preserve this configuration')

    def test_actual_entry_starts_keyless_app_in_local_mode_and_cleans_its_state(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app = root / 'fixture.app'
            exe = app / 'Contents/MacOS/fixture'
            exe.parent.mkdir(parents=True)
            (app / 'Contents/Info.plist').write_bytes(plistlib.dumps({'CFBundleExecutable': 'fixture'}))
            exe.write_text('#!' + sys.executable + '''
import json,os,pathlib,time
p=pathlib.Path(os.environ['ZWAI_NATIVE_DATA'])/'config.yaml'
row={'config':p.read_text() if p.exists() else '', 'mode':p.stat().st_mode & 0o777 if p.exists() else None}
with os.fdopen(os.open(pathlib.Path(os.environ['ZWAI_NATIVE_EVIDENCE'])/'launch.json',os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600),'w') as out:json.dump(row,out)
while True:time.sleep(.1)
''')
            exe.chmod(0o755)
            scenario = root / 'scenario.py'
            scenario.write_text('''import json,os,pathlib,time
p=pathlib.Path(os.environ['ZWAI_NATIVE_EVIDENCE'])/'launch.json'
end=time.monotonic()+3
while not p.exists() and time.monotonic()<end:time.sleep(.01)
r=json.loads(p.read_text())
assert r['config']=='product:\\n  mode: local\\n',r
assert r['mode']==0o600,r
''')
            evidence = root / 'evidence'
            with mock.patch.object(smoke.runner, 'require_guest'), \
                 mock.patch.object(smoke.runner, 'LOCK_PATH', root / 'case.lock'), \
                 mock.patch.object(smoke, 'case_preflight'), \
                 mock.patch.object(smoke, '__file__', str(scenario)):
                code = smoke.main(['--case', '--ax', str(root/'helper'), '--app', str(app),
                                   '--evidence', str(evidence), '--timeout', '6', '--budget', '12'])
            cleanup = json.loads((evidence/'cleanup.json').read_text())
            self.assertEqual(code, 0, cleanup)
            self.assertEqual(cleanup['fixture_product_mode'], 'local')
            self.assertTrue(cleanup['cleanup_complete'], cleanup)
            self.assertTrue(cleanup['data_removed'], cleanup)
            self.assertEqual(cleanup['remaining_owned_pids'], [])


if __name__ == '__main__':
    unittest.main()
