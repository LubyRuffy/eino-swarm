"""Execute the actual fixed-slot decoder, scanner and final choose caller."""
from pathlib import Path
import json
import os
import platform
import subprocess
import tempfile
import unittest

from test_native_macos_attachment_scan import PREFIX, function

ROOT=Path(__file__).resolve().parent
SIGNATURES=['static AXError filePanelAttribute(', 'static NSDictionary *filePanelSlot(',
            'static BOOL filePanelBatch(', 'static BOOL filePanelText(',
            'static BOOL filePanelVisit(', 'static BOOL scanFilePanel(', 'static BOOL filePanelRoots(']
CONFIG=r'''
static void batchConfigure(NSString *mode){
 configure(mode);
 if([mode isEqual:@"batch-role-null"])nodes[@"file"][@"AXRole"]=NSNull.null;
 if([mode isEqual:@"batch-role-negative"])nodes[@"file"][@"AXRole"]=@(-25212);
 if([mode isEqual:@"batch-children-null"])nodes[@"file"][@"AXChildren"]=NSNull.null;
 if([mode isEqual:@"batch-title-null"]||[mode isEqual:@"batch-title-unsupported"]||[mode isEqual:@"batch-title-novalue"]){
  nodes[@"untitled"]=node(@"AXButton",@[]);nodes[@"root"][@"AXChildren"]=@[@"untitled",@"file",@"open"];
  if([mode isEqual:@"batch-title-null"])nodes[@"untitled"][@"AXTitle"]=NSNull.null;
  else nodes[@"untitled"][@"errors"]=@{@"AXTitle":@([mode hasSuffix:@"novalue"]?kAXErrorNoValue:kAXErrorAttributeUnsupported)};
 }
 if([mode isEqual:@"batch-aux-null"]){nodes[@"file"][@"AXTitle"]=NSNull.null;nodes[@"file"][@"AXDescription"]=@"input.bin";}
 if([mode isEqual:@"batch-aux-only-null"]){nodes[@"file"][@"AXTitle"]=NSNull.null;nodes[@"file"][@"AXDescription"]=NSNull.null;nodes[@"file"][@"AXValue"]=NSNull.null;}
 if([mode isEqual:@"batch-negative-aux"]){nodes[@"file"][@"AXTitle"]=@(-25212);nodes[@"file"][@"AXDescription"]=@"input.bin";}
 if([mode isEqual:@"batch-nonerror-axvalue"]){CGPoint point=CGPointMake(1,2);nodes[@"file"][@"AXTitle"]=CFBridgingRelease(AXValueCreate(kAXValueCGPointType,&point));nodes[@"file"][@"AXDescription"]=@"input.bin";}
 if([mode isEqual:@"batch-error-zero"]){AXError status=kAXErrorSuccess;nodes[@"file"][@"AXTitle"]=CFBridgingRelease(AXValueCreate(kAXValueAXErrorType,&status));}
 if([mode isEqual:@"batch-malformed-unused"]){AXError status=kAXErrorSuccess;nodes[@"open"][@"AXValue"]=CFBridgingRelease(AXValueCreate(kAXValueAXErrorType,&status));}
}
'''


