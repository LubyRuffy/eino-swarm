from pathlib import Path
import json
import os
import platform
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parent


def function(source,signature):
    start=source.find(signature)
    if start<0:return ''
    opening=source.index('{',start);depth=1;end=opening+1
    while depth:
        depth+=(source[end]=='{')-(source[end]=='}');end+=1
    return source[start:end]+'\n'


# Broad traversal retained only as the original RPC-cost reference. Acceptance
# always executes the current scanner and its current caller below with OS seams.
LEGACY_COLLECT = r'''
static void collect(AXUIElementRef item, NSMutableArray *areas, NSMutableArray *buttons,
                    NSMutableString *text, NSUInteger *count, BOOL *web) {
    if (++*count > 20000 || text.length > 100000) return;
    NSString *role = string(attr(item, kAXRoleAttribute));
    if ([role isEqual:@"AXWebArea"]) *web = YES;
    if ([role isEqual:@"AXTextArea"]) [areas addObject:(__bridge id)item];
    if ([role isEqual:@"AXButton"]) [buttons addObject:(__bridge id)item];
    for (id key in @[(__bridge id)kAXTitleAttribute, (__bridge id)kAXDescriptionAttribute, (__bridge id)kAXValueAttribute]) {
        NSString *value = string(attr(item, (__bridge CFStringRef)key));
        if (value.length) [text appendFormat:@"%@\n", value];
    }
    id children = attr(item, kAXChildrenAttribute);
    if ([children isKindOfClass:NSArray.class]) for (id child in children)
        collect((__bridge AXUIElementRef)child, areas, buttons, text, count, web);
}
'''
LEGACY_CALLER = r'''
        chooseTrace(@"panel.collect.before", nil);
        NSMutableArray *areas = [NSMutableArray array], *buttons = [NSMutableArray array];
        NSMutableString *panelText = [NSMutableString string]; NSUInteger count = 0; BOOL web = NO;
        for (id sheet in sheets(attr(app, kAXWindowsAttribute))) collect((__bridge AXUIElementRef)sheet, areas, buttons, panelText, &count, &web);
        chooseTrace(@"panel.collect.after", nil);
        if (![panelText containsString:value.lastPathComponent]) { CFRelease(app); return fail(@"native panel did not reach the exact selected filename"); }
        NSMutableArray *opens = [NSMutableArray array];
        for (id button in buttons) if ([@[@"Open", @"打开"] containsObject:string(attr((__bridge AXUIElementRef)button, kAXTitleAttribute))]) [opens addObject:button];
        chooseTrace(@"panel.open.before", nil);
        if (opens.count != 1 || AXUIElementPerformAction((__bridge AXUIElementRef)opens[0], kAXPressAction))
            { CFRelease(app); return fail(@"native selected file has no unique Open action"); }
        chooseTrace(@"panel.open.after", nil);
'''

