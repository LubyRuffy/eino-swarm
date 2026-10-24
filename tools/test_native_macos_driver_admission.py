"""Admission linearizes with real campaign ownership, before receipts can change."""
import argparse
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location('admission_control', Path(__file__).with_name('native_macos_resilience_control.py'))
control = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(control)


class AdmissionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='driver-admission-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.args = argparse.Namespace(evidence=self.root, launch_id='a' * 32)

    def assert_lock_free(self):
        with (self.root / 'driver.lock').open('a+') as descriptor:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)

    def test_admission_holds_actual_lock_and_refusal_closes_without_receipts(self):
        failure = RuntimeError('refused under original lock')
        calls = []
        def admit(args):
            self.assertIs(args, self.args)
            with (self.root / 'driver.lock').open('a+') as other:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(other, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.assertFalse((self.root / 'owner.json').exists())
            raise failure
        with self.assertRaises(RuntimeError) as caught:
            control.execute(self.args, lambda args: calls.append(args), admission=admit)
        self.assertIs(caught.exception, failure)
        self.assertEqual(calls, [])
        self.assertFalse((self.root / 'owner.json').exists())
        self.assertFalse((self.root / 'driver-exit.json').exists())
        self.assert_lock_free()

    def test_optional_admission_runs_once_before_owner_and_original_action(self):
        order = []
        def admit(args):
            order.append('admit')
            self.assertFalse((self.root / 'owner.json').exists())
        def action(args):
            order.append('action')
            self.assertEqual(json.loads((self.root / 'owner.json').read_text())['launch_id'], args.launch_id)
            return 7
        self.assertEqual(control.execute(self.args, action, admission=admit), 7)
        self.assertEqual(order, ['admit', 'action'])
        self.assertEqual(json.loads((self.root / 'driver-exit.json').read_text())['exit_code'], 7)
        self.assert_lock_free()

    def test_default_two_positional_callers_keep_return_and_exception_semantics(self):
        for value in (0, 7, KeyboardInterrupt(), ValueError('original action failure')):
            with self.subTest(value=type(value).__name__):
                calls = []
                def action(args):
                    calls.append(args)
                    if isinstance(value, BaseException):
                        raise value
                    return value
                if isinstance(value, BaseException):
                    with self.assertRaises(type(value)) as caught:
                        control.execute(self.args, action)
                    self.assertIs(caught.exception, value)
                else:
                    self.assertEqual(control.execute(self.args, action), value)
                row = json.loads((self.root / 'driver-exit.json').read_text())
                self.assertEqual(row['exit_code'], 130 if isinstance(value, KeyboardInterrupt) else (2 if isinstance(value, Exception) else value))
                self.assertTrue(row['cleanup_complete']) # Metadata-only fixture, never a native success.
                self.assertEqual(calls, [self.args])
                self.assert_lock_free()

    def test_busy_lock_never_calls_admission_or_action_and_preserves_old_owner(self):
        owner = self.root / 'owner.json'; owner.write_bytes(b'old owner')
        calls = []
        with (self.root / 'driver.lock').open('a+') as holder:
            fcntl.flock(holder, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(RuntimeError, 'already owns'):
                control.execute(self.args, lambda args: calls.append('action'), admission=lambda args: calls.append('admission'))
            self.assertEqual(owner.read_bytes(), b'old owner')
            self.assertEqual(calls, [])
        self.assert_lock_free()

    def test_owner_eisdir_closes_lock_without_action_or_terminal(self):
        (self.root / 'owner.json').mkdir(); calls = []
        failure = None
        try:
            control.execute(self.args, lambda args: calls.append(args))
        except IsADirectoryError as error:
            failure = error
        self.assertIsInstance(failure, IsADirectoryError)
        self.assertIsNotNone(failure.__traceback__) # Keep the frame alive; GC cannot hide an unclosed lock.
        self.assertEqual(calls, [])
        self.assertFalse((self.root / 'driver-exit.json').exists())
        self.assert_lock_free()

    def test_terminal_eisdir_preserves_io_failure_after_action_and_closes_lock(self):
        (self.root / 'driver-exit.json').mkdir(); calls = []
        def action(args):
            calls.append(args)
            return 0
        with self.assertRaises(IsADirectoryError):
            control.execute(self.args, action)
        self.assertEqual(calls, [self.args])
        self.assertTrue((self.root / 'owner.json').is_file())
        self.assert_lock_free()

    def test_admission_interrupt_or_io_failure_is_primary_and_does_not_create_owner(self):
        for failure in (KeyboardInterrupt(), OSError(28, 'controlled admission IO')):
            def admit(args):
                raise failure
            with self.subTest(kind=type(failure).__name__):
                with self.assertRaises(type(failure)) as caught:
                    control.execute(self.args, lambda args: self.fail('action before admission'), admission=admit)
                self.assertIs(caught.exception, failure)
                self.assertFalse((self.root / 'owner.json').exists())
                self.assertFalse((self.root / 'driver-exit.json').exists())
                self.assert_lock_free()

    def test_completed_original_driver_between_precheck_and_execute_keeps_exact_records(self):
        # A real previous execute completes at the victim's call-frame boundary.
        # This reproduces the lock-before-owner race without replacing flock/Path.
        for mode in ('complete', 'broken-owner', 'broken-terminal'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory(prefix='admission-race-') as temporary:
                root = Path(temporary)
                code = '''import argparse,inspect,json,os,sys,time
from pathlib import Path
sys.path.insert(0,sys.argv[2]);import native_macos_resilience_control as c
r=Path(sys.argv[1]);mode=sys.argv[3];role=sys.argv[4];args=argparse.Namespace(evidence=r,launch_id='e'*32)
def wait(name):
 end=time.monotonic()+5
 while not (r/name).exists():
  if time.monotonic()>=end:raise RuntimeError('owned synchronization expired')
  time.sleep(.005)
def snapshot():
 return {n:dict(link=os.readlink(r/n)) if (r/n).is_symlink() else dict(hex=(r/n).read_bytes().hex()) for n in ('owner.json','driver-exit.json')}
if role=='writer':
 wait('go');c.execute(args,lambda _:0)
 if mode!='complete':
  p=r/('owner.json' if mode=='broken-owner' else 'driver-exit.json');p.unlink();p.symlink_to(r/'absent')
 (r/'old.json').write_text(json.dumps(snapshot()));(r/'done').touch();raise SystemExit(0)
def admit(_):
 if any(os.path.lexists(r/n) for n in ('owner.json','driver-exit.json')):raise RuntimeError('existing owner or terminal')
admit(args)
def boundary(frame,event,arg):
 if event=='call' and frame.f_code is c.execute.__code__:
  sys.settrace(None);(r/'go').touch();wait('done')
 return boundary
sys.settrace(boundary);kwargs={'admission':admit} if 'admission' in inspect.signature(c.execute).parameters else {}
try:c.execute(args,lambda _:(r/'action-published').touch(),**kwargs)
except RuntimeError:pass
finally:sys.settrace(None)
print(json.dumps(dict(preserved=snapshot()==json.loads((r/'old.json').read_text()),action_published=(r/'action-published').exists())))
'''
                flags = ['-O'] if sys.flags.optimize else []
                argv = [sys.executable, *flags, '-c', code, str(root), str(Path(SPEC.origin).parent), mode]
                writer = subprocess.Popen([*argv, 'writer'], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                try:
                    victim = subprocess.run([*argv, 'victim'], capture_output=True, text=True, timeout=15)
                    writer_output, writer_error = writer.communicate(timeout=5)
                    self.assertEqual(writer.returncode, 0, writer_error)
                    self.assertEqual(victim.returncode, 0, victim.stderr)
                    row = json.loads(victim.stdout)
                    self.assertTrue(row['preserved'], victim.stdout)
                    self.assertFalse(row['action_published'], victim.stdout)
                finally:
                    if writer.poll() is None:
                        writer.terminate(); writer.wait(timeout=5)


if __name__ == '__main__':
    unittest.main(verbosity=2)
