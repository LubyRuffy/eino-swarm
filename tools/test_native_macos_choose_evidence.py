"""Preserve the first native file-selection result through final cleanup."""
from pathlib import Path
import base64
import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parent


class NativeTimeoutReceipt(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location('choose_diagnostic', ROOT/'native_macos_resilience.py')
        cls.h = importlib.util.module_from_spec(spec);sys.modules[spec.name] = cls.h;spec.loader.exec_module(cls.h)

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory();self.addCleanup(self.temporary.cleanup)
        self.context = dict(app_pid=41,case_id='own-case',app=Path(self.temporary.name)/'zwai.app',helper='/private/own-helper',evidence=Path(self.temporary.name))
        self.owner = patch.object(self.h.runner,'process_identity',return_value=([str(self.context['app']/'Contents/MacOS/zwai')],'own-case'));self.owner.start();self.addCleanup(self.owner.stop)

    def test_actual_subprocess_timeout_retains_original_binary_streams_and_same_exception(self):
        real_run = subprocess.run;actual = {}
        def os_boundary(argv, **kwargs):
            self.assertEqual(kwargs, dict(capture_output=True,text=True,timeout=8))
            self.assertEqual(argv,['/private/own-helper','choose-file','41','/private/input.bin'])
            try:
                real_run([sys.executable,'-c','import os,time;os.write(1,b"\\x00\\xffout");os.write(2,b"err\\x00\\xfe");time.sleep(1)'],capture_output=True,text=True,timeout=.08)
            except subprocess.TimeoutExpired as error:
                actual['error'] = error
                raise
        with patch.object(self.h.subprocess,'run',side_effect=os_boundary) as run:
            with self.assertRaises(subprocess.TimeoutExpired) as caught:self.h.native(self.context,'choose-file','/private/input.bin')
        self.assertIs(caught.exception,actual['error']);self.assertEqual(run.call_count,1)
        row=json.loads((self.context['evidence']/'native-timeout.json').read_text())
        self.assertEqual(base64.b64decode(row['stdout_base64']),b'\x00\xffout')
        self.assertEqual(base64.b64decode(row['stderr_base64']),b'err\x00\xfe')
        self.assertEqual(row['type'],'TimeoutExpired');self.assertEqual(row['action'],'choose-file')
        self.assertEqual(row['requested_argv'],['/private/own-helper','choose-file','41','/private/input.bin'])

    def test_evidence_io_failure_never_masks_original_timeout_or_adds_retry(self):
        error=subprocess.TimeoutExpired(['/private/own-helper'],8,output=b'out',stderr=b'err');cause=RuntimeError('original-cause');error.__cause__=cause
        with patch.object(self.h.subprocess,'run',side_effect=error) as run,patch.object(self.h,'atomic_json',side_effect=OSError('disk unavailable')):
            with self.assertRaises(subprocess.TimeoutExpired) as caught:self.h.native(self.context,'choose-file','/private/input.bin')
        self.assertIs(caught.exception,error);self.assertIs(caught.exception.__cause__,cause);self.assertEqual(run.call_count,1)

    def test_timeout_distinguishes_absent_stream_from_present_empty_bytes(self):
        error=subprocess.TimeoutExpired(['/private/own-helper'],8,output=None,stderr=b'')
        with patch.object(self.h.subprocess,'run',side_effect=error):
            with self.assertRaises(subprocess.TimeoutExpired) as caught:self.h.native(self.context,'choose-file','/private/input.bin')
        self.assertIs(caught.exception,error)
        row=json.loads((self.context['evidence']/'native-timeout.json').read_text())
        self.assertTrue(row['stdout_is_none']);self.assertFalse(row['stderr_is_none'])
        self.assertEqual(row['stdout_base64'],'');self.assertEqual(row['stderr_base64'],'')

    def test_actual_final_retain_snapshot_timeout_does_not_overwrite_choose_failure(self):
        self.context.update(data=self.context['evidence']/'data',thread_id='thread',origin='http://127.0.0.1:1')
        def timeout(argv, **kwargs):
            raise subprocess.TimeoutExpired(argv,8,output=None,stderr=(argv[1]+' original receipt').encode())
        with patch.object(self.h.subprocess,'run',side_effect=timeout) as run,patch.object(self.h,'api',return_value={'draft':{'text':'draft','files':[],'state':'editing'}}):
            with self.assertRaises(subprocess.TimeoutExpired):self.h.native(self.context,'choose-file','/private/input.bin')
            first=(self.context['evidence']/'native-timeout.json').read_bytes()
            self.h.background.retain(self.context,self.h)
        self.assertEqual(run.call_count,2)
        self.assertEqual([call.args[0][1] for call in run.call_args_list],['choose-file','snapshot'])
        self.assertEqual((self.context['evidence']/'native-timeout.json').read_bytes(),first)
        self.assertEqual(json.loads(first)['action'],'choose-file')
        self.assertTrue((self.context['evidence']/'final-observation-error.json').exists())

    def test_success_remains_single_original_eight_second_call_without_timeout_receipt(self):
        with patch.object(self.h.subprocess,'run',return_value=subprocess.CompletedProcess([],0,'{"chosen":true}','')) as run:
            self.assertEqual(self.h.native(self.context,'choose-file','/private/input.bin'),{'chosen':True})
        self.assertEqual(run.call_count,1);self.assertEqual(run.call_args.kwargs['timeout'],8)
        self.assertFalse((self.context['evidence']/'native-timeout.json').exists())

    def test_success_and_nonzero_keep_completed_stdout_stderr_argv_and_returncode(self):
        argv=['/private/own-helper','choose-file','41','/private/input.bin']
        for code in [0,2]:
            with self.subTest(code=code),patch.object(self.h.subprocess,'run',return_value=subprocess.CompletedProcess(argv,code,'{"chosen":true}', 'ZWAI_CHOOSE real stage\n')) as run:
                if code:
                    with self.assertRaisesRegex(OSError,'native choose-file: ZWAI_CHOOSE real stage'):self.h.native(self.context,'choose-file','/private/input.bin')
                else:self.assertEqual(self.h.native(self.context,'choose-file','/private/input.bin'),{'chosen':True})
            row=json.loads((self.context['evidence']/'native-result.json').read_text())
            self.assertEqual(row['argv'],argv);self.assertEqual(row['returncode'],code)
            self.assertEqual(row['stdout'],'{"chosen":true}');self.assertEqual(row['stderr'],'ZWAI_CHOOSE real stage\n')
            self.assertEqual(run.call_count,1);self.assertEqual(run.call_args.kwargs['timeout'],8)

    def test_completed_receipt_io_failure_rejects_success_but_keeps_original_action_error(self):
        for code in [0,2]:
            with self.subTest(code=code),patch.object(self.h.subprocess,'run',return_value=subprocess.CompletedProcess([],code,'{"chosen":true}','original failure')),patch.object(self.h,'atomic_json',side_effect=OSError('evidence unavailable')):
                if code:
                    with self.assertRaisesRegex(OSError,'^native choose-file: original failure$'):self.h.native(self.context,'choose-file','/private/input.bin')
                else:
                    with self.assertRaisesRegex(OSError,'^evidence unavailable$'):self.h.native(self.context,'choose-file','/private/input.bin')


if __name__ == '__main__':unittest.main(verbosity=2)
