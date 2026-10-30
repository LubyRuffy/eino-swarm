"""Compile actual Objective-C activation/action logic with only OS/clock seams."""
from pathlib import Path
import json
import platform
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent


def function(source, signature):
    start = source.find(signature)
    if start < 0:
        return ""
    opening = source.index("{", start)
    depth = 1
    end = opening + 1
    while depth:
        depth += (source[end] == "{") - (source[end] == "}")
        end += 1
    return source[start:end] + "\n"


PREFIX = r'''
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CommonCrypto/CommonDigest.h>
#include <sys/sysctl.h>
#include <stdio.h>
static double clockValue=10, lastSlice=0;
static int pumps=0, activations=0, raises=0, presses=0, inputs=0, axReads=0, collections=0;
static NSString *mode;
static int identities=0;
@interface FixtureRunning:NSObject
@property BOOL active;
@property(readonly) NSURL *bundleURL;
@property(readonly) pid_t processIdentifier;
-(BOOL)activateWithOptions:(NSUInteger)options;
+(id)runningApplicationWithProcessIdentifier:(pid_t)pid;
@end
static FixtureRunning *runningFixture;
@implementation FixtureRunning
-(BOOL)activateWithOptions:(NSUInteger)options {
    activations++;
    if ([mode isEqual:@"activation-spent"]) clockValue+=3;
    return ![mode isEqual:@"request-false"];
}
+(id)runningApplicationWithProcessIdentifier:(pid_t)pid {return [mode isEqual:@"no-running"] ? nil : runningFixture;}
-(NSURL *)bundleURL {return [NSURL fileURLWithPath:[mode isEqual:@"wrong-bundle"] ? @"/other/case" : @"/private/case"];}
-(pid_t)processIdentifier {return [mode isEqual:@"wrong-running-pid"] ? 99 : 41;}
@end
@interface FixtureProcessInfo:NSObject
+(FixtureProcessInfo *)processInfo;
-(double)systemUptime;
@end
@implementation FixtureProcessInfo
+(FixtureProcessInfo *)processInfo {return [self new];}
-(double)systemUptime {return clockValue;}
@end
@interface FixtureDate:NSObject
+(NSDate *)dateWithTimeIntervalSinceNow:(NSTimeInterval)seconds;
@end
@implementation FixtureDate
+(NSDate *)dateWithTimeIntervalSinceNow:(NSTimeInterval)seconds {
    lastSlice=seconds; return [NSDate date];
}
@end
@interface FixtureRunLoop:NSObject
+(id)mainRunLoop;
-(BOOL)runMode:(NSRunLoopMode)mode beforeDate:(NSDate *)date;
@end
@implementation FixtureRunLoop
+(id)mainRunLoop {return [self new];}
-(BOOL)runMode:(NSRunLoopMode)unused beforeDate:(NSDate *)date {
    pumps++; clockValue+=lastSlice;
    if ([mode isEqual:@"late-active"]) clockValue=12.1;
    if ([mode isEqual:@"stale-true"]) runningFixture.active=NO;
    else if ([mode isEqual:@"several"]) runningFixture.active=pumps>=3;
    else if (![mode isEqual:@"never"]) runningFixture.active=YES;
    return YES;
}
@end
@interface FixtureWorkspace:NSObject
+(FixtureWorkspace *)sharedWorkspace;
-(FixtureWorkspace *)frontmostApplication;
-(pid_t)processIdentifier;
@end
@implementation FixtureWorkspace
+(FixtureWorkspace *)sharedWorkspace {return [self new];}
-(FixtureWorkspace *)frontmostApplication {return self;}
-(pid_t)processIdentifier {return [mode isEqual:@"wrong-front"] ? 2 : 41;}
@end
static AXError fixtureAction(AXUIElementRef element, CFStringRef action) {
    if (CFEqual(action,kAXRaiseAction)) raises++;
    if (CFEqual(action,kAXPressAction)) presses++;
    return [mode isEqual:@"press-error"] && CFEqual(action,kAXPressAction) ? kAXErrorFailure : 0;
}
static id fixtureWindow;
static id attribute(AXUIElementRef element, CFStringRef key) {
    axReads++;
    if (CFEqual(key,kAXWindowsAttribute)) {
        if ([mode hasPrefix:@"zero-window"]) return @[];
        if ([mode isEqual:@"malformed-windows"]) return @{@"not":@"array"};
        if ([mode isEqual:@"duplicate-window"]) return @[fixtureWindow,fixtureWindow];
        if ([mode isEqual:@"wrong-type"]) return @[@"not an AX element"];
        return @[fixtureWindow];
    }
    if (CFEqual(key,kAXTitleAttribute)) return @"fixture case-marker";
    return @[];
}
static AXError fixtureCopy(AXUIElementRef element, CFStringRef key, CFTypeRef *out) {
    axReads++;
    if (CFEqual(key,kAXWindowsAttribute)) {
        *out=CFBridgingRetain(attribute(element,key)); return kAXErrorSuccess;
    }
    if ([mode isEqual:@"unknown-role"]) return kAXErrorCannotComplete;
    if ([mode isEqual:@"missing-role"]) {*out=NULL;return kAXErrorSuccess;}
    if ([mode isEqual:@"malformed-role"]) {*out=CFRetain((__bridge CFTypeRef)@7);return kAXErrorSuccess;}
    if (CFEqual(key,kAXRoleAttribute)) {
        *out=CFRetain((__bridge CFStringRef)([mode isEqual:@"proxy-window"] ? @"AXApplication" : @"AXWindow"));
        return kAXErrorSuccess;
    }
    return kAXErrorAttributeUnsupported;
}
static AXError fixturePid(AXUIElementRef element,pid_t *pid) {
    axReads++; *pid=[mode isEqual:@"wrong-pid"] ? 99 : 41;
    return [mode isEqual:@"unknown-pid"] ? kAXErrorCannotComplete : kAXErrorSuccess;
}
static id attr(AXUIElementRef element, CFStringRef key) {return attribute(element,key);}
static NSDictionary *identity(pid_t pid) {identities++; return @{@"pid":@(pid),@"birth":@[@1,@([mode isEqual:@"pid-reused"] && activations>0 ? 3 : 2)],@"executable":@"/private/case/Contents/MacOS/zwai"};}
static NSString *env(NSString *key) {return [key isEqual:@"ZWAI_NATIVE_APP"] ? @"/private/case" : [key isEqual:@"ZWAI_NATIVE_CASE_ID"] ? @"case-marker" : @"";}
static BOOL bound(pid_t pid) {return ![mode isEqual:@"bound-lost"] || activations==0;}
static int fixtureSysctl(int *mib,u_int count,void *out,size_t *size,void *unused,size_t length) {
    NSMutableData *data=[NSMutableData data]; int argc=1; [data appendBytes:&argc length:sizeof(argc)];
    const char *value="/private/case/Contents/MacOS/zwai"; char zero=0;
    [data appendBytes:value length:strlen(value)+1]; [data appendBytes:&zero length:1];
    [data appendBytes:value length:strlen(value)+1];
    value="ZWAI_NATIVE_CASE_ID=case-marker"; [data appendBytes:value length:strlen(value)+1];
    if (!out) {*size=data.length;return 0;}
    memcpy(out,data.bytes,data.length); *size=data.length; return 0;
}
#define sysctl fixtureSysctl

static NSString *text(AXUIElementRef element, CFStringRef key) {return @"private window";}
static void fixtureRelease(CFTypeRef value) {}
static AXError fixtureSet(AXUIElementRef element,CFStringRef key,CFTypeRef value) {return 0;}
static CGEventSourceRef fixtureSource(CGEventSourceStateID value) {return (void*)1;}
static CGEventRef keyboard(CGEventSourceRef source,bool down,UniChar character) {return (void*)1;}
static void fixturePost(pid_t pid,CGEventRef event) {inputs++;}
static int fixtureSleep(useconds_t value) {clockValue+=value/1000000.;return 0;}
#define NSRunningApplication FixtureRunning
#define NSProcessInfo FixtureProcessInfo
#define NSRunLoop FixtureRunLoop
#define NSDate FixtureDate
#define NSWorkspace FixtureWorkspace
#define AXUIElementCopyAttributeValue fixtureCopy
#define AXUIElementGetPid fixturePid
#define AXUIElementPerformAction fixtureAction
#define AXUIElementSetAttributeValue fixtureSet
#define CGEventSourceCreate fixtureSource
#define CGEventPostToPid fixturePost
#define CFRelease fixtureRelease
#define usleep fixtureSleep
'''