def program(source):
    helpers=''.join(function(source,name) for name in SIGNATURES)
    start=source.index('        chooseTrace(@"panel.collect.before", nil);')
    end=source.index('        chooseTrace(@"panel.open.after", nil);',start)+len('        chooseTrace(@"panel.open.after", nil);')
    text=PREFIX+CONFIG+helpers+'\nstatic int run(void){AXUIElementRef app=(__bridge AXUIElementRef)@"app";NSString *value=@"/private/test/input.bin";\n'+source[start:end]+'\nreturn 0;}\n'
    # The missing decoder branch is only the pre-fix RED harness. Caller
    # negatives also execute the original scanner, which has no batch protocol.
    if function(source,'static NSDictionary *filePanelSlot('):
        text+=r'''
static NSDictionary *decode(NSString *mode){
 id raw=@"text";AXError status=kAXErrorCannotComplete;
 if([mode isEqual:@"decode-null"])raw=NSNull.null;
 if([mode isEqual:@"decode-negative"])raw=@(-25212);
 if([mode isEqual:@"decode-nil"])raw=nil;
 if([mode isEqual:@"decode-error"]||[mode isEqual:@"decode-zero"]){if([mode hasSuffix:@"zero"])status=0;raw=CFBridgingRelease(AXValueCreate(kAXValueAXErrorType,&status));}
 if([mode isEqual:@"decode-point"]){CGPoint p=CGPointZero;raw=CFBridgingRelease(AXValueCreate(kAXValueCGPointType,&p));}
 NSString *failure=nil;NSDictionary *slot=filePanelSlot(raw,&failure);
 return @{@"source":slot[@"source"]?:NSNull.null,@"status":slot[@"status"]?:NSNull.null,@"failure":failure?:@"",@"value_is_number":@([slot[@"value"] isKindOfClass:NSNumber.class])};
}
'''
    else:text+='static NSDictionary *decode(NSString *mode){return @{@"missing_decoder":@YES};}\n'
    text+=r'''
int main(int argc,const char **argv){@autoreleasepool{
 NSString *mode=@(argv[1]);batchConfigure(mode);double start=NSProcessInfo.processInfo.systemUptime;
 if([mode hasPrefix:@"decode-"]){puts([[[NSString alloc]initWithData:[NSJSONSerialization dataWithJSONObject:decode(mode) options:0 error:nil]encoding:NSUTF8StringEncoding]UTF8String]);return 0;}
 int code=run();double elapsed=NSProcessInfo.processInfo.systemUptime-start;
 NSDictionary *row=@{@"queries":@(queries),@"single_queries":@(singles),@"batch_queries":@(batches),@"batch_arguments":batchArguments,@"presses":@(presses),@"reads":reads,@"error":errorText?:@"",@"elapsed_seconds":@(elapsed)};
 puts([[[NSString alloc]initWithData:[NSJSONSerialization dataWithJSONObject:row options:0 error:nil]encoding:NSUTF8StringEncoding]UTF8String]);return code;
}}
'''
    return text


