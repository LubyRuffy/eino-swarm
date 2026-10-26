// Direct AX inspection and PID-targeted input for one isolated macOS VM case.
// Build during candidate preparation; never run a compiler inside a timed case.
#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <CoreGraphics/CoreGraphics.h>
#include <sys/sysctl.h>
#include <limits.h>
#include <unistd.h>

static id attribute(AXUIElementRef element, CFStringRef key) {
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(element, key, &value) != kAXErrorSuccess) return nil;
    return CFBridgingRelease(value);
}
static NSString *text(AXUIElementRef element, CFStringRef key) {
    id value = attribute(element, key);
    return [value isKindOfClass:NSString.class] ? value : @"";
}
static BOOL realWindow(id candidate, pid_t expectedPID) {
    if (!candidate || CFGetTypeID((__bridge CFTypeRef)candidate) != AXUIElementGetTypeID()) return NO;
    AXUIElementRef window = (__bridge AXUIElementRef)candidate;
    CFTypeRef role = NULL; pid_t owner = 0;
    AXError roleError = AXUIElementCopyAttributeValue(window, kAXRoleAttribute, &role);
    AXError pidError = AXUIElementGetPid(window, &owner);
    BOOL valid = roleError == kAXErrorSuccess && role && CFGetTypeID(role) == CFStringGetTypeID()
        && CFEqual(role, kAXWindowRole) && pidError == kAXErrorSuccess && owner == expectedPID;
    if (role) CFRelease(role);
    return valid;
}
static NSArray *children(AXUIElementRef element) {
    id value = attribute(element, kAXChildrenAttribute);
    return [value isKindOfClass:NSArray.class] ? value : @[];
}
static CGRect frame(AXUIElementRef element) {
    id position = attribute(element, kAXPositionAttribute), size = attribute(element, kAXSizeAttribute);
    CGPoint origin = CGPointZero; CGSize extent = CGSizeZero;
    if (position && CFGetTypeID((__bridge CFTypeRef)position) == AXValueGetTypeID())
        AXValueGetValue((__bridge AXValueRef)position, kAXValueCGPointType, &origin);
    if (size && CFGetTypeID((__bridge CFTypeRef)size) == AXValueGetTypeID())
        AXValueGetValue((__bridge AXValueRef)size, kAXValueCGSizeType, &extent);
    return (CGRect){origin, extent};
}
static NSString *normalized(NSString *value) {
    NSArray *pieces = [value componentsSeparatedByCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet];
    return [[pieces filteredArrayUsingPredicate:[NSPredicate predicateWithFormat:@"length > 0"]] componentsJoinedByString:@" "];
}
static void emit(id value) {
    NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:nil];
    fwrite(data.bytes, 1, data.length, stdout); fputc('\n', stdout);
}
static int fail(NSString *message) { emit(@{@"error": message}); return 2; }
static int failSnapshot(AXUIElementRef app, NSMutableDictionary *result, NSString *message) {
    result[@"error"] = message; emit(result); CFRelease(app); return 2;
}

