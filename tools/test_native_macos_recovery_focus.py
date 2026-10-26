"""Exercise the actual parent coordinator using AppKit/kernel seams, without host GUI."""
from pathlib import Path
import json
import platform
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = r'''
#import <Cocoa/Cocoa.h>
#include <libproc.h>
#include <sys/proc_info.h>
#include <unistd.h>
#include <string.h>
#include <stdio.h>
static NSString *mode;
static int activations, yields, legacy, kernelReads, frontReads;
static id app, window, panel, current, child, other;
static void require(BOOL value, const char *reason) { if (!value) { fprintf(stderr, "%s\n", reason); exit(31); } }
static int fixturePidInfo(int pid, int flavor, uint64_t arg, void *buffer, int size) {
    kernelReads++;
    if ([mode isEqual:@"kernel-missing"]) return 0;
    struct proc_bsdinfo *info = buffer; memset(info, 0, sizeof(*info));
    info->pbi_pid = [mode isEqual:@"wrong-kernel-pid"] ? 901 : pid;
    info->pbi_ppid = [mode isEqual:@"foreign-child"] ? getpid()+1 : getpid();
    info->pbi_start_tvsec = [mode isEqual:@"birth-changed"] || ([mode isEqual:@"late-birth-change"] && kernelReads > 1) ? 11 : 10;
    info->pbi_start_tvusec = 20;
    return sizeof(*info);
}
static int fixturePidPath(int pid, void *buffer, uint32_t size) {
    if ([mode isEqual:@"kernel-path-missing"]) return 0;
    strcpy(buffer, [mode isEqual:@"kernel-path-changed"] ? "/fixture/other" : "/fixture/child"); return 15;
}
static char *fixtureRealpath(const char *path, char *resolved) {
    if ([mode isEqual:@"realpath-failed"]) return NULL;
    strcpy(resolved, path); return resolved;
}
@interface FixtureThread : NSObject
+ (BOOL)isMainThread;
@end
@implementation FixtureThread
+ (BOOL)isMainThread { return [mode isEqual:@"not-main"] ? NO : [NSThread isMainThread]; }
@end
@interface FixtureWindow : NSObject
- (BOOL)isMiniaturized;
- (BOOL)isVisible;
@end
@implementation FixtureWindow
- (BOOL)isMiniaturized { return [mode isEqual:@"minimized"]; }
- (BOOL)isVisible { return ![mode isEqual:@"invisible"]; }
@end
@interface FixtureApp : NSObject
- (BOOL)isHidden;
- (id)mainWindow;
- (id)keyWindow;
- (NSArray *)windows;
- (void)yieldActivationToApplication:(id)target;
@end
@implementation FixtureApp
- (BOOL)isHidden { return [mode isEqual:@"hidden"]; }
- (id)mainWindow { return [mode isEqual:@"no-window"] ? nil : [mode isEqual:@"bad-window-type"] ? [NSObject new] : window; }
- (id)keyWindow { return [mode isEqual:@"native-panel"] ? panel : window; }
- (NSArray *)windows { return [mode isEqual:@"foreign-window"] ? @[] : @[window]; }
- (void)yieldActivationToApplication:(id)target { require(target == child && activations == 0, "yield did not precede the one owned-child request"); yields++; }
@end
@interface FixtureRunning : NSObject
+ (id)currentApplication;
+ (id)runningApplicationWithProcessIdentifier:(pid_t)pid;
- (pid_t)processIdentifier;
- (NSString *)bundleIdentifier;
- (NSURL *)executableURL;
- (BOOL)isTerminated;
- (BOOL)isActive;
- (BOOL)isHidden;
- (BOOL)isFinishedLaunching;
- (NSApplicationActivationPolicy)activationPolicy;
- (BOOL)activateWithOptions:(NSApplicationActivationOptions)options;
@end
@implementation FixtureRunning
+ (id)currentApplication { return [mode isEqual:@"no-current"] ? nil : current; }
+ (id)runningApplicationWithProcessIdentifier:(pid_t)pid { require(pid == 900, "looked up an unowned target"); return [mode isEqual:@"unregistered"] ? nil : child; }
- (pid_t)processIdentifier {
    if (self == current) return [mode isEqual:@"wrong-current-pid"] ? getpid()+1 : getpid();
    if (self == child) return [mode isEqual:@"wrong-child-pid"] ? 901 : 900;
    return 901;
}
- (NSString *)bundleIdentifier {
    if (self == current && [mode isEqual:@"empty-bundle"]) return @"";
    return self == child && [mode isEqual:@"bundle-mismatch"] ? @"other.app" : @"app.zwai.desktop";
}
- (NSURL *)executableURL { return [mode isEqual:@"no-child-executable"] ? nil : [NSURL fileURLWithPath:[mode isEqual:@"child-executable-changed"] ? @"/fixture/other" : @"/fixture/child"]; }
- (BOOL)isTerminated { return (self == current && [mode isEqual:@"current-terminated"]) || (self == child && [mode isEqual:@"child-terminated"]); }
- (BOOL)isActive { return self == current ? ![mode isEqual:@"parent-inactive"] : [mode isEqual:@"already-frontmost"]; }
- (BOOL)isHidden { return self == child && [mode isEqual:@"child-hidden"]; }
- (BOOL)isFinishedLaunching { return ![mode isEqual:@"launching"]; }
- (NSApplicationActivationPolicy)activationPolicy { return NSApplicationActivationPolicyRegular; }
- (BOOL)activateWithOptions:(NSApplicationActivationOptions)options {
    require(self == child && options == 0 && yields == ([mode isEqual:@"legacy-api"] ? 0 : 1), "parent-owned request changed receiver/options/yield order");
    if ([mode isEqual:@"legacy-api"]) legacy++;
    activations++; return ![mode isEqual:@"activation-refused"];
}
@end
@interface FixtureWorkspace : NSObject
+ (instancetype)sharedWorkspace;
- (id)frontmostApplication;
@end
@implementation FixtureWorkspace
+ (instancetype)sharedWorkspace { static id value; if (!value) value = [self new]; return value; }
- (id)frontmostApplication {
    frontReads++;
    if ([mode isEqual:@"already-frontmost"]) return child;
    if ([mode isEqual:@"user-switched"] || ([mode isEqual:@"late-user-switch"] && frontReads > 1)) return other;
    return current;
}
@end
#define NSThread FixtureThread
#define NSWindow FixtureWindow
#define NSRunningApplication FixtureRunning
#define NSWorkspace FixtureWorkspace
#undef NSApp
#define NSApp ((FixtureApp *)app)
#define proc_pidinfo fixturePidInfo
#define proc_pidpath fixturePidPath
#define realpath fixtureRealpath
#include "recovery_focus_darwin.m"
int main(int argc, char **argv) { @autoreleasepool {
    mode = argc > 1 ? @(argv[1]) : @"normal";
    window = [FixtureWindow new]; panel = [FixtureWindow new]; app = [FixtureApp new];
    current = [FixtureRunning new]; child = [FixtureRunning new]; other = [FixtureRunning new];
    if ([mode isEqual:@"legacy-api"]) {
        require(activateRecoveryChild(child, NO) && activations == 1 && legacy == 1 && yields == 0, "legacy API fallback did not request once");
        printf("legacy API verified\n"); return 0;
    }
    if ([mode isEqual:@"capture-invalid"]) {
        zwaiRecoveryBirth value;
        require(!zwaiRecoveryCapture(1, "/fixture/child", &value) && !zwaiRecoveryCapture(900, NULL, &value)
            && !zwaiRecoveryCapture(900, "/fixture/child", NULL), "invalid capture input accepted");
        printf("capture boundary verified\n"); return 0;
    }
    int status;
    if ([mode isEqual:@"actual-worker"]) {
        __block int result = -1;
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        dispatch_async(dispatch_get_global_queue(QOS_CLASS_DEFAULT, 0), ^{
            result = zwaiRecoveryFocusSuccessor(900, "/fixture/child", (zwaiRecoveryBirth){10,20}); dispatch_semaphore_signal(done);
        });
        require(dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC)) == 0, "worker did not finish"); status = result;
    } else status = zwaiRecoveryFocusSuccessor(900, "/fixture/child", (zwaiRecoveryBirth){10,20});
    BOOL requested = [mode isEqual:@"normal"] || [mode isEqual:@"activation-refused"];
    BOOL pending = [mode isEqual:@"unregistered"] || [mode isEqual:@"launching"];
    require(activations == (requested ? 1 : 0) && yields == (requested ? 1 : 0) && legacy == 0, "refusal/pending activated or success missed one cooperative request");
    require(!pending || status == 0, "registration did not stay pending");
    require(!requested || status == 2, "request was confused with readiness");
    printf("owned parent coordination verified status=%d\n", status); return 0;
}}
'''