@unittest.skipUnless(platform.system()=='Darwin','Objective-C SDK is macOS-only')
class ActualPanelBatch(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory(prefix='issue94-batch-headless-');cls.addClassCleanup(cls.temp.cleanup)
        folder=Path(os.environ.get('PANEL_BATCH_COVERAGE_DIR',cls.temp.name));folder.mkdir(parents=True,exist_ok=True)
        path=folder/'actual.m';path.write_text(program((ROOT/'native_macos_resilience_ax.m').read_text()));cls.binary=path.with_suffix('')
        flags=['-fprofile-instr-generate','-fcoverage-mapping'] if os.environ.get('PANEL_BATCH_COVERAGE_DIR') else []
        built=subprocess.run(['/usr/bin/xcrun','clang','-fobjc-arc',*flags,'-framework','Foundation','-framework','ApplicationServices',str(path),'-o',str(cls.binary)],capture_output=True,text=True)
        if built.returncode:raise AssertionError(built.stderr)

    def sample(self,mode):
        result=subprocess.run([str(self.binary),mode],capture_output=True,text=True,timeout=5)
        return result.returncode,json.loads(result.stdout),result.stderr

    def test_actual_caller_uses_one_fixed_five_slot_batch_per_distinct_scan_node(self):
        code,row,_=self.sample('repeated');self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)
        self.assertEqual(row['batch_queries'],3);self.assertEqual(row['single_queries'],3)
        self.assertEqual([call['node'] for call in row['batch_arguments']],['root','file','open'])
        for call in row['batch_arguments']:
            self.assertEqual(call['attributes'],['AXRole','AXTitle','AXDescription','AXValue','AXChildren']);self.assertEqual(call['options'],0)

    def test_batch_top_error_and_malformed_shape_never_fall_back_or_press(self):
        for mode in ['batch-top-error','batch-top-unsupported','batch-top-partial','batch-nil','batch-array-type','batch-short','batch-long']:
            with self.subTest(mode=mode):
                code,row,_=self.sample(mode);self.assertEqual(code,2,row);self.assertEqual(row['presses'],0)
                self.assertEqual(row['batch_queries'],1);self.assertEqual(row['single_queries'],3);self.assertTrue(row['error'])
        self.assertIn('-25204',self.sample('batch-top-error')[1]['error'])

    def test_decoder_keeps_batch_missing_separate_from_real_error_and_negative_number(self):
        expected={'decode-null':('batch_missing',None),'decode-error':('attribute_error',-25204),'decode-negative':('value',0),'decode-point':('value',0)}
        for mode,(source,status) in expected.items():
            with self.subTest(mode=mode):
                _,row,_=self.sample(mode);self.assertEqual(row.get('source'),source);self.assertEqual(row.get('status'),status);self.assertEqual(row.get('failure'),'')
        self.assertTrue(self.sample('decode-negative')[1]['value_is_number'])
        for mode in ['decode-zero','decode-nil']:
            with self.subTest(mode=mode):self.assertTrue(self.sample(mode)[1].get('failure'))

    def test_missing_role_refuses_but_explicit_missing_title_and_children_keep_old_boundaries(self):
        for mode in ['batch-role-null','batch-role-negative']:
            with self.subTest(mode=mode):
                code,row,_=self.sample(mode);self.assertEqual(code,2,row);self.assertEqual(row['presses'],0)
        for mode in ['batch-children-null','batch-title-null','batch-title-unsupported','batch-title-novalue']:
            with self.subTest(mode=mode):
                code,row,_=self.sample(mode);self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)

    def test_auxiliary_null_number_and_nonerror_axvalue_never_become_errors_or_witnesses(self):
        for mode in ['batch-aux-null','batch-negative-aux','batch-nonerror-axvalue']:
            with self.subTest(mode=mode):
                code,row,_=self.sample(mode);self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)
        records=[json.loads(line.removeprefix('ZWAI_PANEL_NODE_FAILURE ')) for line in self.sample('batch-negative-aux')[2].splitlines() if line.startswith('ZWAI_PANEL_NODE_FAILURE ')]
        self.assertEqual(records[0]['status'],0);self.assertEqual(records[0]['source'],'value')
        code,row,_=self.sample('batch-aux-only-null');self.assertEqual(code,2,row);self.assertEqual(row['presses'],0)

    def test_malformed_error_wrapper_fails_even_in_unused_prefetched_auxiliary_slot(self):
        for mode in ['batch-error-zero','batch-decode-failure','batch-malformed-unused']:
            with self.subTest(mode=mode):
                code,row,_=self.sample(mode);self.assertEqual(code,2,row);self.assertEqual(row['presses'],0);self.assertIn('AXError',row['error'])

    def test_prefetched_auxiliary_errors_after_witness_are_not_consumed_as_required_failures(self):
        code,row,stderr=self.sample('aux-after-witness');self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)
        self.assertEqual(row['reads']['outline:AXValue'],1);self.assertNotIn('ZWAI_PANEL_NODE_FAILURE',stderr)

    def test_late_open_required_errors_and_outer_inventory_are_still_authoritative(self):
        for mode in ['late-duplicate','unknown-children-error','unknown-button-title-type','outer-children-error','outer-second-open','visit-5001','depth-46']:
            with self.subTest(mode=mode):
                code,row,_=self.sample(mode);self.assertEqual(code,2,row);self.assertEqual(row['presses'],0)

    def test_final_press_failure_stops_after_the_original_single_attempt(self):
        code,row,_=self.sample('press-error');self.assertEqual(code,2,row);self.assertEqual(row['presses'],1)
        self.assertEqual(row['batch_queries'],3);self.assertEqual(row['single_queries'],3)


if __name__=='__main__':unittest.main(verbosity=2)