@unittest.skipUnless(platform.system() == "Darwin", "Objective-C SDK is macOS-only")
class NativeActivationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="native-activation-headless-")
        cls.addClassCleanup(cls.temporary.cleanup)
        directory = Path(cls.temporary.name).resolve()
        bundle = directory / "zwai-native-case-marker-fixture/zwai.app"
        core = bundle / "Contents/MacOS/zwai"
        core.parent.mkdir(parents=True)
        core.write_bytes(b"frozen actual activation core\n")
        cls.binaries = {}
        cls.full_binaries = {}
        for label, name in (("smoke", "native_macos_ax.m"),
                            ("resilience", "native_macos_resilience_ax.m")):
            source = (ROOT / name).read_text()
            first_read = ("static BOOL axReadFailed; static AXError axReadStatus; static NSString *axReadKey,*axReadReason;\n"
                          + function(source, "static void rememberAXRead(")) if label == "smoke" else ""
            helpers = (first_read + function(source, "static void emit(")
                       + function(source, "static int fail(")
                       + function(source, "static int failAXRead(")
                       + function(source, "static BOOL waitForActivation(")
                       + function(source, "static int failSnapshot(")
                       + function(source, "static BOOL realWindow(")
                       + function(source, "static NSString *pasteDigest(")
                       + function(source, "static BOOL activationCase(")
                       + function(source, "static NSString *activationCoreHash(")
                       + function(source, "static NSString *string(")
                       + function(source, "static BOOL activationRecoveryProof(")
                       + function(source, "static NSString *activationBundle(")
                       + function(source, "static int activationRefused("))
            if label == "smoke":
                caller_start = ("    CFTypeRef rawWindows = NULL;" if "    CFTypeRef rawWindows = NULL;" in source
                                else "    NSArray *windows = attribute(app, kAXWindowsAttribute)")
                caller = source[source.index(caller_start):
                                source.index("    CGRect bounds = window ? frame(window)")]
                action = source[source.index("    BOOL frontmost = running.active;"):
                                source.index("    emit(result); CFRelease(app); return 0;")
                                + len("    emit(result); CFRelease(app); return 0;")]
                body = caller + r'''
    CGRect bounds=CGRectMake(0,0,800,600);
    NSMutableArray *areas=[NSMutableArray arrayWithObject:[NSObject new]];
    NSMutableArray *sends=[NSMutableArray arrayWithObject:[NSObject new]];
    NSMutableArray *urls=[NSMutableArray arrayWithObject:@"http://127.0.0.1:42"];
    NSMutableArray *cg=[NSMutableArray arrayWithObject:@{@"id":@17,@"owner_pid":@41}];
    NSMutableArray *tree=[NSMutableArray array];
    NSArray *values=[mode isEqual:@"bad-draft"] ? @[@"other"] : @[value];
    if ([action isEqual:@"fill"]) values=@[@"occupied"];
    BOOL sendEnabled=![mode isEqual:@"disabled"],visible=NO;
''' + action
            else:
                body = ""
                body += function(source, '    if ([action isEqual:@"activate"]) {')
                if "windows[" in body:
                    body = "id windows=attr(app,kAXWindowsAttribute);\n" + body
                body += "    return 3;\n"
            program = PREFIX + helpers + r'''
static int run(NSString *selected) {
    mode=selected; fixtureWindow=CFBridgingRelease(AXUIElementCreateApplication(41)); runningFixture=[FixtureRunning new];
    runningFixture.active=[@[@"already",@"request-false",@"stale-true"] containsObject:mode];
    NSRunningApplication *running=runningFixture;
    AXUIElementRef app=(void*)1; pid_t pid=41;
    NSString *bundle=@"/private/case",*origin=@"http://127.0.0.1:42",*value=@"exact draft";
    NSString *action=[mode hasPrefix:@"fill-"] ? @"fill" : @"send";
    if ([mode isEqual:@"zero-window-snapshot"]) action=@"snapshot";
''' + ('    action=@"activate";\n' if label == "resilience" else "") + body + r'''
}
int main(int argc,const char **argv) {@autoreleasepool {
    int code=run(@(argv[1]));
    fprintf(stderr,"{\"activations\":%d,\"raises\":%d,\"presses\":%d,\"inputs\":%d,\"ax_reads\":%d,\"pumps\":%d,\"clock\":%.6f,\"slice\":%.6f}\n",
            activations,raises,presses,inputs,axReads,pumps,clockValue,lastSlice);
    return code;
}}
'''
            path = directory / (label + ".m")
            path.write_text(program.replace("/private/case", str(bundle)))
            binary = directory / label
            built = subprocess.run(["/usr/bin/xcrun", "clang", "-fobjc-arc",
                                    "-framework", "AppKit", "-framework", "ApplicationServices",
                                    "-framework", "CoreGraphics", str(path), "-o", str(binary)],
                                   capture_output=True, text=True)
            if built.returncode:
                raise RuntimeError(built.stderr)
            cls.binaries[label] = binary
            full = directory / ("full-" + label)
            subprocess.run(["/usr/bin/xcrun", "clang", "-fobjc-arc",
                            "-mmacosx-version-min=13.0", "-framework", "Foundation",
                            "-framework", "AppKit", "-framework", "ApplicationServices",
                            "-framework", "CoreGraphics", str(ROOT / name), "-o", str(full)],
                           capture_output=True, text=True, check=True)
            cls.full_binaries[label] = full

            if label == "resilience":
                start = source.index('    if (![windows isKindOfClass:NSArray.class] || [windows count] != 1)',
                                     source.index('chosen.stdout.after'))
                stop = source.index('    NSMutableArray *values',start)
                root = source[start:stop]
                actions = source[source.index('    if ([action isEqual:@"fill"] || [action isEqual:@"press"]'):source.index('    } else if ([action isEqual:@"paste-file"])')]
                program = PREFIX + function(source,"static int fail(") + function(source,"static BOOL realWindow(") + r"""
static BOOL exactText(AXUIElementRef element,NSString *value,NSUInteger depth) {return NO;}
static NSString *string(id value) {return [value isKindOfClass:NSString.class] ? value : @"";}
static void key(pid_t pid,CGKeyCode code,CGEventFlags flags) {inputs++;}
static void type(pid_t pid,NSString *text) {inputs++;}
static void collect(AXUIElementRef item,NSMutableArray *areas,NSMutableArray *buttons,NSMutableString *text,NSUInteger *count,BOOL *web) {
    collections++; [areas addObject:(__bridge id)item]; [buttons addObject:(__bridge id)item];
}
static int run(NSString *selected,NSString *action) {
    mode=selected; fixtureWindow=CFBridgingRelease(AXUIElementCreateApplication(41));
    runningFixture=[FixtureRunning new];
    AXUIElementRef app=AXUIElementCreateApplication(41); pid_t pid=41;
    id windows=attr(app,kAXWindowsAttribute); NSString *value=@"fixture case-marker";
""" + root + r"""
    NSMutableDictionary *result=[NSMutableDictionary dictionary];
""" + actions + r"""
    }
    CFRelease(app); return 0;
}
int main(int argc,const char **argv) {@autoreleasepool {
    int code=run(@(argv[1]),@(argv[2]));
    fprintf(stderr,"{\"collections\":%d,\"raises\":%d,\"presses\":%d,\"inputs\":%d,\"activations\":%d}\n",collections,raises,presses,inputs,activations);
    return code;
}}
"""
                path = directory / "resilience-window.m"
                path.write_text(program)
                binary = directory / "resilience-window"
                subprocess.run(["/usr/bin/xcrun","clang","-O2","-fobjc-arc","-framework","AppKit",
                                "-framework","ApplicationServices","-framework","CoreGraphics",str(path),"-o",str(binary)],
                               capture_output=True,text=True,check=True)
                cls.window_binary = binary

    def run_mode(self, helper, mode):
        result = subprocess.run([str(self.binaries[helper]), mode], capture_output=True,
                                text=True, timeout=3)
        lines = result.stderr.splitlines()
        calls = json.loads(lines[-1])
        calls["stderr"] = "\n".join(lines[:-1])
        return result.returncode, json.loads(result.stdout) if result.stdout.strip() else {}, calls

    def test_main_runloop_refreshes_cached_active_without_reactivation(self):
        for helper in self.binaries:
            for mode, pumps in (("cached", 1), ("several", 3)):
                with self.subTest(helper=helper, mode=mode):
                    code, row, calls = self.run_mode(helper, mode)
                    self.assertEqual(code, 0)
                    self.assertEqual(calls["activations"], 1)
                    self.assertEqual(calls["raises"], 1 if helper == "smoke" else 0)
                    self.assertEqual(calls["pumps"], pumps)
                    self.assertLessEqual(calls["slice"], .01)
                    self.assertTrue(row["frontmost"] if helper == "smoke" else row["active"])

    def test_activation_timeout_does_not_allocate_another_wait_or_press(self):
        for helper in self.binaries:
            for mode in ("never", "activation-spent", "late-active", "stale-true"):
                with self.subTest(helper=helper, mode=mode):
                    code, row, calls = self.run_mode(helper, mode)
                    self.assertEqual(code, 2)
                    self.assertEqual(calls["activations"], 1)
                    self.assertEqual(calls["raises"], 1 if helper == "smoke" else 0)
                    self.assertEqual(calls["presses"], 0)
                    self.assertFalse(row["activation_ready"])
                    if mode == "activation-spent":
                        self.assertEqual(calls["pumps"], 0)
                    elif mode != "late-active":
                        self.assertLessEqual(calls["clock"], 12.011)
                    if helper == "resilience":
                        for field in ("active=", "frontmost_pid=", "activated=", "elapsed="):
                            self.assertIn(field, calls["stderr"])
                    else:
                        for field in ("ready", "frontmost", "area_values", "send_enabled",
                                      "send_count", "activation_elapsed_seconds"):
                            self.assertIn(field, row)
                        self.assertFalse(row["press_attempted"])

    def test_send_and_fill_guard_failure_retains_predicates_without_input(self):
        for mode in ("bad-draft", "disabled", "fill-occupied"):
            with self.subTest(mode=mode):
                code, row, calls = self.run_mode("smoke", mode)
                self.assertEqual(code, 2)
                for key in ("ready", "frontmost", "area_values", "send_count", "send_enabled"):
                    self.assertIn(key, row)
                self.assertFalse(row["press_attempted"])
                self.assertTrue(row["ready"])
                self.assertEqual(row["area_values"], ["other" if mode == "bad-draft"
                                                     else "occupied" if mode.startswith("fill-")
                                                     else "exact draft"])
                self.assertEqual((calls["presses"], calls["inputs"]), (0, 0))

    def test_already_active_is_refreshed_and_request_result_is_not_authority(self):
        for helper in self.binaries:
            for mode in ("already", "request-false"):
                with self.subTest(helper=helper, mode=mode):
                    code, row, calls = self.run_mode(helper, mode)
                    self.assertEqual(code, 0)
                    self.assertEqual((calls["activations"], calls["pumps"]), (1, 1))
                    self.assertEqual(calls["presses"], 1 if helper == "smoke" else 0)
                    if helper == "smoke":
                        self.assertTrue(row["submitted"])
                        self.assertEqual(row["activation_requested"], mode != "request-false")
                    else:
                        self.assertEqual(row["activated"], mode != "request-false")

    def test_press_error_is_an_attempt_and_is_not_retried(self):
        code, row, calls = self.run_mode("smoke", "press-error")
        self.assertEqual(code, 2)
        self.assertTrue(row["press_attempted"])
        self.assertEqual(calls["presses"], 1)
        self.assertNotIn("submitted", row)

    def test_resilience_keeps_actual_frontmost_pid_for_the_caller_guard(self):
        code, row, calls = self.run_mode("resilience", "wrong-front")
        self.assertEqual(code, 0)
        self.assertTrue(row["active"])
        self.assertEqual(row["frontmost_pid"], 2)
        self.assertEqual(calls["presses"], 0)


    def test_proxy_wrong_and_unknown_windows_refuse_before_any_window_action(self):
        for mode in ("proxy-window","unknown-role","missing-role","malformed-role","wrong-pid","unknown-pid","wrong-type","duplicate-window","malformed-windows"):
            with self.subTest(helper="smoke",mode=mode):
                code,row,calls=self.run_mode("smoke",mode)
                self.assertEqual(code,2)
                self.assertEqual((calls["raises"],calls["presses"],calls["inputs"]),(0,0,0))
            for action in ("snapshot","fill","press","select-thread"):
                with self.subTest(helper="resilience",action=action,mode=mode):
                    result=subprocess.run([str(self.window_binary),mode,action],capture_output=True,text=True,timeout=3)
                    calls=json.loads(result.stderr.splitlines()[-1])
                    self.assertEqual(result.returncode,2)
                    self.assertEqual([calls[key] for key in ("collections","raises","presses","inputs","activations")],[0]*5)

    def test_resilience_activation_is_ns_only_even_when_ax_window_is_a_proxy(self):
        for mode in ("proxy-window","zero-window","unknown-role","wrong-pid","duplicate-window"):
            with self.subTest(mode=mode):
                code,row,calls=self.run_mode("resilience",mode)
                self.assertEqual(code,0)
                self.assertTrue(row["activation_only"])
                self.assertEqual((calls["ax_reads"],calls["raises"],calls["presses"],calls["inputs"]),(0,0,0,0))

    def test_resilience_real_window_still_reaches_original_actions(self):
        for action in ("snapshot","fill","press","select-thread"):
            with self.subTest(action=action):
                result=subprocess.run([str(self.window_binary),"real-window",action],capture_output=True,text=True,timeout=3)
                self.assertEqual(result.returncode,0,result.stderr)
                calls=json.loads(result.stderr.splitlines()[-1])
                self.assertEqual(calls["collections"],1)
                self.assertEqual(calls["raises"],0 if action=="snapshot" else 1)
                self.assertEqual(calls["activations"],0 if action=="snapshot" else 1)
                self.assertEqual(calls["presses"],1 if action in ("press","select-thread") else 0)

    def test_zero_smoke_window_remains_pending_and_cannot_send(self):
        for mode, expected in (("zero-window",2),("zero-window-snapshot",0)):
            code,row,calls=self.run_mode("smoke",mode)
            self.assertEqual(code,expected)
            self.assertFalse(row["ready"])
            # An empty AX list must not suppress the already-guarded application
            # activation. Window raising and input still require a real window.
            self.assertTrue(row["activation_requested"])
            self.assertEqual(calls["activations"],1)
            self.assertEqual((calls["raises"],calls["presses"],calls["inputs"]),(0,0,0))

    def test_ns_activation_refuses_changed_identity_without_any_ax_work(self):
        for mode in ("no-running","wrong-bundle","wrong-running-pid","pid-reused","bound-lost"):
            with self.subTest(mode=mode):
                code,row,calls=self.run_mode("resilience",mode)
                self.assertEqual(code,2)
                self.assertEqual((calls["ax_reads"],calls["raises"],calls["presses"],calls["inputs"]),(0,0,0,0))
                self.assertEqual(calls["activations"],1 if mode in ("pid-reused","bound-lost") else 0)

    def test_ns_only_dispatch_precedes_every_ax_api_in_actual_main(self):
        source=(ROOT/"native_macos_resilience_ax.m").read_text()
        main=source[source.index("int main("):]
        activation=main.index('if ([action isEqual:@"activate"])')
        for call in ("AXIsProcessTrusted()","AXUIElementCreateApplication(pid)","attr(app, kAXWindowsAttribute)"):
            self.assertLess(activation,main.index(call))
        self.assertNotIn("AX",function(main,'    if ([action isEqual:@"activate"]) {').replace("an AX window","").replace("validated window", ""))

    def test_real_sdk_main_refuses_no_target_without_window_calls(self):
        # No seams here: argc refuses before permission/window/input calls on a VM too.
        for helper, binary in self.full_binaries.items():
            with self.subTest(helper=helper):
                result = subprocess.run([str(binary)], capture_output=True, text=True, timeout=3)
                self.assertEqual(result.returncode, 2)
                if helper == "smoke":
                    self.assertIn(json.loads(result.stdout)["error"],
                                  ("macOS VM required; physical host refused",
                                   "expected pid action case-id bundle origin value"))
                else:
                    self.assertIn("guest case required", result.stderr)