@unittest.skipUnless(platform.system() == "Darwin", "requires the macOS SDK")
class NativeRecoveryFocusTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        temporary = tempfile.TemporaryDirectory(prefix="recovery-parent-headless-")
        cls.addClassCleanup(temporary.cleanup)
        source = Path(temporary.name) / "test.m"
        cls.binary = Path(temporary.name) / "test"
        source.write_text(FIXTURE)
        result = subprocess.run(["clang", "-fobjc-arc", "-mmacosx-version-min=11.0", "-I", str(ROOT / "internal/desktop"),
                                 "-framework", "Cocoa", "-lproc", str(source), "-o", str(cls.binary)], capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError("native parent fixture compilation failed:\n" + result.stdout + result.stderr)

    def test_parent_owns_one_cooperative_request_and_refusals_do_no_action(self):
        reasons = {"normal": "activation_requested", "activation-refused": "activation_refused",
                   "not-main": "not_main", "actual-worker": "not_main", "kernel-missing": "child_identity_changed",
                   "wrong-kernel-pid": "child_identity_changed", "foreign-child": "child_identity_changed",
                   "kernel-path-missing": "child_identity_changed", "kernel-path-changed": "child_identity_changed",
                   "realpath-failed": "child_identity_changed", "birth-changed": "child_identity_changed",
                   "late-birth-change": "child_identity_changed", "no-current": "invalid_current",
                   "wrong-current-pid": "invalid_current", "current-terminated": "invalid_current",
                   "already-frontmost": "already_frontmost", "user-switched": "parent_not_frontmost",
                   "late-user-switch": "parent_not_frontmost", "parent-inactive": "parent_not_frontmost",
                   "hidden": "parent_hidden", "no-window": "no_parent_window", "bad-window-type": "no_parent_window",
                   "foreign-window": "foreign_parent_window", "minimized": "parent_window_unavailable",
                   "invisible": "parent_window_unavailable", "native-panel": "parent_modal_window",
                   "wrong-child-pid": "invalid_child", "child-terminated": "invalid_child", "child-hidden": "child_hidden",
                   "empty-bundle": "bundle_mismatch", "bundle-mismatch": "bundle_mismatch",
                   "no-child-executable": "child_executable_changed", "child-executable-changed": "child_executable_changed"}
        for mode in (*reasons, "unregistered", "launching", "legacy-api", "capture-invalid"):
            with self.subTest(mode=mode):
                result = subprocess.run([str(self.binary), mode], capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                observations = [json.loads(line.removeprefix("ZWAI_RECOVERY_FOCUS ")) for line in result.stderr.splitlines()
                                if line.startswith("ZWAI_RECOVERY_FOCUS ")]
                if mode not in reasons:
                    self.assertEqual(observations, [], "pending registration wrote repeated diagnostics")
                    continue
                self.assertEqual(len(observations), 1, result.stderr)
                row = observations[0]
                self.assertEqual(row["reason"], reasons[mode])
                self.assertEqual(row["child_pid"], 900)
                self.assertEqual(row["activation_requested"], mode in ("normal", "activation-refused"))
                self.assertLess(len(result.stderr), 2048)
                self.assertNotIn("/fixture", result.stderr)
                if mode == "normal":
                    self.assertEqual(row["current_bundle"], "app.zwai.desktop")
                    self.assertEqual(row["child_bundle"], "app.zwai.desktop")
                    self.assertTrue(row["current_active"])
                    self.assertFalse(row["child_active"])
                    self.assertTrue(row["child_finished_launching"])
                    self.assertTrue(row["cooperative"])
                    self.assertTrue(row["activation_returned"])
                    self.assertTrue(row["own_window"])
                    self.assertFalse(row["hidden"])
                    self.assertFalse(row["minimized"])


if __name__ == "__main__":
    unittest.main()
