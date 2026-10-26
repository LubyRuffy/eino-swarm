#import <Cocoa/Cocoa.h>
#include <libproc.h>
#include <sys/proc_info.h>
#include <limits.h>
#include <stdlib.h>
#include <string.h>
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

int zwaiRecoveryCapture(int pid, const char *executable, zwaiRecoveryBirth *birth) {
    struct proc_bsdinfo info;
    char actual[PROC_PIDPATHINFO_MAXSIZE], resolvedActual[PATH_MAX], resolvedExpected[PATH_MAX];
    if (pid <= 1 || executable == NULL || birth == NULL
        || proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info)) != sizeof(info)
        || info.pbi_pid != pid || info.pbi_ppid != getpid()
        || proc_pidpath(pid, actual, sizeof(actual)) <= 0
        || realpath(actual, resolvedActual) == NULL || realpath(executable, resolvedExpected) == NULL
        || strcmp(resolvedActual, resolvedExpected) != 0) return 0;
    birth->seconds = info.pbi_start_tvsec;
    birth->microseconds = info.pbi_start_tvusec;
    return 1;
}

static BOOL sameRecoveryBirth(zwaiRecoveryBirth first, zwaiRecoveryBirth second) {
    return first.seconds == second.seconds && first.microseconds == second.microseconds;
}

static BOOL activateRecoveryChild(NSRunningApplication *child, BOOL cooperative) {
    if (cooperative) {
        if (@available(macOS 14.0, *)) {
            [NSApp yieldActivationToApplication:child];
        }
    }
    return [child activateWithOptions:0];
}

int zwaiRecoveryFocusSuccessor(int pid, const char *executable, zwaiRecoveryBirth birth) {
    NSMutableDictionary *facts = [@{@"pid": @(getpid()), @"child_pid": @(pid), @"main": @([NSThread isMainThread]),
        @"activation_requested": @NO, @"activation_returned": NSNull.null, @"cooperative": NSNull.null,
        @"current_pid": NSNull.null, @"frontmost_pid": NSNull.null,
        @"current_bundle": NSNull.null, @"child_bundle": NSNull.null,
        @"current_active": NSNull.null, @"child_active": NSNull.null, @"child_finished_launching": NSNull.null,
        @"current_policy": NSNull.null, @"child_policy": NSNull.null,
        @"hidden": NSNull.null, @"minimized": NSNull.null, @"own_window": NSNull.null} mutableCopy];
#define REFUSE(reason, status) do { recordRecoveryFocus(facts, @reason); return status; } while (0)
    if (![facts[@"main"] boolValue]) REFUSE("not_main", 1);
    zwaiRecoveryBirth actual;
    if (!zwaiRecoveryCapture(pid, executable, &actual) || !sameRecoveryBirth(actual, birth)) REFUSE("child_identity_changed", 3);
    NSRunningApplication *current = NSRunningApplication.currentApplication;
    if (current == nil || current.processIdentifier != getpid() || current.isTerminated) REFUSE("invalid_current", 1);
    facts[@"current_pid"] = @(current.processIdentifier);
    facts[@"current_active"] = @(current.isActive);
    facts[@"current_policy"] = @(current.activationPolicy);
    NSString *currentBundle = current.bundleIdentifier;
    facts[@"current_bundle"] = currentBundle == nil ? (id)NSNull.null : [currentBundle substringToIndex:MIN(currentBundle.length, 128)];
    NSRunningApplication *front = NSWorkspace.sharedWorkspace.frontmostApplication;
    facts[@"frontmost_pid"] = @(front.processIdentifier);
    if (front.processIdentifier == pid) REFUSE("already_frontmost", 1);
    if (front.processIdentifier != getpid() || !current.isActive) REFUSE("parent_not_frontmost", 1);
    facts[@"hidden"] = @([NSApp isHidden]);
    if ([facts[@"hidden"] boolValue]) REFUSE("parent_hidden", 1);
    NSWindow *window = [NSApp mainWindow];
    if (![window isKindOfClass:NSWindow.class]) REFUSE("no_parent_window", 1);
    facts[@"own_window"] = @([[NSApp windows] containsObject:window]);
    if (![facts[@"own_window"] boolValue]) REFUSE("foreign_parent_window", 1);
    facts[@"minimized"] = @(window.isMiniaturized);
    if (window.isMiniaturized || !window.isVisible) REFUSE("parent_window_unavailable", 1);
    if ([NSApp keyWindow] != window) REFUSE("parent_modal_window", 1);
    NSRunningApplication *child = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
    if (child == nil) return 0;
    if (child.processIdentifier != pid || child.isTerminated) REFUSE("invalid_child", 3);
    facts[@"child_active"] = @(child.isActive);
    facts[@"child_finished_launching"] = @(child.isFinishedLaunching);
    facts[@"child_policy"] = @(child.activationPolicy);
    if (!child.isFinishedLaunching) return 0;
    if (child.isHidden) REFUSE("child_hidden", 1);
    NSString *childBundle = child.bundleIdentifier;
    facts[@"child_bundle"] = childBundle == nil ? (id)NSNull.null : [childBundle substringToIndex:MIN(childBundle.length, 128)];
    if (!currentBundle.length || ![currentBundle isEqual:childBundle]) REFUSE("bundle_mismatch", 3);
    char resolvedChild[PATH_MAX], resolvedExpected[PATH_MAX];
    if (child.executableURL == nil || realpath(child.executableURL.fileSystemRepresentation, resolvedChild) == NULL
        || realpath(executable, resolvedExpected) == NULL || strcmp(resolvedChild, resolvedExpected) != 0) REFUSE("child_executable_changed", 3);
    // The last check uses the original birth/path and the current foreground,
    // after registration observations, immediately before the single request.
    if (!zwaiRecoveryCapture(pid, executable, &actual) || !sameRecoveryBirth(actual, birth)) REFUSE("child_identity_changed", 3);
    front = NSWorkspace.sharedWorkspace.frontmostApplication;
    facts[@"frontmost_pid"] = @(front.processIdentifier);
    if (front.processIdentifier != getpid()) REFUSE("parent_not_frontmost", 1);
    BOOL cooperative = NO;
    if (@available(macOS 14.0, *)) cooperative = YES;
    facts[@"cooperative"] = @(cooperative);
    facts[@"activation_requested"] = @YES;
    BOOL activated = activateRecoveryChild(child, cooperative);
    facts[@"activation_returned"] = @(activated);
    recordRecoveryFocus(facts, activated ? @"activation_requested" : @"activation_refused");
    return 2;
#undef REFUSE
}