STARTUP_PREFIX = r"""
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CommonCrypto/CommonDigest.h>
#include <sys/sysctl.h>
#include <sys/sysctl.h>
#include <unistd.h>
static NSString *startupMode;
static NSMutableData *startupArguments;
static int startupAX, startupActivations, startupRaises, startupInputs, startupLookups;
@interface StartupEmptyURL:NSObject
@property(readonly) NSString *path;
@end
@implementation StartupEmptyURL
-(NSString *)path {return @"";}
@end
@interface StartupRunning:NSObject
+(instancetype)runningApplicationWithProcessIdentifier:(pid_t)pid;
@property(readonly) NSURL *bundleURL;
@property(readonly) pid_t processIdentifier;
@property(readonly,getter=isActive) BOOL active;
-(BOOL)activateWithOptions:(NSApplicationActivationOptions)options;
@end
@implementation StartupRunning
+(instancetype)runningApplicationWithProcessIdentifier:(pid_t)pid {
    startupLookups++; return [startupMode isEqual:@"no-running"] ? nil : [self new];
}
-(NSURL *)bundleURL {
    if ([startupMode isEqual:@"empty-url"]) return nil;
    if ([startupMode isEqual:@"empty-path"]) return (NSURL *)[StartupEmptyURL new];
    return [NSURL fileURLWithPath:[startupMode isEqual:@"wrong-bundle"] ? @"/private/other-case" : @"/private/case"];
}
-(pid_t)processIdentifier {return [startupMode isEqual:@"wrong-running-pid"] ? 99 : 41;}
-(BOOL)isActive {return NO;}
-(BOOL)activateWithOptions:(NSApplicationActivationOptions)options {startupActivations++;return NO;}
@end
static int startupSysctlByName(const char *name,void *old,size_t *size,void *newValue,size_t newSize) {
    if (strcmp(name,"kern.hv_vmm_present") || *size!=sizeof(int)) abort();
    *(int *)old=1;return 0;
}
static int startupSysctl(int *mib,u_int count,void *old,size_t *size,void *newValue,size_t newSize) {
    if (count!=3 || mib[1]!=KERN_PROCARGS2 || mib[2]!=41) abort();
    if ([startupMode isEqual:@"unreadable-kernel"]) return -1;
    if (!old) {*size=startupArguments.length;return 0;}
    memcpy(old,startupArguments.bytes,startupArguments.length);*size=startupArguments.length;return 0;
}
static BOOL startupTrusted(void) {startupAX++;return NO;}
static bool startupCapture(void) {startupAX++;return false;}
static AXError startupPerform(AXUIElementRef item,CFStringRef action) {startupRaises++;return kAXErrorFailure;}
static AXError startupCopy(AXUIElementRef item,CFStringRef name,CFTypeRef *out) {startupAX++;*out=NULL;return kAXErrorAttributeUnsupported;}
static AXError startupGetPid(AXUIElementRef item,pid_t *pid) {startupAX++;return kAXErrorInvalidUIElement;}
static void startupPost(pid_t pid,CGEventRef event) {startupInputs++;}
#define NSRunningApplication StartupRunning
#define sysctlbyname startupSysctlByName
#define sysctl startupSysctl
#define AXIsProcessTrusted startupTrusted
#define CGPreflightScreenCaptureAccess startupCapture
#define AXUIElementPerformAction startupPerform
#define AXUIElementCopyAttributeValue startupCopy
#define AXUIElementGetPid startupGetPid
#define CGEventPostToPid startupPost
#define main startup_actual_main
"""