static BOOL ownsCase(pid_t pid, NSString *caseID) {
    // Only the one inherited marker is compared; no other environment is exposed.
    int mib[] = {CTL_KERN, KERN_PROCARGS2, pid}; size_t size = 0;
    if (sysctl(mib, 3, NULL, &size, NULL, 0) || size < sizeof(int)) return NO;
    NSMutableData *data = [NSMutableData dataWithLength:size];
    if (sysctl(mib, 3, data.mutableBytes, &size, NULL, 0)) return NO;
    const char *cursor = (const char *)data.bytes + sizeof(int), *end = (const char *)data.bytes + size;
    int argc = 0; memcpy(&argc, data.bytes, sizeof(argc));
    if (argc <= 0) return NO;
    size_t length = strnlen(cursor, end-cursor);
    if (cursor+length == end) return NO;
    cursor += length+1;
    while (cursor < end && !*cursor) cursor++;
    for (int index = 0; index < argc && cursor < end; index++) {
        length = strnlen(cursor, end-cursor);
        if (cursor+length == end) return NO;
        cursor += length+1;
    }
    NSString *expected = [@"ZWAI_NATIVE_CASE_ID=" stringByAppendingString:caseID];
    while (cursor < end) {
        if (!*cursor) { cursor++; continue; }
        length = strnlen(cursor, end-cursor);
        if (cursor+length == end) return NO;
        if ([expected isEqualToString:[[NSString alloc] initWithBytes:cursor length:length encoding:NSUTF8StringEncoding]]) return YES;
        cursor += length+1;
    }
    return NO;
}
static BOOL validOrigin(NSString *origin) {
    NSURLComponents *url = [NSURLComponents componentsWithString:origin];
    NSString *canonical = [NSString stringWithFormat:@"http://127.0.0.1:%@", url.port];
    return [origin isEqual:canonical] && [url.scheme isEqual:@"http"] && [url.host isEqual:@"127.0.0.1"] &&
        url.port.integerValue > 0 && url.port.integerValue <= 65535 && !url.user && !url.password &&
        !url.path.length && !url.query && !url.fragment;
}
static BOOL pageMatches(NSString *value, NSString *origin, pid_t pid) {
    if (!validOrigin(origin)) return NO;
    NSURLComponents *url = [NSURLComponents componentsWithString:value];
    NSString *base = [NSString stringWithFormat:@"%@://%@:%@", url.scheme, url.host, url.port];
    NSMutableArray *shell = [NSMutableArray array], *pids = [NSMutableArray array];
    for (NSURLQueryItem *item in url.queryItems) {
        if ([item.name isEqualToString:@"shell"]) [shell addObject:item.value ?: @""];
        if ([item.name isEqualToString:@"pid"]) [pids addObject:item.value ?: @""];
    }
    return [base isEqualToString:origin] && !url.user && !url.password && !url.fragment &&
        (!url.path.length || [url.path isEqual:@"/"]) &&
        [shell isEqual:@[@"desktop"]] && [pids isEqual:@[[NSString stringWithFormat:@"%d", pid]]];
}
static void collectUnique(AXUIElementRef element, NSMutableArray *tree, NSMutableArray *areas,
                    NSMutableArray *sends, NSMutableArray *urls, NSString *expected,
                    CGRect window, BOOL *visible, BOOL *truncated, int depth,
                    CFMutableSetRef seen, NSUInteger *visits) {
    if (*truncated) return;
    if (++*visits > 5000) { *truncated = YES; return; }
    // AX children form a graph. Equivalent references must not become duplicate
    // editable controls, while empty nodes and repeated edges still cost work.
    if (CFSetContainsValue(seen, element)) return;
    if (depth > 45) { *truncated = YES; return; }
    CFSetAddValue(seen, element);
    NSMutableDictionary *row = [NSMutableDictionary dictionary];
    for (NSString *key in @[@"AXRole", @"AXTitle", @"AXDescription", @"AXValue", @"AXURL"]) {
        id value = attribute(element, (__bridge CFStringRef)key);
        if ([value isKindOfClass:NSURL.class]) value = [value absoluteString];
        if ([value isKindOfClass:NSString.class] && [value length]) row[key] = [value substringToIndex:MIN([value length], 8192)];
    }
    for (NSString *key in @[@"AXEnabled", @"AXFocused"]) {
        id value = attribute(element, (__bridge CFStringRef)key);
        if ([value isKindOfClass:NSNumber.class]) row[key] = value;
    }
    if ([row[@"AXRole"] isEqual:@"AXTextArea"]) [areas addObject:(__bridge id)element];
    if ([row[@"AXRole"] isEqual:@"AXWebArea"] && row[@"AXURL"]) [urls addObject:row[@"AXURL"]];
    if ([row[@"AXRole"] isEqual:@"AXButton"] &&
        ([@[@"Send", @"发送"] containsObject:row[@"AXTitle"]] ||
         [@[@"Send", @"发送"] containsObject:row[@"AXDescription"]])) [sends addObject:(__bridge id)element];
    if (expected.length && [@[@"AXStaticText", @"AXHeading"] containsObject:row[@"AXRole"]]) {
        NSString *content = row[@"AXValue"] ?: row[@"AXTitle"] ?: @"";
        if ([normalized(content) containsString:expected]) {
            AXUIElementPerformAction(element, CFSTR("AXScrollToVisible"));
            CGRect intersection = CGRectIntersection(frame(element), window);
            if (!CGRectIsNull(intersection) && intersection.size.width > 0 && intersection.size.height > 0) {
                *visible = YES;
                row[@"result_intersects_window"] = @YES;
            }
        }
    }
    if (row.count) [tree addObject:row];
    for (id child in children(element)) {
        collectUnique((__bridge AXUIElementRef)child, tree, areas, sends, urls,
                      expected, window, visible, truncated, depth+1, seen, visits);
        if (*truncated) break;
    }
}
static void collect(AXUIElementRef element, NSMutableArray *tree, NSMutableArray *areas,
                    NSMutableArray *sends, NSMutableArray *urls, NSString *expected,
                    CGRect window, BOOL *visible, BOOL *truncated, int depth) {
    CFMutableSetRef seen = CFSetCreateMutable(kCFAllocatorDefault, 0, &kCFTypeSetCallBacks);
    NSUInteger visits = 0;
    collectUnique(element, tree, areas, sends, urls, expected, window, visible, truncated, depth, seen, &visits);
    CFRelease(seen);
}
static CGEventRef keyboard(CGEventSourceRef source, bool down, UniChar character) {
    CGEventRef event = CGEventCreateKeyboardEvent(source, 0, down);
    CGEventSetFlags(event, 0); CGEventKeyboardSetUnicodeString(event, 1, &character);
    return event;
}

