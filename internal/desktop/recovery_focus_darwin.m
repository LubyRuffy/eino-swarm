#import <Cocoa/Cocoa.h>
#include <unistd.h>
#include "recovery_focus_darwin.h"

static void recordRecoveryFocus(NSMutableDictionary *facts, NSString *reason) {
    facts[@"reason"] = reason;
    NSData *data = [NSJSONSerialization dataWithJSONObject:facts options:0 error:NULL];
    if (data != nil) {
        static const char prefix[] = "ZWAI_RECOVERY_FOCUS ";
        NSMutableData *line = [NSMutableData dataWithBytes:prefix length:sizeof(prefix) - 1];
        [line appendData:data];
        [line appendBytes:"\n" length:1];
        (void)write(STDERR_FILENO, line.bytes, line.length);
    }
}

void zwaiRecoveryFocus(void *native) {
    NSMutableDictionary *facts = [@{@"pid": @(getpid()), @"main": @([NSThread isMainThread]),
        @"activation_requested": @NO, @"activation_returned": NSNull.null,
        @"ppid": NSNull.null, @"ppid_recheck": NSNull.null, @"current_pid": NSNull.null,
        @"parent_pid": NSNull.null, @"frontmost_pid": NSNull.null,
        @"current_bundle": NSNull.null, @"parent_bundle": NSNull.null,
        @"hidden": NSNull.null, @"minimized": NSNull.null, @"own_window": NSNull.null} mutableCopy];
#define REFUSE(reason) do { recordRecoveryFocus(facts, @reason); return; } while (0)
    if (native == NULL) REFUSE("no_window");
    if (![facts[@"main"] boolValue]) REFUSE("not_main");
    NSWindow *window = (__bridge NSWindow *)native;
    if (![window isKindOfClass:NSWindow.class]) REFUSE("wrong_window_type");
    facts[@"minimized"] = @(window.isMiniaturized);
    if ([facts[@"minimized"] boolValue]) REFUSE("minimized");
    facts[@"hidden"] = @([NSApp isHidden]);
    if ([facts[@"hidden"] boolValue]) REFUSE("hidden");
    facts[@"own_window"] = @([[NSApp windows] containsObject:window]);
    if (![facts[@"own_window"] boolValue]) REFUSE("foreign_window");
    pid_t parentPID = getppid();
    facts[@"ppid"] = @(parentPID);
    if (parentPID <= 1) REFUSE("no_parent_pid");
    NSRunningApplication *current = NSRunningApplication.currentApplication;
    NSRunningApplication *parent = [NSRunningApplication runningApplicationWithProcessIdentifier:parentPID];
    if (current == nil) REFUSE("no_current");
    facts[@"current_pid"] = @(current.processIdentifier);
    if ([facts[@"current_pid"] intValue] != getpid()) REFUSE("wrong_current_pid");
    if ([current isTerminated]) REFUSE("current_terminated");
    if (parent == nil) REFUSE("no_parent");
    facts[@"parent_pid"] = @(parent.processIdentifier);
    if ([facts[@"parent_pid"] intValue] != parentPID) REFUSE("wrong_parent_pid");
    if ([parent isTerminated]) REFUSE("parent_terminated");
    NSString *currentBundle = current.bundleIdentifier;
    facts[@"current_bundle"] = currentBundle == nil ? (id)NSNull.null : [currentBundle substringToIndex:MIN(currentBundle.length, 128)];
    if (!currentBundle.length) REFUSE("empty_current_bundle");
    NSString *parentBundle = parent.bundleIdentifier;
    facts[@"parent_bundle"] = parentBundle == nil ? (id)NSNull.null : [parentBundle substringToIndex:MIN(parentBundle.length, 128)];
    if (![currentBundle isEqual:parentBundle]) REFUSE("bundle_mismatch");
    // Navigation and launch callbacks race with user focus changes. Refresh the
    // actual parent/frontmost identity on the main thread immediately before
    // requesting activation of this shell, never of an external application.
    NSRunningApplication *front = [[NSWorkspace sharedWorkspace] frontmostApplication];
    facts[@"ppid_recheck"] = @(getppid());
    if ([facts[@"ppid_recheck"] intValue] != parentPID) REFUSE("parent_changed");
    facts[@"frontmost_pid"] = @(front.processIdentifier);
    if ([facts[@"frontmost_pid"] intValue] != parentPID) REFUSE("frontmost_changed");
    facts[@"activation_requested"] = @YES;
    BOOL activated = [current activateWithOptions:0];
    facts[@"activation_returned"] = @(activated);
    if (!activated) REFUSE("activation_refused");
    [window makeKeyAndOrderFront:nil];
    [window makeKeyWindow];
    recordRecoveryFocus(facts, @"activation_requested");
#undef REFUSE
}
