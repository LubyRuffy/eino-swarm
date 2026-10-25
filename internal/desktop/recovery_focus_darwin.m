#import <Cocoa/Cocoa.h>
#include <unistd.h>
#include "recovery_focus_darwin.h"

void zwaiRecoveryFocus(void *native) {
    if (native == NULL || ![NSThread isMainThread]) return;
    NSWindow *window = (__bridge NSWindow *)native;
    if (![window isKindOfClass:NSWindow.class] || window.isMiniaturized || [NSApp isHidden]
        || ![[NSApp windows] containsObject:window]) return;
    pid_t parentPID = getppid();
    if (parentPID <= 1) return;
    NSRunningApplication *current = NSRunningApplication.currentApplication;
    NSRunningApplication *parent = [NSRunningApplication runningApplicationWithProcessIdentifier:parentPID];
    if (current == nil || current.processIdentifier != getpid() || [current isTerminated]
        || parent == nil || parent.processIdentifier != parentPID || [parent isTerminated]
        || !current.bundleIdentifier.length || ![current.bundleIdentifier isEqual:parent.bundleIdentifier]) return;
    // Navigation and launch callbacks race with user focus changes. Refresh the
    // actual parent/frontmost identity on the main thread immediately before
    // requesting activation of this shell, never of an external application.
    NSRunningApplication *front = [[NSWorkspace sharedWorkspace] frontmostApplication];
    if (getppid() != parentPID || front.processIdentifier != parentPID) return;
    if (![current activateWithOptions:0]) return;
    [window makeKeyAndOrderFront:nil];
    [window makeKeyWindow];
}