static BOOL waitForActivation(NSRunningApplication *running, double deadline) {
    for (;;) {
        double remaining = deadline - NSProcessInfo.processInfo.systemUptime;
        if (remaining <= 0) return NO;
        // NSRunningApplication refreshes these properties on the main runloop.
        [[NSRunLoop mainRunLoop] runMode:NSDefaultRunLoopMode
                            beforeDate:[NSDate dateWithTimeIntervalSinceNow:MIN(.01, remaining)]];
        if (NSProcessInfo.processInfo.systemUptime >= deadline) return NO;
        if (running.active) return YES;
    }
}

static id sessionBoolean(NSDictionary *session, NSString *key) {
    id value = session[key];
    return value && CFGetTypeID((__bridge CFTypeRef)value) == CFBooleanGetTypeID()
        ? value : NSNull.null;
}

static int preflight(NSString *caseID) {
    if (!caseID.length) return fail(@"preflight requires the current case identity");
    NSDictionary *session = CFBridgingRelease(CGSessionCopyCurrentDictionary()) ?: @{};
    // These reads never request permission, activate a window, or synthesize input.
    emit(@{@"schema_version": @1, @"case_id": caseID, @"uid": @(getuid()),
        @"session_uid": session[@"kCGSSessionUserIDKey"] ?: NSNull.null,
        @"on_console": sessionBoolean(session, @"kCGSSessionOnConsoleKey"),
        @"login_done": sessionBoolean(session, @"kCGSessionLoginDoneKey"),
        @"lock_key_present": [NSNumber numberWithBool:session[@"CGSSessionScreenIsLocked"] != nil],
        @"locked": sessionBoolean(session, @"CGSSessionScreenIsLocked"),
        @"ax_trusted": [NSNumber numberWithBool:AXIsProcessTrusted()],
        @"screen_capture_allowed": [NSNumber numberWithBool:CGPreflightScreenCaptureAccess()]});
    return 0;
}

