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
#include <stdio.h>
static double clockValue=10, lastSlice=0;
static int pumps=0, activations=0, raises=0, presses=0, inputs=0;
static NSString *mode;
@interface FixtureRunning:NSObject
@property BOOL active;
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
+(id)runningApplicationWithProcessIdentifier:(pid_t)pid {return runningFixture;}
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
static id attribute(AXUIElementRef element, CFStringRef key) {
    return CFEqual(key,kAXWindowsAttribute) ? @[[NSObject new]] : @[];
}
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
        directory = Path(cls.temporary.name)
        cls.binaries = {}
        cls.full_binaries = {}
        for label, name in (("smoke", "native_macos_ax.m"),
                            ("resilience", "native_macos_resilience_ax.m")):
            source = (ROOT / name).read_text()
            helpers = (function(source, "static void emit(")
                       + function(source, "static int fail(")
                       + function(source, "static BOOL waitForActivation(")
                       + function(source, "static int failSnapshot("))
            if label == "smoke":
                caller = source[source.index("    NSArray *windows = attribute(app, kAXWindowsAttribute)"):
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
                body = "    NSArray *windows=@[[NSObject new]];\n"
                body += source[source.index('    if ([action isEqual:@"activate"]) {'):
                               source.index('    if ([action isEqual:@"quit"]) {')]
                body += "    return 3;\n"
            program = PREFIX + helpers + r'''
static int run(NSString *selected) {
    mode=selected; runningFixture=[FixtureRunning new];
    runningFixture.active=[@[@"already",@"request-false",@"stale-true"] containsObject:mode];
    NSRunningApplication *running=runningFixture;
    AXUIElementRef app=(void*)1; pid_t pid=41;
    NSString *bundle=@"/private/case",*origin=@"http://127.0.0.1:42",*value=@"exact draft";
    NSString *action=[mode hasPrefix:@"fill-"] ? @"fill" : @"send";
''' + ('    action=@"activate";\n' if label == "resilience" else "") + body + r'''
}
int main(int argc,const char **argv) {@autoreleasepool {
    int code=run(@(argv[1]));
    fprintf(stderr,"{\"activations\":%d,\"raises\":%d,\"presses\":%d,\"inputs\":%d,\"pumps\":%d,\"clock\":%.6f,\"slice\":%.6f}\n",
            activations,raises,presses,inputs,pumps,clockValue,lastSlice);
    return code;
}}
'''
            path = directory / (label + ".m")
            path.write_text(program)
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

    def run_mode(self, helper, mode):
        result = subprocess.run([str(self.binaries[helper]), mode], capture_output=True,
                                text=True, timeout=3)
        lines = result.stderr.splitlines()
        calls = json.loads(lines[-1])
        calls["stderr"] = "\n".join(lines[:-1])
        return result.returncode, json.loads(result.stdout), calls

    def test_main_runloop_refreshes_cached_active_without_reactivation(self):
        for helper in self.binaries:
            for mode, pumps in (("cached", 1), ("several", 3)):
                with self.subTest(helper=helper, mode=mode):
                    code, row, calls = self.run_mode(helper, mode)
                    self.assertEqual(code, 0)
                    self.assertEqual(calls["activations"], 1)
                    self.assertEqual(calls["raises"], 1)
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
                    self.assertEqual(calls["raises"], 1)
                    self.assertEqual(calls["presses"], 0)
                    self.assertFalse(row["activation_ready"])
                    if mode == "activation-spent":
                        self.assertEqual(calls["pumps"], 0)
                    elif mode != "late-active":
                        self.assertLessEqual(calls["clock"], 12.011)
                    if helper == "resilience":
                        for field in ("active=", "frontmost_pid=", "activated=", "raise_error=", "elapsed="):
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


if __name__ == "__main__":
    unittest.main(verbosity=2)