PREFIX=r'''#import <Foundation/Foundation.h>
#import <ApplicationServices/ApplicationServices.h>
static NSMutableDictionary *nodes,*reads;static NSUInteger queries=0,presses=0,batches=0,singles=0;
static NSMutableArray *batchArguments;static NSString *fixtureMode;
static NSString *errorText;
static NSString *string(id value){return [value isKindOfClass:NSString.class]?value:@"";}
static AXError fixtureValue(AXUIElementRef element,CFStringRef key,CFTypeRef *output){
    NSString *node=(__bridge NSString*)element,*name=(__bridge NSString*)key;
    NSString *read=[NSString stringWithFormat:@"%@:%@",node,name];reads[read]=@([reads[read] unsignedIntegerValue]+1);
    NSDictionary *row=nodes[node];NSNumber *failure=row[@"errors"][name];*output=NULL;
    if(failure)return failure.intValue;
    id value=row[name];if(!value)return kAXErrorAttributeUnsupported;
    *output=CFRetain((__bridge CFTypeRef)value);return kAXErrorSuccess;
}
static AXError fixtureCopy(AXUIElementRef element,CFStringRef key,CFTypeRef *output){
    queries++;singles++;return fixtureValue(element,key,output);
}
static AXError fixtureBatch(AXUIElementRef element,CFArrayRef attributes,AXCopyMultipleAttributeOptions options,CFArrayRef *output){
    queries++;batches++;NSString *name=(__bridge NSString*)element;
    [batchArguments addObject:@{@"node":name,@"attributes":(__bridge NSArray*)attributes,@"options":@(options)}];
    *output=NULL;
    if([fixtureMode isEqual:@"batch-top-error"])return kAXErrorCannotComplete;
    if([fixtureMode isEqual:@"batch-top-unsupported"])return kAXErrorAttributeUnsupported;
    if([fixtureMode isEqual:@"batch-nil"])return kAXErrorSuccess;
    if([fixtureMode isEqual:@"batch-array-type"]){*output=(CFArrayRef)CFRetain((__bridge CFTypeRef)@42);return kAXErrorSuccess;}
    NSMutableArray *slots=[NSMutableArray array];
    for(NSString *key in (__bridge NSArray*)attributes){
        CFTypeRef raw=NULL;AXError status=fixtureValue(element,(__bridge CFStringRef)key,&raw);
        if(status){AXValueRef error=AXValueCreate(kAXValueAXErrorType,&status);[slots addObject:CFBridgingRelease(error)];}
        else [slots addObject:CFBridgingRelease(raw)];
    }
    NSDictionary *overrides=nodes[name][@"batch_slots"];
    for(NSNumber *index in overrides)slots[index.unsignedIntegerValue]=overrides[index];
    if([fixtureMode isEqual:@"batch-short"])[slots removeLastObject];
    if([fixtureMode isEqual:@"batch-long"])[slots addObject:NSNull.null];
    *output=(CFArrayRef)CFRetain((__bridge CFTypeRef)slots);
    return [fixtureMode isEqual:@"batch-top-partial"] ? kAXErrorCannotComplete : kAXErrorSuccess;
}
static Boolean fixtureDecode(AXValueRef value,AXValueType type,void *output){
    return [fixtureMode isEqual:@"batch-decode-failure"] ? false : AXValueGetValue(value,type,output);
}
static id attr(AXUIElementRef element,CFStringRef key){
    CFTypeRef value=NULL;AXError status=fixtureCopy(element,key,&value);
    if(status!=kAXErrorSuccess)return nil;return value?CFBridgingRelease(value):nil;
}
static AXError press(AXUIElementRef element,CFStringRef action){presses++;return [fixtureMode isEqual:@"press-error"]?kAXErrorCannotComplete:0;}
static NSArray *sheets(id windows){
    NSMutableArray *result=[NSMutableArray array];if(![windows isKindOfClass:NSArray.class])return result;
    for(id window in windows){id children=attr((__bridge AXUIElementRef)window,kAXChildrenAttribute);
        if([children isKindOfClass:NSArray.class])for(id child in children)
            if([string(attr((__bridge AXUIElementRef)child,kAXRoleAttribute)) isEqual:@"AXSheet"])[result addObject:child];}
    return result;
}
static int fail(NSString *text){errorText=text;return 2;}
static void releaseFixture(CFTypeRef value){if(CFGetTypeID(value)!=CFStringGetTypeID())CFRelease(value);}
static void chooseTrace(NSString *stage,NSDictionary *identityRow){}
static NSMutableDictionary *node(NSString *role,NSArray *children){return [@{@"AXRole":role,@"AXChildren":children} mutableCopy];}
static void configure(NSString *mode){
    fixtureMode=mode;nodes=[NSMutableDictionary dictionary];reads=[NSMutableDictionary dictionary];batchArguments=[NSMutableArray array];
    nodes[@"app"]=[@{@"AXWindows":@[@"window"]} mutableCopy];nodes[@"window"]=node(@"AXWindow",@[@"root"]);nodes[@"root"]=node(@"AXSheet",@[@"file",@"open"]);
    nodes[@"file"]=node(@"AXStaticText",@[]);nodes[@"open"]=node(@"AXButton",@[]);nodes[@"open"][@"AXTitle"]=@"Open";
    NSString *field=[mode isEqual:@"description"]?@"AXDescription":[mode isEqual:@"value"]?@"AXValue":@"AXTitle";
    if(![mode isEqual:@"missing"])nodes[@"file"][field]=@"before input.bin after";
    if([mode isEqual:@"optional-nontext"]){nodes[@"file"][@"AXTitle"]=@42;nodes[@"file"][@"AXDescription"]=@"input.bin";}
    if([mode isEqual:@"repeated"]){nodes[@"root"][@"AXChildren"]=@[@"file",@"open",[NSString stringWithFormat:@"%@",@"open"]];}
    if([mode isEqual:@"cycle"]){nodes[@"open"][@"AXChildren"]=@[@"root"];}
    if([mode isEqual:@"large"]||[mode isEqual:@"late-duplicate"]){
        NSMutableArray *children=[NSMutableArray arrayWithArray:@[@"file",@"open"]];
        for(NSUInteger i=0;i<200;i++){NSString *name=[NSString stringWithFormat:@"filler-%lu",i];nodes[name]=node(@"AXGroup",@[]);nodes[name][@"AXTitle"]=@"unrelated";nodes[name][@"AXDescription"]=@"metadata";nodes[name][@"AXValue"]=@"content";[children addObject:name];}
        if([mode isEqual:@"late-duplicate"]){nodes[@"another-open"]=node(@"AXButton",@[]);nodes[@"another-open"][@"AXTitle"]=@"Open";[children addObject:@"another-open"];}
        nodes[@"root"][@"AXChildren"]=children;
    }
    if([mode isEqual:@"edge-budget"]){
        NSMutableArray *children=[NSMutableArray arrayWithArray:@[@"file",@"open"]];for(NSUInteger i=0;i<5000;i++)[children addObject:@"file"];nodes[@"root"][@"AXChildren"]=children;
    }
    if([mode isEqual:@"node-budget"]){
        NSMutableArray *children=[NSMutableArray arrayWithArray:@[@"file",@"open"]];for(NSUInteger i=0;i<5000;i++){NSString *name=[NSString stringWithFormat:@"node-%lu",i];nodes[name]=node(@"AXGroup",@[]);[children addObject:name];}nodes[@"root"][@"AXChildren"]=children;
    }
    if([mode isEqual:@"visit-5000"]||[mode isEqual:@"visit-5001"]){
        NSMutableArray *children=[NSMutableArray arrayWithArray:@[@"file",@"open"]];NSUInteger count=[mode isEqual:@"visit-5000"]?4994:4995;
        for(NSUInteger i=0;i<count;i++){NSString *name=[NSString stringWithFormat:@"node-%lu",i];nodes[name]=node(@"AXGroup",@[]);[children addObject:name];}nodes[@"root"][@"AXChildren"]=children;
    }
    if([mode hasPrefix:@"depth-"]){
        NSUInteger depth=[mode hasSuffix:@"45"]?45:46;NSString *current=@"root";
        for(NSUInteger i=1;i<=depth;i++){NSString *next=[NSString stringWithFormat:@"depth-%lu",i];nodes[current]=node(@"AXGroup",@[next]);nodes[next]=node(@"AXGroup",@[]);current=next;}
        nodes[current]=node(@"AXButton",@[]);nodes[current][@"AXTitle"]=@"Open";nodes[current][@"AXDescription"]=@"input.bin";
        nodes[@"root"][@"AXRole"]=@"AXSheet";
    }
    if([mode isEqual:@"deep-cycle"]){
        nodes[@"root"][@"AXTitle"]=@"input.bin";nodes[@"root"][@"AXChildren"]=@[@"open",@"chain-1"];
        for(NSUInteger i=1;i<=45;i++){NSString *name=[NSString stringWithFormat:@"chain-%lu",i],*next=i==45?@"root":[NSString stringWithFormat:@"chain-%lu",i+1];nodes[name]=node(@"AXGroup",@[next]);}
    }
    if([mode isEqual:@"bad-children"])nodes[@"root"][@"AXChildren"]=@42;
    if([mode isEqual:@"bad-child"])nodes[@"root"][@"AXChildren"]=@[@"file",@"open",@42];
    if([mode isEqual:@"bad-role"])nodes[@"file"][@"AXRole"]=@42;
    if([mode isEqual:@"children-error"])nodes[@"root"][@"errors"]=@{@"AXChildren":@(kAXErrorCannotComplete)};
    if([mode isEqual:@"invalid-element"])nodes[@"file"][@"errors"]=@{@"AXRole":@(kAXErrorInvalidUIElement)};
    if([mode isEqual:@"title-error"])nodes[@"file"][@"errors"]=@{@"AXTitle":@(kAXErrorCannotComplete)};
    if([mode isEqual:@"late-title-error"])nodes[@"open"][@"errors"]=@{@"AXTitle":@(kAXErrorCannotComplete)};
    if([mode isEqual:@"optional-absence"]){nodes[@"file"][@"errors"]=@{@"AXTitle":@(kAXErrorNoValue),@"AXDescription":@(kAXErrorAttributeUnsupported)};nodes[@"file"][@"AXValue"]=@"input.bin";}
    if([mode isEqual:@"children-unsupported"]||[mode isEqual:@"children-novalue"]||[mode isEqual:@"button-file"]){
        nodes[@"root"][@"AXChildren"]=@[@"open"];nodes[@"open"][@"AXDescription"]=@"input.bin";
        if(![mode isEqual:@"button-file"])nodes[@"open"][@"errors"]=@{@"AXChildren":@([mode isEqual:@"children-novalue"]?kAXErrorNoValue:kAXErrorAttributeUnsupported)};
    }
    if([mode isEqual:@"description-error"]){nodes[@"file"][@"AXTitle"]=@"unrelated";nodes[@"file"][@"errors"]=@{@"AXDescription":@(kAXErrorCannotComplete)};}
    if([mode isEqual:@"value-error"]){nodes[@"file"][@"AXTitle"]=@"unrelated";nodes[@"file"][@"AXDescription"]=@"unrelated";nodes[@"file"][@"errors"]=@{@"AXValue":@(kAXErrorCannotComplete)};}
    if([mode hasPrefix:@"aux-"]||[mode hasPrefix:@"unknown-"]){
        nodes[@"outline"]=node(@"AXOutline",@[]);nodes[@"outline"][@"errors"]=@{@"AXValue":@(kAXErrorFailure)};
        nodes[@"root"][@"AXChildren"]=@[@"outline",@"file",@"open"];
    }
    if([mode isEqual:@"aux-after-open"])nodes[@"root"][@"AXChildren"]=@[@"open",@"outline",@"file"];
    if([mode isEqual:@"aux-after-witness"])nodes[@"root"][@"AXChildren"]=@[@"file",@"outline",@"open"];
    if([mode isEqual:@"aux-description-witness"]){nodes[@"file"][@"errors"]=@{@"AXTitle":@(kAXErrorCannotComplete)};[nodes[@"file"] removeObjectForKey:@"AXTitle"];nodes[@"file"][@"AXDescription"]=@"input.bin";}
    if([mode isEqual:@"aux-value-witness"]){nodes[@"file"][@"errors"]=@{@"AXTitle":@(kAXErrorCannotComplete),@"AXDescription":@(kAXErrorFailure)};nodes[@"file"][@"AXValue"]=@"input.bin";}
    if([mode isEqual:@"aux-button-witness"]){nodes[@"root"][@"AXChildren"]=@[@"open"];nodes[@"open"][@"errors"]=@{@"AXDescription":@(kAXErrorFailure)};nodes[@"open"][@"AXValue"]=@"input.bin";}
    if([mode isEqual:@"unknown-without-witness"])nodes[@"file"][@"AXTitle"]=@"unrelated";
    if([mode isEqual:@"unknown-nonstring"]){nodes[@"file"][@"AXTitle"]=@42;nodes[@"file"][@"AXDescription"]=@{};nodes[@"file"][@"AXValue"]=@42;}
    if([mode isEqual:@"unknown-children-error"])nodes[@"outline"][@"errors"]=@{@"AXValue":@(kAXErrorFailure),@"AXChildren":@(kAXErrorCannotComplete)};
    if([mode isEqual:@"unknown-late-open"]){nodes[@"another-open"]=node(@"AXButton",@[]);nodes[@"another-open"][@"AXTitle"]=@"Open";nodes[@"root"][@"AXChildren"]=@[@"outline",@"file",@"open",@"another-open"];}
    if([mode isEqual:@"unknown-button-title"]||[mode isEqual:@"unknown-button-title-type"]){nodes[@"root"][@"AXChildren"]=@[@"file",@"open"];if([mode hasSuffix:@"type"])nodes[@"open"][@"AXTitle"]=@42;else nodes[@"open"][@"errors"]=@{@"AXTitle":@(kAXErrorFailure)};}
    if([mode isEqual:@"unknown-cap"]||[mode isEqual:@"unknown-cap-required"]){
        NSMutableArray *children=[NSMutableArray array];
        for(NSUInteger i=0;i<200;i++){NSString *name=[NSString stringWithFormat:@"unknown-%lu",i];nodes[name]=node(@"AXGroup",@[]);nodes[name][@"errors"]=@{@"AXTitle":@(kAXErrorFailure),@"AXDescription":@(kAXErrorFailure),@"AXValue":@(kAXErrorFailure)};[children addObject:name];}
        [children addObjectsFromArray:@[@"file",@"open"]];nodes[@"root"][@"AXChildren"]=children;
        if([mode hasSuffix:@"required"])nodes[@"open"][@"errors"]=@{@"AXTitle":@(kAXErrorFailure)};
    }
    if([mode isEqual:@"empty-role"])nodes[@"file"][@"AXRole"]=@"";
    if([mode isEqual:@"inner-role-novalue"])nodes[@"file"][@"errors"]=@{@"AXRole":@(kAXErrorNoValue)};
    if([mode hasPrefix:@"outer-"]){
        nodes[@"app"][@"AXWindows"]=@[@"window",@"window-2"];
        nodes[@"window-2"]=node(@"AXWindow",@[@"other-root"]);nodes[@"other-root"]=node(@"AXSheet",@[]);
        if([mode isEqual:@"outer-children-error"])nodes[@"window-2"][@"errors"]=@{@"AXChildren":@(kAXErrorCannotComplete)};
        if([mode isEqual:@"outer-children-type"])nodes[@"window-2"][@"AXChildren"]=@42;
        if([mode isEqual:@"outer-role-error"])nodes[@"other-root"][@"errors"]=@{@"AXRole":@(kAXErrorInvalidUIElement)};
        if([mode isEqual:@"outer-role-novalue"])nodes[@"other-root"][@"errors"]=@{@"AXRole":@(kAXErrorNoValue)};
        if([mode isEqual:@"outer-role-type"])nodes[@"other-root"][@"AXRole"]=@42;
        if([mode isEqual:@"outer-child-type"])nodes[@"window-2"][@"AXChildren"]=@[@42];
        if([mode isEqual:@"outer-window-type"])nodes[@"app"][@"AXWindows"]=@[@"window",@42];
        if([mode isEqual:@"outer-windows-type"])nodes[@"app"][@"AXWindows"]=@42;
        if([mode isEqual:@"outer-windows-error"])nodes[@"app"][@"errors"]=@{@"AXWindows":@(kAXErrorCannotComplete)};
        if([mode isEqual:@"outer-windows-absent"])nodes[@"app"][@"errors"]=@{@"AXWindows":@(kAXErrorNoValue)};
        if([mode isEqual:@"outer-children-absent"])nodes[@"window-2"][@"errors"]=@{@"AXChildren":@(kAXErrorNoValue)};
        if([mode isEqual:@"outer-second-open"]){nodes[@"other-open"]=node(@"AXButton",@[]);nodes[@"other-open"][@"AXTitle"]=@"Open";nodes[@"other-root"][@"AXChildren"]=@[@"other-open"];}
        if([mode isEqual:@"outer-budget"]){nodes[@"empty-window"]=node(@"AXWindow",@[]);NSMutableArray *windows=[NSMutableArray array];for(NSUInteger i=0;i<5000;i++)[windows addObject:@"empty-window"];nodes[@"app"][@"AXWindows"]=windows;}
        if([mode isEqual:@"outer-shared-budget"]){
            NSMutableArray *first=[NSMutableArray arrayWithArray:@[@"file",@"open"]],*second=[NSMutableArray array];
            for(NSUInteger i=0;i<3000;i++){NSString *a=[NSString stringWithFormat:@"first-%lu",i],*b=[NSString stringWithFormat:@"second-%lu",i];nodes[a]=node(@"AXGroup",@[]);nodes[b]=node(@"AXGroup",@[]);[first addObject:a];[second addObject:b];}
            nodes[@"root"][@"AXChildren"]=first;nodes[@"other-root"][@"AXChildren"]=second;
        }
    }
}
#define AXUIElementCopyAttributeValue fixtureCopy
#define AXUIElementCopyMultipleAttributeValues fixtureBatch
#define AXValueGetValue fixtureDecode
#define AXUIElementGetTypeID CFStringGetTypeID
#define AXUIElementPerformAction press
#define CFRelease releaseFixture
'''