int main(int argc, const char **argv) { @autoreleasepool {
    int virtualized = 0; size_t size = sizeof(virtualized);
    if (sysctlbyname("kern.hv_vmm_present", &virtualized, &size, NULL, 0) || virtualized != 1)
        return fail(@"macOS VM required; physical host refused");
    if (argc == 3 && !strcmp(argv[1], "--preflight")) return preflight(@(argv[2]));
    if (argc != 7) return fail(@"expected pid action case-id bundle origin value");
    char *tail = NULL; long parsed = strtol(argv[1], &tail, 10);
    if (!tail || *tail || parsed <= 0 || parsed > INT_MAX) return fail(@"invalid private app PID");
    pid_t pid = (pid_t)parsed; NSString *action = @(argv[2]), *caseID = @(argv[3]);
    NSString *bundle = [@(argv[4]) stringByResolvingSymlinksInPath], *origin = @(argv[5]), *value = @(argv[6]);
    if (!validOrigin(origin)) return fail(@"native origin must be numeric loopback HTTP without a path");
    if (!caseID.length || !ownsCase(pid, caseID)) return fail(@"native target case identity mismatch");
    NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
    if (running && running.processIdentifier != pid)
        return fail(@"native target process identity mismatch");
    NSString *registeredBundle = running.bundleURL.path;
    if (!running || !registeredBundle.length) {
        if (![action isEqual:@"snapshot"])
            return fail(@"native target bundle identity mismatch");
        // A process and its engine can precede native application registration.
        // Only the caller's existing snapshot deadline may wait for this state.
        emit(@{@"pid": @(pid), @"case_id": caseID, @"origin": origin,
            @"bundle": bundle, @"requested_bundle": bundle,
            @"case_verified": @NO, @"bundle_verified": @NO, @"ready": @NO,
            @"registration_pending": @YES,
            @"registration_state": running ? @"bundle_unavailable" : @"not_registered",
            @"window_count": @0, @"windows": @[], @"tree": @[]});
        return 0;
    }
    if (![[registeredBundle stringByResolvingSymlinksInPath] isEqual:bundle])
        return fail(@"native target bundle identity mismatch");
    if (!AXIsProcessTrusted()) return fail(@"guest Accessibility permission must already be granted");
    if (!CGPreflightScreenCaptureAccess()) return fail(@"guest Screen Recording permission must already be granted");
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    AXUIElementSetMessagingTimeout(app, 2);
    AXUIElementSetAttributeValue(app, CFSTR("AXEnhancedUserInterface"), kCFBooleanTrue);
    AXUIElementSetAttributeValue(app, CFSTR("AXManualAccessibility"), kCFBooleanTrue);
    NSArray *windows = attribute(app, kAXWindowsAttribute) ?: @[];
    if (![windows isKindOfClass:NSArray.class] || windows.count > 1)
        { CFRelease(app); return fail(@"native target has malformed or multiple AX windows"); }
    __attribute__((objc_precise_lifetime)) id windowOwner = windows.count ? windows[0] : nil;
    if (windowOwner && !realWindow(windowOwner, pid))
        { CFRelease(app); return fail(@"native AX window role or PID refused"); }
    AXUIElementRef window = (__bridge AXUIElementRef)windowOwner;
    if (window && [attribute(window, CFSTR("AXSheets")) count]) { CFRelease(app); return fail(@"native target has a modal sheet"); }
    double activationStarted = NSProcessInfo.processInfo.systemUptime;
    double activationDeadline = activationStarted + 2;
    BOOL activationRequested = [running activateWithOptions:0];
    if (window) AXUIElementPerformAction(window, kAXRaiseAction);
    BOOL activationReady = waitForActivation(running, activationDeadline);
    double activationElapsed = NSProcessInfo.processInfo.systemUptime - activationStarted;
    CGRect bounds = window ? frame(window) : CGRectZero;
    NSMutableArray *tree = [NSMutableArray array], *areas = [NSMutableArray array], *sends = [NSMutableArray array];
    NSMutableArray *urls = [NSMutableArray array], *cg = [NSMutableArray array], *values = [NSMutableArray array];
    BOOL visible = NO, truncated = NO;
    if (window) collect(window, tree, areas, sends, urls, [action isEqual:@"snapshot"] ? value : @"",
                        bounds, &visible, &truncated, 0);
    if (truncated) { CFRelease(app); return fail(@"native AX tree exceeded its bounded traversal"); }
    for (NSString *url in urls) if (!pageMatches(url, origin, pid)) { CFRelease(app); return fail(@"native webview origin, shell or PID mismatch"); }
    NSArray *all = CFBridgingRelease(CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID));
    for (NSDictionary *row in all) {
        if ([row[(id)kCGWindowOwnerPID] intValue] == pid && [row[(id)kCGWindowLayer] intValue] == 0)
            [cg addObject:@{@"id": row[(id)kCGWindowNumber], @"owner_pid": @(pid)}];
    }
    for (id area in areas) [values addObject:text((__bridge AXUIElementRef)area, kAXValueAttribute)];
    BOOL sendEnabled = sends.count == 1 && [attribute((__bridge AXUIElementRef)sends[0], kAXEnabledAttribute) boolValue];
    BOOL frontmost = running.active;
    BOOL hasWebArea = urls.count > 0;
    BOOL ready = activationReady && frontmost && bounds.size.width > 0 && bounds.size.height > 0 &&
        windows.count == 1 && cg.count == 1 && hasWebArea && areas.count == 1;
    NSMutableDictionary *result = [@{@"pid": @(pid), @"bundle": bundle, @"origin": origin,
        @"case_verified": @YES, @"ready": @(ready), @"activation_ready": @(activationReady),
        @"activation_requested": @(activationRequested),
        @"activation_elapsed_seconds": @(activationElapsed),
        @"press_attempted": @NO, @"title": window ? text(window, kAXTitleAttribute) : @"",
        @"frontmost": @(frontmost), @"has_web_area": @(hasWebArea), @"web_urls": urls,
        @"textareas": @(areas.count), @"area_values": values, @"send_count": @(sends.count),
        @"send_enabled": @(sendEnabled), @"rect": @[@(bounds.origin.x), @(bounds.origin.y), @(bounds.size.width), @(bounds.size.height)],
        @"window_count": @(windows.count), @"windows": cg, @"screen_capture_allowed": @YES,
        @"expected_visible": @(visible), @"tree": tree} mutableCopy];
    if (cg.count == 1) { result[@"window_id"] = cg[0][@"id"]; result[@"window_owner_pid"] = @(pid); }
    if (!activationReady) return failSnapshot(app, result, @"native activation did not become active before its deadline");
    if ([action isEqual:@"fill"]) {
        if (!ready || ![values isEqual:@[@""]])
            return failSnapshot(app, result, @"fill requires one empty native composer");
        AXError error = AXUIElementSetAttributeValue((__bridge AXUIElementRef)areas[0], kAXFocusedAttribute, kCFBooleanTrue);
        if (error != kAXErrorSuccess) { CFRelease(app); return fail(@"native composer focus failed"); }
        CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStatePrivate);
        if (!source) { CFRelease(app); return fail(@"private keyboard source is unavailable"); }
        for (NSUInteger index = 0; index < value.length; index++) {
            CGEventRef down = keyboard(source, true, [value characterAtIndex:index]);
            CGEventRef up = keyboard(source, false, [value characterAtIndex:index]);
            CGEventPostToPid(pid, down); CGEventPostToPid(pid, up);
            CFRelease(down); CFRelease(up); usleep(15000);
        }
        CFRelease(source); result[@"filled"] = @YES;
    } else if ([action isEqual:@"send"]) {
        // Recheck at the mutation boundary; an earlier inspect is only a snapshot.
        if (!ready || ![values isEqual:@[value]] || !sendEnabled)
            return failSnapshot(app, result, @"Send requires the exact draft and one enabled button");
        result[@"press_attempted"] = @YES;
        AXError error = AXUIElementPerformAction((__bridge AXUIElementRef)sends[0], kAXPressAction);
        if (error != kAXErrorSuccess) return failSnapshot(app, result, @"native Send action failed");
        result[@"submitted"] = @YES;
    } else if (![action isEqual:@"snapshot"]) { CFRelease(app); return fail(@"unsupported native action"); }
    emit(result); CFRelease(app); return 0;
} }
