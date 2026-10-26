from pathlib import Path
import hashlib
import json
import platform
import subprocess
import tempfile
import unittest

import native_goal_resume_notice as notice


# Compile the complete current helper; only system observations/input are seams.
READING_OS_SEAMS = r"""
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <sys/sysctl.h>
static NSString *mode;
static AXUIElementRef app, win, area, web, group, receipt, second, branch, detached;
static int reveals=0, inputs=0, lookups=0, rootOwnerReads=0;
static BOOL hitRead=NO;
@interface ReadingApp:NSObject
@property(readonly) BOOL active;
@property(readonly) BOOL finishedLaunching;
@property(readonly) pid_t processIdentifier;
@property(readonly) NSURL *bundleURL;
+(id)runningApplicationWithProcessIdentifier:(pid_t)pid;
@end
@implementation ReadingApp
+(id)runningApplicationWithProcessIdentifier:(pid_t)pid {lookups++; return [self new];}
-(BOOL)active {return ![mode isEqual:@"inactive"];}
-(BOOL)finishedLaunching {return YES;}
-(pid_t)processIdentifier {return 123;}
-(NSURL *)bundleURL {return [NSURL fileURLWithPath:([mode isEqual:@"refreshed-bundle"] && lookups>1) ? @"/other.app" : @"/tmp/reading-private.app"];}
@end
static int qa_vm(const char *n,void *out,size_t *len,void *in,size_t size) {*(int *)out=1;return 0;}
static bool qa_trusted(void) {return true;}
static AXUIElementRef qa_application(pid_t pid) {return (AXUIElementRef)CFRetain(app);}
static AXError qa_owner(AXUIElementRef e,pid_t *pid) {
    if ([mode isEqual:@"unknown-child-pid"] && CFEqual(e,group)) return kAXErrorCannotComplete;
    if (CFEqual(e,win)) rootOwnerReads++;
    *pid=((([mode isEqual:@"wrong-window-owner"] || ([mode isEqual:@"changed-root-owner"] && rootOwnerReads>2)) && CFEqual(e,win)) ||
          ([mode isEqual:@"foreign-root-parent"] && CFEqual(e,detached))) ? 999 :
        ([mode isEqual:@"zero-child-pid"] && CFEqual(e,group)) ? 0 :
        ([mode isEqual:@"negative-child-pid"] && CFEqual(e,group)) ? -1 :
        ([mode isEqual:@"wrong-hit-owner"] && hitRead && CFEqual(e,receipt)) ? 999 :
        (([mode hasPrefix:@"remote-"] || [mode isEqual:@"wrong-hit-owner"]) &&
         (CFEqual(e,web)||CFEqual(e,group)||CFEqual(e,receipt)||CFEqual(e,branch))) ? 777 : 123;
    return kAXErrorSuccess;
}
static AXError qa_get(AXUIElementRef e,CFStringRef key,CFTypeRef *out) {
    id value=nil;
    if (CFEqual(key,kAXWindowsAttribute)) value=[mode isEqual:@"proxy-window"] ? @[@"proxy"] : @[(__bridge id)win];
    if (CFEqual(key,kAXRoleAttribute)) value=CFEqual(e,win) ? ([mode isEqual:@"wrong-window-role"] ? @"AXGroup" : @"AXWindow") :
        ([mode isEqual:@"foreign-root-parent"] && CFEqual(e,detached)) ? @"AXWindow" :
        CFEqual(e,area) ? @"AXScrollArea" : CFEqual(e,web) ? @"AXWebArea" :
        (CFEqual(e,receipt)||CFEqual(e,second)) ? @"AXStaticText" : @"AXGroup";
    if (CFEqual(key,kAXChildrenAttribute) && CFEqual(e,group) && [mode isEqual:@"children-null"]) {*out=NULL;return kAXErrorSuccess;}
    if (CFEqual(key,kAXChildrenAttribute) && CFEqual(e,group) && [mode isEqual:@"children-cfnull"]) {*out=CFRetain(kCFNull);return kAXErrorSuccess;}
    if (CFEqual(key,kAXValueAttribute) && [mode isEqual:@"unknown-text"] && CFEqual(e,receipt)) return kAXErrorCannotComplete;
    if (CFEqual(key,kAXValueAttribute) && (CFEqual(e,receipt)||CFEqual(e,second)))
        value=[mode isEqual:@"no-notice"] ? @"other receipt" : @"继续追求目标。";
    if (CFEqual(key,kAXParentAttribute) && CFEqual(e,receipt)) {
        if ([mode isEqual:@"unknown-parent"]) return kAXErrorCannotComplete;
        if ([mode isEqual:@"detached-parent"] || [mode isEqual:@"foreign-root-parent"] ||
            ([mode isEqual:@"detached-after-reveal"] && reveals>0) ||
            ([mode isEqual:@"detached-before-wheel"] && lookups>1)) {*out=CFRetain(detached);return kAXErrorSuccess;}
        if ([mode isEqual:@"proxy-parent"]) {*out=CFBridgingRetain(@"proxy");return kAXErrorSuccess;}
        if ([mode isEqual:@"remote-dag"] || [mode isEqual:@"remote-canonical-parent"]) {*out=CFRetain(branch);return kAXErrorSuccess;}
    }
    if (CFEqual(key,kAXParentAttribute) && CFEqual(e,group)) {
        if ([mode isEqual:@"cycle-parent"]) {*out=CFRetain(receipt);return kAXErrorSuccess;}
        if ([mode isEqual:@"no-root-parent"]) return kAXErrorNoValue;
    }
    if (CFEqual(key,kAXParentAttribute) && CFEqual(e,web) && [mode isEqual:@"remote-offscreen-detached-parent"]) {*out=CFRetain(detached);return kAXErrorSuccess;}
    if (CFEqual(key,kAXParentAttribute)) value=CFEqual(e,receipt)||CFEqual(e,second) ? (__bridge id)group :
        CFEqual(e,branch) ? (__bridge id)web :
        CFEqual(e,group) ? (__bridge id)web : CFEqual(e,web) ? (__bridge id)area :
        CFEqual(e,area) ? (__bridge id)win : nil;
    if (CFEqual(key,kAXChildrenAttribute)) value=CFEqual(e,win) ? @[(__bridge id)area] :
        CFEqual(e,area) ? @[(__bridge id)web] : CFEqual(e,web) ? ([mode isEqual:@"remote-dag"] ? @[(__bridge id)group,(__bridge id)branch] : @[(__bridge id)group]) :
        (CFEqual(e,branch) && [mode isEqual:@"remote-dag"]) ? @[(__bridge id)receipt] :
        CFEqual(e,group) ? (([mode isEqual:@"two-notices"]||[mode isEqual:@"shared-closing"]) ? @[(__bridge id)receipt,(__bridge id)second] : @[(__bridge id)receipt]) : @[];
    if (CFEqual(key,kAXPositionAttribute)||CFEqual(key,kAXSizeAttribute)) {
        if ([mode isEqual:@"unknown-ancestor"] && CFEqual(e,group)) return kAXErrorCannotComplete;
        if (([mode isEqual:@"remote-offscreen-unknown-outer"] && CFEqual(e,web)) ||
            ([mode isEqual:@"unknown-target"] && CFEqual(e,receipt))) return kAXErrorCannotComplete;
        CGPoint p=CGPointMake(0,30); CGSize s=CGSizeMake(800,600);
        if (CFEqual(e,group)) {p=CGPointMake(100,100);s=CGSizeMake(500,400);}
        if (CFEqual(e,receipt)||CFEqual(e,second)) {p=CGPointMake(120,([mode isEqual:@"outside-notice"] || ([mode isEqual:@"detached-after-reveal"] && !reveals))?700:200);s=CGSizeMake(100,20);}
        if ([mode hasPrefix:@"remote-offscreen"] && (!reveals || [mode isEqual:@"remote-offscreen-stays-outside"])) {
            if (CFEqual(e,group)) p.y=800;
            if (CFEqual(e,receipt)) p.y=900;
        }
        *out=CFEqual(key,kAXPositionAttribute) ? AXValueCreate(kAXValueCGPointType,&p) : AXValueCreate(kAXValueCGSizeType,&s);
        return kAXErrorSuccess;
    }
    if (!value) return kAXErrorNoValue;
    *out=CFBridgingRetain(value);return kAXErrorSuccess;
}
static AXError qa_action(AXUIElementRef e,CFStringRef a) {
    reveals++; return [mode isEqual:@"reveal-refused"] ? kAXErrorCannotComplete : kAXErrorSuccess;
}
static AXError qa_hit(AXUIElementRef e,float x,float y,AXUIElementRef *out) {
    hitRead=YES;*out=(AXUIElementRef)CFRetain([mode isEqual:@"different-hit"] ? group : receipt);return kAXErrorSuccess;
}
static void qa_post(pid_t pid,CGEventRef event) {
    if (pid!=123 || CGEventGetType(event)!=kCGEventScrollWheel || CGEventGetIntegerValueField(event,kCGScrollWheelEventDeltaAxis1)<=0) abort();
    inputs++;
}
#define NSRunningApplication ReadingApp
#define sysctlbyname qa_vm
#define AXIsProcessTrusted qa_trusted
#define AXUIElementCreateApplication qa_application
#define AXUIElementGetPid qa_owner
#define AXUIElementCopyAttributeValue qa_get
#define AXUIElementPerformAction qa_action
#define AXUIElementCopyElementAtPosition qa_hit
#define CGEventPostToPid qa_post
#define main reading_actual_main
"""


