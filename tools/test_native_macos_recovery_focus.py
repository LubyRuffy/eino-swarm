"""Exercise the actual successor focus entry without opening a host window."""
from pathlib import Path
import json
import platform
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
FIXTURE = r'''
#import <Cocoa/Cocoa.h>
#include <unistd.h>
#include <stdio.h>
static NSString *mode;
static int activations, orders, keys, parentReads;
static id app, window, current, parent, front;
static void require(BOOL value, const char *reason) {
    if (!value) { fprintf(stderr, "%s\n", reason); exit(31); }
}
static pid_t fixtureParent(void) {
    parentReads++;
    if ([mode isEqual:@"orphan"] || ([mode isEqual:@"parent-changed"] && parentReads > 1)) return 1;
    return 900;
}
@interface FixtureThread : NSObject
+ (BOOL)isMainThread;
@end
@implementation FixtureThread
+ (BOOL)isMainThread {
    if ([mode hasPrefix:@"actual-"]) return [NSThread isMainThread];
    return ![mode isEqual:@"worker-thread"];
}
@end
@interface FixtureWindow : NSObject
- (BOOL)isMiniaturized;
- (void)makeKeyAndOrderFront:(id)sender;
- (void)makeKeyWindow;
@end
@implementation FixtureWindow
- (BOOL)isMiniaturized { return [mode isEqual:@"minimized"]; }
- (void)makeKeyAndOrderFront:(id)sender { orders++; require(activations == 1, "window ordered before self activation request"); }
- (void)makeKeyWindow { keys++; require(orders == 1, "key request preceded ordering"); }
@end
@interface FixtureApp : NSObject
- (BOOL)isHidden;
- (NSArray *)windows;
@end
@implementation FixtureApp
- (BOOL)isHidden { return [mode isEqual:@"hidden"]; }
- (NSArray *)windows { return [mode isEqual:@"foreign-window"] ? @[] : @[window]; }
@end
@interface FixtureRunning : NSObject
+ (id)currentApplication;
+ (id)runningApplicationWithProcessIdentifier:(pid_t)pid;
- (pid_t)processIdentifier;
- (NSString *)bundleIdentifier;
- (BOOL)isTerminated;
- (BOOL)activateWithOptions:(NSApplicationActivationOptions)options;
@end
@implementation FixtureRunning
+ (id)currentApplication { return [mode isEqual:@"no-self"] ? nil : current; }
+ (id)runningApplicationWithProcessIdentifier:(pid_t)pid { return [mode isEqual:@"no-parent"] ? nil : parent; }
- (pid_t)processIdentifier {
    if (self == current) return [mode isEqual:@"wrong-self"] ? getpid()+1 : getpid();
    if (self == parent) return [mode isEqual:@"wrong-parent"] ? 901 : 900;
    return [mode isEqual:@"user-switched"] ? 901 : 900;
}
- (NSString *)bundleIdentifier {
    if ([mode isEqual:@"empty-bundle"] && self == current) return @"";
    return self == parent && [mode isEqual:@"other-bundle"] ? @"other.app" : @"app.zwai.desktop";
}
- (BOOL)isTerminated { return ([mode isEqual:@"parent-terminated"] && self == parent) || ([mode isEqual:@"self-terminated"] && self == current); }
- (BOOL)activateWithOptions:(NSApplicationActivationOptions)options {
    require(self == current && options == 0, "activated an external app or changed activation policy");
    activations++;
    return ![mode isEqual:@"activation-refused"];
}
@end
@interface FixtureWorkspace : NSObject
+ (id)sharedWorkspace;
- (id)frontmostApplication;
@end
@implementation FixtureWorkspace
+ (id)sharedWorkspace { static id workspace; if (!workspace) workspace = [self new]; return workspace; }
- (id)frontmostApplication { return [mode isEqual:@"no-frontmost"] ? nil : front; }
@end
#define NSThread FixtureThread
#define NSWindow FixtureWindow
#define NSRunningApplication FixtureRunning
#define NSWorkspace FixtureWorkspace
#undef NSApp
#define NSApp ((FixtureApp *)app)
#define getppid fixtureParent
#include "recovery_focus_darwin.m"
int main(int argc, char **argv) { @autoreleasepool {
    mode = argc > 1 ? @(argv[1]) : @"normal";
    window = [FixtureWindow new]; app = [FixtureApp new];
    current = [FixtureRunning new]; parent = [FixtureRunning new]; front = [FixtureRunning new];
    id target = [mode isEqual:@"wrong-window-type"] ? [NSObject new] : window;
    if ([mode isEqual:@"actual-worker"]) {
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        dispatch_async(dispatch_get_global_queue(QOS_CLASS_DEFAULT, 0), ^{
            zwaiRecoveryFocus((__bridge void *)target);
            dispatch_semaphore_signal(done);
        });
        require(dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC)) == 0, "worker entry did not finish");
    } else {
        zwaiRecoveryFocus([mode isEqual:@"no-window"] ? NULL : (__bridge void *)target);
    }
    BOOL accepted = [mode isEqual:@"normal"] || [mode isEqual:@"actual-main"];
    BOOL refusedActivation = [mode isEqual:@"activation-refused"];
    require(activations == (accepted || refusedActivation ? 1 : 0), "wrong number of self activation requests");
    require(orders == (accepted ? 1 : 0) && keys == (accepted ? 1 : 0), "refusal changed window focus or success missed its one request");
    printf("guarded successor focus verified\n");
    return 0;
}}
'''