@unittest.skipUnless(platform.system() == 'Darwin', 'Objective-C SDK is macOS-only')
class ActualFilePanelScanner(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory(prefix='issue94-panel-headless-');cls.addClassCleanup(cls.temp.cleanup)
        cls.binaries={};after=(ROOT/'native_macos_resilience_ax.m').read_text()
        if not function(after,'static BOOL scanFilePanel('):raise AssertionError('target behavior missing: complete unique file panel scanner')
        for label in ['before','after']:
            source=after;helpers=LEGACY_COLLECT if label=='before' else ''
            if label=='after':
                for signature in ['static AXError filePanelAttribute(','static NSDictionary *filePanelSlot(','static BOOL filePanelBatch(','static BOOL filePanelText(','static BOOL filePanelVisit(','static BOOL scanFilePanel(','static BOOL filePanelRoots(']:helpers+=function(source,signature)
            start=source.index('        chooseTrace(@"panel.collect.before", nil);');last=source.index('        chooseTrace(@"panel.open.after", nil);',start)+len('        chooseTrace(@"panel.open.after", nil);')
            actual=LEGACY_CALLER if label=='before' else source[start:last]
            program=PREFIX+helpers+'\nstatic int run(void){AXUIElementRef app=(__bridge AXUIElementRef)@"app";NSString *value=@"/private/test/input.bin";\n'+actual+'\nreturn 0;}\n'
            program+=r'''int main(int argc,const char **argv){@autoreleasepool{configure(@(argv[1]));int code=run();
                NSDictionary *row=@{@"queries":@(queries),@"batch_queries":@(batches),@"single_queries":@(singles),@"presses":@(presses),@"reads":reads,@"error":errorText?:@""};
                puts([[NSString alloc]initWithData:[NSJSONSerialization dataWithJSONObject:row options:0 error:nil]encoding:NSUTF8StringEncoding].UTF8String);return code;}}
'''
            folder=Path(os.environ.get('PANEL_SCAN_COVERAGE_DIR',cls.temp.name));folder.mkdir(parents=True,exist_ok=True)
            p=folder/(label+'.m');p.write_text(program);binary=p.with_suffix('')
            flags=['-fprofile-instr-generate','-fcoverage-mapping'] if os.environ.get('PANEL_SCAN_COVERAGE_DIR') and label=='after' else []
            built=subprocess.run(['/usr/bin/xcrun','clang','-fobjc-arc',*flags,'-framework','Foundation','-framework','ApplicationServices',str(p),'-o',str(binary)],capture_output=True,text=True)
            if built.returncode:raise AssertionError(built.stderr)
            cls.binaries[label]=binary

    def sample(self,mode,label='after'):
        result=subprocess.run([str(self.binaries[label]),mode],capture_output=True,text=True,timeout=5)
        return result.returncode,json.loads(result.stdout)

    def witnessed(self,mode):
        result=subprocess.run([str(self.binaries['after']),mode],capture_output=True,text=True,timeout=5)
        records=[json.loads(line.removeprefix('ZWAI_PANEL_NODE_FAILURE ')) for line in result.stderr.splitlines() if line.startswith('ZWAI_PANEL_NODE_FAILURE ')]
        summaries=[json.loads(line.removeprefix('ZWAI_PANEL_UNKNOWN_SUMMARY ')) for line in result.stderr.splitlines() if line.startswith('ZWAI_PANEL_UNKNOWN_SUMMARY ')]
        return result.returncode,json.loads(result.stdout),records,summaries

    def test_other_successful_filename_witness_survives_auxiliary_unknown_reads(self):
        for mode in ['aux-before-witness','aux-after-open','aux-description-witness','aux-value-witness','aux-button-witness']:
            with self.subTest(mode=mode):
                code,row,records,summaries=self.witnessed(mode)
                self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)
                self.assertTrue(records);self.assertTrue(all(item['disposition']=='unknown' for item in records))
                self.assertTrue(all(isinstance(item['status'],int) for item in records));self.assertGreater(summaries[0]['unknown_reads'],0)
        code,row,records,summaries=self.witnessed('aux-before-witness')
        error=records[0];self.assertEqual(error['role'],'AXOutline');self.assertEqual(error['attribute'],'AXValue')
        self.assertEqual(error['status'],-25200);self.assertEqual(error['node_path'],[0,0]);self.assertFalse(error['filename_found'])
        self.assertEqual(error['open_count'],0);self.assertEqual(error['depth'],1)

    def test_unknown_and_nonstring_values_cannot_invent_a_filename_witness(self):
        for mode in ['unknown-without-witness','unknown-nonstring']:
            with self.subTest(mode=mode):
                code,row,records,summaries=self.witnessed(mode)
                self.assertEqual(code,2,row);self.assertEqual(row['presses'],0);self.assertTrue(records)
                self.assertTrue(all(item['disposition']=='unknown' for item in records))
                self.assertIn('did not reach',row['error'])
        code,row,records,summaries=self.witnessed('unknown-nonstring')
        self.assertTrue(any(item['status']==0 and item['value_type']!='NSString' for item in records))

    def test_auxiliary_unknown_never_hides_a_second_open_or_required_read_failure(self):
        for mode in ['unknown-late-open','unknown-children-error','unknown-button-title','unknown-button-title-type']:
            with self.subTest(mode=mode):
                code,row,records,summaries=self.witnessed(mode)
                self.assertEqual(code,2,row);self.assertEqual(row['presses'],0)
        code,row,records,summaries=self.witnessed('unknown-button-title')
        self.assertEqual(row['error'],'native panel AXTitle read failed: -25200')
        self.assertEqual(records[-1]['disposition'],'required_failure');self.assertTrue(records[-1]['filename_found'])
        code,row,records,summaries=self.witnessed('aux-after-witness')
        self.assertEqual(code,0,row);self.assertEqual(row['presses'],1);self.assertEqual(records,[])
        self.assertEqual(row['reads']['outline:AXValue'],1)  # prefetched, not consumed

    def test_unknown_diagnostics_are_bounded_without_hiding_a_later_required_failure(self):
        code,row,records,summaries=self.witnessed('unknown-cap')
        self.assertEqual(code,0,row);self.assertEqual(row['presses'],1);self.assertEqual(len(records),64)
        self.assertEqual(summaries,[dict(unknown_reads=600,retained_unknown_reads=64)])
        code,row,records,summaries=self.witnessed('unknown-cap-required')
        self.assertEqual(code,2,row);self.assertEqual(row['presses'],0);self.assertEqual(len(records),65)
        self.assertEqual(records[-1]['disposition'],'required_failure');self.assertEqual(records[-1]['status'],-25200)
        self.assertEqual(summaries,[dict(unknown_reads=600,retained_unknown_reads=64)])

    def test_filename_contains_remains_supported_in_each_original_text_attribute(self):
        for mode in ['title','description','value','optional-nontext','optional-absence']:
            with self.subTest(mode=mode):
                code,row=self.sample(mode);self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)

    def test_distinct_late_open_is_rejected_after_filename_and_first_open_were_seen(self):
        code,row=self.sample('late-duplicate');self.assertEqual(code,2);self.assertEqual(row['presses'],0)
        self.assertEqual(row['reads']['another-open:AXTitle'],1)

    def test_repeated_references_and_cycles_use_cf_identity_without_duplicate_open(self):
        for mode in ['repeated','cycle']:
            with self.subTest(mode=mode):
                code,row=self.sample(mode);self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)
                self.assertEqual(row['reads']['open:AXTitle'],1)

    def test_missing_filename_never_presses_open(self):
        code,row=self.sample('missing');self.assertEqual(code,2);self.assertEqual(row['presses'],0)

    def test_visit_limit_counts_distinct_nodes_and_repeated_edges(self):
        for mode in ['node-budget','edge-budget']:
            with self.subTest(mode=mode):
                code,row=self.sample(mode);self.assertEqual(code,2);self.assertEqual(row['presses'],0)
                self.assertIn('5000',row['error'])

    def test_new_node_depth_forty_five_is_allowed_and_forty_six_is_rejected(self):
        code,row=self.sample('depth-45');self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)
        code,row=self.sample('depth-46');self.assertEqual(code,2);self.assertEqual(row['presses'],0);self.assertIn('45',row['error'])

    def test_revisited_node_at_deep_edge_is_not_a_new_depth_violation(self):
        code,row=self.sample('deep-cycle');self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)
        self.assertEqual(row['reads']['root:AXChildren'],1)

    def test_shared_visit_budget_accepts_five_thousand_and_refuses_the_next_visit(self):
        code,row=self.sample('visit-5000');self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)
        code,row=self.sample('visit-5001');self.assertEqual(code,2);self.assertEqual(row['presses'],0);self.assertIn('5000',row['error'])

    def test_malformed_or_unreadable_tree_never_claims_complete_selection(self):
        for mode in ['bad-children','bad-child','bad-role','empty-role','inner-role-novalue','children-error','invalid-element','title-error','description-error','value-error','late-title-error']:
            with self.subTest(mode=mode):
                code,row=self.sample(mode);self.assertEqual(code,2,row);self.assertEqual(row['presses'],0)
                self.assertTrue(row['error'])

    def test_absent_children_are_explicit_leaves_after_required_role(self):
        for mode in ['children-unsupported','children-novalue']:
            with self.subTest(mode=mode):
                code,row=self.sample(mode);self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)

    def test_actual_caller_refuses_incomplete_outer_roots_even_with_a_valid_first_panel(self):
        for mode in ['outer-children-error','outer-children-type','outer-role-error','outer-role-novalue','outer-role-type','outer-child-type','outer-window-type','outer-windows-type','outer-windows-error','outer-windows-absent','outer-second-open']:
            with self.subTest(mode=mode):
                code,row=self.sample(mode);self.assertEqual(code,2,row);self.assertEqual(row['presses'],0)

    def test_optional_absent_children_of_another_window_do_not_invent_a_hidden_panel(self):
        code,row=self.sample('outer-children-absent');self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)

    def test_root_enumeration_and_different_panels_share_the_visit_budget(self):
        for mode in ['outer-budget','outer-shared-budget']:
            with self.subTest(mode=mode):
                code,row=self.sample(mode);self.assertEqual(code,2,row);self.assertEqual(row['presses'],0);self.assertIn('5000',row['error'])

    def test_button_title_is_reused_while_searching_its_other_filename_attributes(self):
        code,row=self.sample('button-file');self.assertEqual(code,0,row);self.assertEqual(row['presses'],1)
        self.assertEqual(row['reads']['open:AXTitle'],1)
        self.assertEqual(row['reads']['open:AXDescription'],1)
        self.assertEqual(row['reads']['open:AXValue'],1)  # fixed batch, no second read

    def test_found_filename_keeps_unique_button_proof_with_fixed_auxiliary_prefetch(self):
        before,old=self.sample('large','before');after,new=self.sample('large')
        self.assertEqual((before,after),(0,0));self.assertEqual((old['presses'],new['presses']),(1,1))
        self.assertLess(new['queries'],old['queries']/2)
        for key in ['AXTitle','AXDescription','AXValue']:self.assertEqual(new['reads']['filler-199:'+key],1)
        self.assertEqual(new['reads']['filler-199:AXRole'],1);self.assertEqual(new['reads']['filler-199:AXChildren'],1)
        self.assertEqual(new['reads']['open:AXTitle'],1)
        print(json.dumps({'fixture':'203 unique panel nodes; filename before200unrelated nodes','before_queries':old['queries'],'after_queries':new['queries'],'presses':1,'guest_runtime_not_measured':True}))


if __name__=='__main__':unittest.main(verbosity=2)