@unittest.skipUnless(platform.system() == "Darwin", "real Objective-C SDK required")
class ActualReadingHelper(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="goal-reading-actual-main-")
        cls.addClassCleanup(cls.temporary.cleanup)
        root = Path(cls.temporary.name)
        source = root / "reading.m"
        source.write_text(READING_OS_SEAMS + '#include "' + str(notice.SESSION_SOURCE) + '"\n' + r"""
#undef main
#undef AXUIElementCreateApplication
int main(int argc,const char **argv) {@autoreleasepool {
    if(argc!=3)return 7;mode=@(argv[1]);
    app=AXUIElementCreateApplication(1);win=AXUIElementCreateApplication(2);area=AXUIElementCreateApplication(3);
    web=AXUIElementCreateApplication(4);group=AXUIElementCreateApplication(5);
    receipt=AXUIElementCreateApplication(6);second=AXUIElementCreateApplication(7);
    branch=AXUIElementCreateApplication(8);detached=AXUIElementCreateApplication(9);
    setenv("ZWAI_NATIVE_APP","/tmp/reading-private.app",1);setenv("ZWAI_NATIVE_CASE_ID","reading-case",1);
    const char *descriptor=strcmp(argv[2],"inspect-text")==0 ? "{\"witness\":\"继续追求目标。\",\"unique\":true,\"path\":[0,0,0,0]}" :
        "{\"witness\":\"继续追求目标。\",\"unique\":true}";
    if ([mode isEqual:@"shared-closing"]) descriptor="{\"witness\":\"继续追求目标。\",\"unique\":false}";
    if ([mode isEqual:@"wrong-path"]) descriptor="{\"witness\":\"继续追求目标。\",\"unique\":true,\"path\":[9]}";
    if ([mode isEqual:@"malformed-path"]) descriptor="{\"witness\":\"继续追求目标。\",\"unique\":true,\"path\":[false,false,false,false]}";
    const char *args[]={"reading","123",argv[2],"/tmp/reading-private.app",descriptor};
    int code=reading_actual_main(strstr(argv[2],"-text")?5:4,args);
    fprintf(stderr,"{\"reveals\":%d,\"inputs\":%d}\n",reveals,inputs);return code;
}}
""")
        cls.binary = root / "reading"
        built = subprocess.run(["/usr/bin/xcrun", "clang", *notice.smoke.AX_FLAGS, str(source), "-o", str(cls.binary)],
                               capture_output=True, text=True)
        if built.returncode:
            raise RuntimeError(built.stderr)
        cls.real_binary = root / "unmodified-main"
        notice.prepare_session_helper(cls.real_binary)

    def test_real_sdk_main_refuses_before_any_target_or_ui_is_requested(self):
        result = subprocess.run([str(self.real_binary)], capture_output=True, text=True, timeout=3)
        self.assertEqual(result.returncode, 2)
        self.assertIn(json.loads(result.stdout)["error"],
                      ("macOS VM required; physical host refused", "expected private pid, action, bundle and optional text"))
        notice.validate_session_helper(self.real_binary)

    def observe(self, mode="good", action="read-up"):
        result = subprocess.run([str(self.binary), mode, action], capture_output=True, text=True, timeout=3)
        return result.returncode, json.loads(result.stdout), json.loads(result.stderr)

    def test_actual_main_posts_one_upward_wheel_only_at_the_unique_notice(self):
        code, row, actions = self.observe()
        self.assertEqual((code, actions), (0, {"reveals": 1, "inputs": 1}))
        notice.assert_reading_admission(row)
        self.assertEqual(row["viewport_frame"], {"x": 100, "y": 100, "width": 500, "height": 400})
        self.assertEqual(row["notice_frame"], {"x": 120, "y": 200, "width": 100, "height": 20})
        self.assertEqual(row["point"], {"x": 170, "y": 210})

    def test_real_window_notice_ancestry_hit_and_refresh_refusals_never_post_input(self):
        modes = ("proxy-window", "wrong-window-role", "wrong-window-owner", "wrong-hit-owner",
                 "no-notice", "two-notices", "unknown-text", "unknown-ancestor", "outside-notice", "different-hit",
                 "reveal-refused", "inactive", "refreshed-bundle")
        for mode in modes:
            with self.subTest(mode=mode):
                code, row, actions = self.observe(mode)
                self.assertNotEqual(code, 0)
                self.assertIn("error", row)
                self.assertEqual(actions["inputs"], 0)
                self.assertLessEqual(actions["reveals"], 1)

    def test_one_reveal_selects_one_recorded_path_even_for_explicit_common_closing_evidence(self):
        for mode, matched in (("good", 1), ("shared-closing", 2)):
            code, row, actions = self.observe(mode, "reveal-text")
            self.assertEqual((code, actions), (0, {"reveals": 1, "inputs": 0}))
            self.assertEqual((row["matched_count"], row["selected_path"], row["reveal_count"]), (matched, [0, 0, 0, 0], 1))
            notice.assert_target_geometry(row, unique=matched == 1)
        for mode in ("two-notices", "wrong-path", "malformed-path"):
            code, row, actions = self.observe(mode, "inspect-text" if mode != "two-notices" else "reveal-text")
            self.assertNotEqual(code, 0)
            self.assertEqual(actions, {"reveals": 0, "inputs": 0})
            self.assertIn("error", row)

    def test_scan_reports_original_owner_or_children_refusal_without_input(self):
        for mode, predicate in (("zero-child-pid", "node_owner"), ("negative-child-pid", "node_owner"),
                                ("unknown-child-pid", "node_owner"), ("children-null", "children_read"), ("children-cfnull", "children_read")):
            code, row, actions = self.observe(mode)
            self.assertEqual((code, actions), (2, {"reveals": 0, "inputs": 0}))
            first = row["first_refusal"]
            self.assertEqual(first["predicate"], predicate)
            self.assertEqual(first["status"], -25204 if mode == "unknown-child-pid" else 0)
            self.assertEqual(first["depth"], len(first["path"]))
            self.assertEqual((row["pressed"], row["wheel_posted"], row["reveal_count"]), (False, False, 0))
            if predicate == "node_owner":
                self.assertEqual((first["expected_pid"], first["observed_pid"]),
                                 (123, {"zero-child-pid": 0, "negative-child-pid": -1}.get(mode)))
            else:
                self.assertEqual((first["key"], first["role"]), ("AXChildren", "AXGroup"))
                self.assertEqual((first["raw_null"], first["cf_null"]), (mode == "children-null", mode == "children-cfnull"))

    def test_actual_inspection_reads_clipped_target_geometry_without_reveal_or_input(self):
        code, row, actions = self.observe(action="inspect-text")
        self.assertEqual((code, actions), (0, {"reveals": 0, "inputs": 0}))
        notice.assert_target_geometry(row, unique=True)
        self.assertEqual(row["targets"][0]["viewport"], {"x": 100, "y": 100, "width": 500, "height": 400})

    def test_actual_remote_children_and_canonical_dag_parent_reach_the_same_private_root(self):
        for mode in ("remote-bridge", "remote-dag", "remote-canonical-parent"):
            for action in ("read-up", "reveal-text", "inspect-text", "reveal-notice"):
                with self.subTest(mode=mode, action=action):
                    code, row, actions = self.observe(mode, action)
                    self.assertEqual(code, 0, row)
                    self.assertEqual(actions["inputs"], int(action == "read-up"))
                    self.assertEqual(actions["reveals"], int(action in ("read-up", "reveal-text")))
                    self.assertEqual(row["root_window"], {"role": "AXWindow", "owner_pid": 123})
                    if action.endswith("-text"):
                        notice.assert_target_geometry(row, unique=True)
                        target = row["targets"][0]
                        self.assertEqual(target["owner_pid"], 777)
                        self.assertEqual(target["binding"], {"children_reachable": True, "parent_chain_root": True,
                                                             "root_owner_pid": 123, "root_role": "AXWindow"})
                        self.assertEqual([ancestor["owner_pid"] for ancestor in target["ancestors"]], [777, 777, 123])
                    elif action == "read-up":
                        notice.assert_reading_admission(row)
                        self.assertEqual((row["notice_owner_pid"], row["hit_owner_pid"]), (777, 777))

    def test_unbound_parent_paths_refuse_before_any_reveal_or_input(self):
        for mode in ("detached-parent", "foreign-root-parent", "cycle-parent", "no-root-parent", "proxy-parent", "unknown-parent", "changed-root-owner"):
            for action in ("read-up", "reveal-text", "inspect-text", "reveal-notice"):
                with self.subTest(mode=mode, action=action):
                    code, row, actions = self.observe(mode, action)
                    self.assertEqual((code, actions), (2, {"reveals": 0, "inputs": 0}))
                    self.assertIn("error", row)

    def test_wrong_hit_identity_or_changed_owner_never_receives_a_wheel(self):
        for mode in ("different-hit", "wrong-hit-owner"):
            code, row, actions = self.observe(mode)
            self.assertEqual((code, actions), (2, {"reveals": 1, "inputs": 0}))
            self.assertFalse(row["hit_verified"])

    def test_reveal_and_native_refresh_cannot_keep_a_stale_root_binding_for_input(self):
        for action in ("read-up", "reveal-text", "reveal-notice"):
            code, row, actions = self.observe("detached-after-reveal", action)
            self.assertEqual((code, actions), (2, {"reveals": 1, "inputs": 0}))
            self.assertIn("error", row)
        code, row, actions = self.observe("detached-before-wheel")
        self.assertEqual((code, actions), (2, {"reveals": 1, "inputs": 0}))
        self.assertIn("root binding changed", row["error"])

    def test_known_offscreen_ancestors_allow_one_reveal_before_strict_visible_readback(self):
        for action in ("read-up", "reveal-text", "reveal-notice"):
            with self.subTest(action=action):
                code, row, actions = self.observe("remote-offscreen", action)
                self.assertEqual((code, actions), (0, {"reveals": 1, "inputs": int(action == "read-up")}))
                if action == "read-up":
                    notice.assert_reading_admission(row)
                elif action == "reveal-text":
                    notice.assert_target_geometry(row, unique=True)
                else:
                    notice.assert_notice_in_view(row)
        code, row, actions = self.observe("remote-offscreen", "inspect-text")
        self.assertEqual((code, actions), (2, {"reveals": 0, "inputs": 0}))
        self.assertIn("error", row)

    def test_offscreen_is_not_a_substitute_for_known_geometry_or_exact_root_binding(self):
        for mode in ("remote-offscreen-unknown-outer", "remote-offscreen-detached-parent", "unknown-target"):
            for action in ("read-up", "reveal-text", "reveal-notice"):
                with self.subTest(mode=mode, action=action):
                    code, row, actions = self.observe(mode, action)
                    self.assertEqual((code, actions), (2, {"reveals": 0, "inputs": 0}))
                    self.assertIn("error", row)
        for action in ("read-up", "reveal-text", "reveal-notice"):
            with self.subTest(action=action):
                code, row, actions = self.observe("remote-offscreen-stays-outside", action)
                self.assertEqual((code, actions), (2, {"reveals": 1, "inputs": 0}))
                self.assertIn("error", row)


if __name__ == "__main__":
    unittest.main()