@unittest.skipUnless(platform.system() == "Darwin", "requires the macOS SDK")
class NativeRecoveryFocusTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        temporary = tempfile.TemporaryDirectory(prefix="recovery-focus-headless-")
        cls.addClassCleanup(temporary.cleanup)
        source = Path(temporary.name) / "test.m"
        cls.binary = Path(temporary.name) / "test"
        source.write_text(FIXTURE)
        result = subprocess.run([
            "clang", "-fobjc-arc", "-mmacosx-version-min=11.0", "-I", str(ROOT / "internal/desktop"),
            "-framework", "Cocoa", str(source), "-o", str(cls.binary),
        ], capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError("native focus fixture compilation failed:\n" + result.stdout + result.stderr)

    def test_actual_entry_requests_self_focus_once_only_while_parent_owns_foreground(self):
        for mode in ("normal", "actual-main", "actual-worker", "worker-thread", "orphan", "parent-changed", "minimized", "hidden",
                     "foreign-window", "no-self", "wrong-self", "no-parent", "wrong-parent",
                     "user-switched", "no-frontmost", "empty-bundle", "other-bundle",
                     "parent-terminated", "self-terminated", "no-window", "wrong-window-type",
                     "activation-refused"):
            with self.subTest(mode=mode):
                result = subprocess.run([str(self.binary), mode], capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("guarded successor focus verified", result.stdout)
                observations = [json.loads(line.removeprefix("ZWAI_RECOVERY_FOCUS "))
                                for line in result.stderr.splitlines() if line.startswith("ZWAI_RECOVERY_FOCUS ")]
                self.assertEqual(len(observations), 1, result.stderr)
                expected = {"normal": "activation_requested", "actual-main": "activation_requested", "actual-worker": "not_main",
                            "worker-thread": "not_main", "orphan": "no_parent_pid",
                            "parent-changed": "parent_changed", "minimized": "minimized", "hidden": "hidden",
                            "foreign-window": "foreign_window", "no-self": "no_current", "wrong-self": "wrong_current_pid",
                            "no-parent": "no_parent", "wrong-parent": "wrong_parent_pid", "user-switched": "frontmost_changed",
                            "no-frontmost": "frontmost_changed", "empty-bundle": "empty_current_bundle", "other-bundle": "bundle_mismatch",
                            "parent-terminated": "parent_terminated", "self-terminated": "current_terminated",
                            "no-window": "no_window", "wrong-window-type": "wrong_window_type", "activation-refused": "activation_refused"}[mode]
                self.assertEqual(observations[0]["reason"], expected)
                self.assertEqual(observations[0]["activation_requested"], mode in ("normal", "actual-main", "activation-refused"))
                self.assertIsInstance(observations[0]["pid"], int)
                self.assertNotIn("path", observations[0])
                if mode in ("normal", "actual-main"):
                    self.assertEqual(observations[0]["ppid"], 900)
                    self.assertEqual(observations[0]["frontmost_pid"], 900)
                    self.assertEqual(observations[0]["current_bundle"], "app.zwai.desktop")
                    self.assertEqual(observations[0]["parent_bundle"], "app.zwai.desktop")
                    self.assertFalse(observations[0]["hidden"])
                    self.assertFalse(observations[0]["minimized"])
                    self.assertTrue(observations[0]["main"])


if __name__ == "__main__":
    unittest.main()