@unittest.skipUnless(platform.system() == "Darwin", "Objective-C SDK is macOS-only")
class NativeStartupRegistrationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="native-startup-actual-main-")
        cls.addClassCleanup(cls.temporary.cleanup)
        directory = Path(cls.temporary.name)
        path = directory / "startup.m"
        path.write_text(STARTUP_PREFIX + '#include "' + str(ROOT / "native_macos_ax.m") + '"\n' + r"""
#undef main
int main(int argc,const char **argv) {@autoreleasepool {
    if (argc!=3) return 7;
    startupMode=@(argv[1]);startupArguments=[NSMutableData data];
    int one=1;[startupArguments appendBytes:&one length:sizeof(one)];
    const char *executable="/private/case/Contents/MacOS/zwai";
    [startupArguments appendBytes:executable length:strlen(executable)+1];
    char zero=0;[startupArguments appendBytes:&zero length:1];
    [startupArguments appendBytes:executable length:strlen(executable)+1];
    const char *marker=[startupMode isEqual:@"wrong-marker"] ? "ZWAI_NATIVE_CASE_ID=other-case" : "ZWAI_NATIVE_CASE_ID=case-marker";
    [startupArguments appendBytes:marker length:strlen(marker)+1];
    const char *input[]={"native","41",argv[2],"case-marker","/private/case","http://127.0.0.1:42",""};
    int code=startup_actual_main(7,input);
    fprintf(stderr,"{\"ax_calls\":%d,\"activations\":%d,\"raises\":%d,\"inputs\":%d,\"ns_lookups\":%d}\n",
            startupAX,startupActivations,startupRaises,startupInputs,startupLookups);
    return code;
}}
""")
        cls.binary = directory / "startup"
        built = subprocess.run(["/usr/bin/xcrun", "clang", "-fobjc-arc", "-framework", "AppKit",
                                "-framework", "ApplicationServices", "-framework", "CoreGraphics",
                                str(path), "-o", str(cls.binary)], capture_output=True, text=True)
        if built.returncode:
            raise RuntimeError(built.stderr)

    def observe(self, mode, action):
        call = subprocess.run([str(self.binary), mode, action], capture_output=True, text=True, timeout=3)
        return call.returncode, json.loads(call.stdout), json.loads(call.stderr)

    def assert_no_actions(self, calls):
        self.assertEqual([calls[key] for key in ("ax_calls", "activations", "raises", "inputs")], [0]*4)

    def test_snapshot_waits_only_for_missing_native_registration_without_any_actions(self):
        for mode, state in (("no-running", "not_registered"), ("empty-url", "bundle_unavailable"),
                            ("empty-path", "bundle_unavailable")):
            with self.subTest(mode=mode):
                code, row, calls = self.observe(mode, "snapshot")
                self.assertEqual(code, 0)
                self.assertIs(row["ready"], False)
                self.assertIs(row["registration_pending"], True)
                self.assertEqual(row["registration_state"], state)
                self.assertEqual(row["pid"], 41)
                self.assertEqual(row["origin"], "http://127.0.0.1:42")
                self.assertEqual(row["bundle"], "/private/case")
                self.assertEqual(row["requested_bundle"], "/private/case")
                self.assertIs(row["bundle_verified"], False)
                self.assertIs(row["case_verified"], False)
                self.assert_no_actions(calls)

    def test_fill_send_and_unknown_action_never_treat_missing_registration_as_permission(self):
        for mode in ("no-running", "empty-url", "empty-path"):
            for action in ("fill", "send", "unknown"):
                with self.subTest(mode=mode, action=action):
                    code, row, calls = self.observe(mode, action)
                    self.assertEqual(code, 2)
                    self.assertNotIn("registration_pending", row)
                    self.assert_no_actions(calls)

    def test_wrong_bundle_pid_marker_and_unreadable_kernel_refuse_before_ax(self):
        for mode in ("wrong-bundle", "wrong-running-pid", "wrong-marker", "unreadable-kernel"):
            for action in ("snapshot", "fill", "send"):
                with self.subTest(mode=mode, action=action):
                    code, row, calls = self.observe(mode, action)
                    self.assertEqual(code, 2)
                    self.assertNotIn("registration_pending", row)
                    self.assert_no_actions(calls)
                    if mode in ("wrong-marker", "unreadable-kernel"):
                        self.assertEqual(calls["ns_lookups"], 0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
