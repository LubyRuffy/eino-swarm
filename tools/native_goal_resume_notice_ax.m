#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <CoreGraphics/CoreGraphics.h>
#include <math.h>
#include <sys/sysctl.h>

static id attribute(AXUIElementRef element, CFStringRef key) {
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(element, key, &value)) return nil;
    return CFBridgingRelease(value);
}

static NSString *text(AXUIElementRef element, CFStringRef key) {
    id value = attribute(element, key);
    return [value isKindOfClass:NSString.class] ? value : @"";
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

static NSDictionary *rectangle(CGRect value) {
    return @{@"x": @(value.origin.x), @"y": @(value.origin.y),
             @"width": @(value.size.width), @"height": @(value.size.height)};
}

static CGRect viewport(AXUIElementRef notice, AXUIElementRef window) {
    CGRect clip = frame(window);
    id current = attribute(notice, kAXParentAttribute);
    for (int depth = 0; current && depth < 45; depth++) {
        AXUIElementRef parent = (__bridge AXUIElementRef)current;
        if (CFEqual(parent, window)) return clip;
        if ([@[@"AXScrollArea", @"AXWebArea"] containsObject:text(parent, kAXRoleAttribute)])
            clip = CGRectIntersection(clip, frame(parent));
        if (CGRectIsEmpty(clip) || CGRectIsNull(clip)) return CGRectZero;
        current = attribute(parent, kAXParentAttribute);
    }
    return CGRectZero;
}

static BOOL inView(CGRect notice, CGRect clip) {
    return !CGRectIsEmpty(notice) && !CGRectIsEmpty(clip) && !CGRectIsNull(clip) &&
        CGRectContainsRect(clip, notice);
}

static int emit(NSDictionary *row, int code) {
    NSData *data = [NSJSONSerialization dataWithJSONObject:row options:0 error:nil];
    fwrite(data.bytes, 1, data.length, stdout); puts("");
    return code;
}

static NSString *normalized(NSString *value) {
    NSArray *parts = [[value componentsSeparatedByCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet]
        filteredArrayUsingPredicate:[NSPredicate predicateWithFormat:@"length > 0"]];
    return [parts componentsJoinedByString:@" "];
}

static BOOL realElement(id value, pid_t pid) {
    pid_t owner = 0;
    return value && CFGetTypeID((__bridge CFTypeRef)value) == AXUIElementGetTypeID() &&
        AXUIElementGetPid((__bridge AXUIElementRef)value, &owner) == kAXErrorSuccess && owner == pid;
}

static BOOL geometry(AXUIElementRef element, CGRect *out) {
    id position = attribute(element, kAXPositionAttribute), size = attribute(element, kAXSizeAttribute);
    CGPoint origin; CGSize extent;
    if (!position || !size || CFGetTypeID((__bridge CFTypeRef)position) != AXValueGetTypeID() ||
        CFGetTypeID((__bridge CFTypeRef)size) != AXValueGetTypeID() ||
        AXValueGetType((__bridge AXValueRef)position) != kAXValueCGPointType ||
        AXValueGetType((__bridge AXValueRef)size) != kAXValueCGSizeType ||
        !AXValueGetValue((__bridge AXValueRef)position, kAXValueCGPointType, &origin) ||
        !AXValueGetValue((__bridge AXValueRef)size, kAXValueCGSizeType, &extent) ||
        !isfinite(origin.x) || !isfinite(origin.y) || !isfinite(extent.width) || !isfinite(extent.height) ||
        extent.width <= 0 || extent.height <= 0) return NO;
    *out = (CGRect){origin, extent}; return YES;
}

// Follow the actual target's ancestry, never an arbitrary scroll area from the
// page. The innermost area also identifies whether a hit belongs to a nested box.
static NSDictionary *scrollContext(AXUIElementRef element, AXUIElementRef window, pid_t pid) {
    CGRect clip;
    if (!geometry(window, &clip)) return nil;
    id current = (__bridge id)element, scrollOwner = nil;
    NSMutableArray *ancestors = [NSMutableArray array];
    for (int depth = 0; current && depth < 45; depth++) {
        if (!realElement(current, pid)) return nil;
        AXUIElementRef parent = (__bridge AXUIElementRef)current;
        if (CFEqual(parent, window))
            return scrollOwner ? @{@"element": scrollOwner, @"viewport": rectangle(clip), @"ancestors": ancestors} : nil;
        NSString *role = text(parent, kAXRoleAttribute);
        if (!role.length) return nil;
        if (!CFEqual(parent, element)) {
            // Overflow divs can be AXGroup rather than AXScrollArea in WebKit.
            // Clip through every real ancestor instead of treating the whole
            // native window as the transcript viewport.
            CGRect bounds;
            if (!geometry(parent, &bounds)) return nil;
            if (!scrollOwner && [role isEqual:@"AXScrollArea"]) scrollOwner = current;
            clip = CGRectIntersection(clip, bounds);
            if (CGRectIsEmpty(clip) || CGRectIsNull(clip)) return nil;
            [ancestors addObject:@{@"role": role, @"owner_pid": @(pid), @"frame": rectangle(bounds)}];
        }
        current = attribute(parent, kAXParentAttribute);
    }
    return nil;
}

static CGRect fromRectangle(NSDictionary *value) {
    return CGRectMake([value[@"x"] doubleValue], [value[@"y"] doubleValue],
                      [value[@"width"] doubleValue], [value[@"height"] doubleValue]);
}

static NSDictionary *readUp(AXUIElementRef app, AXUIElementRef window, AXUIElementRef notice, pid_t pid) {
    // Reveal the unique receipt once so the wheel can hit that exact native
    // node. A window-wide scroll area is not enough to exclude the sidebar.
    AXError reveal = AXUIElementPerformAction(notice, CFSTR("AXScrollToVisible"));
    NSMutableDictionary *row = [@{@"wheel_posted": @NO, @"wheel_count": @0,
        @"notice_count": @1, @"notice_reveal_requested": @YES, @"notice_reveal_status": @(reveal)} mutableCopy];
    if (reveal) { row[@"error"] = @"unique reading notice could not be revealed"; return row; }
    NSDictionary *context = scrollContext(notice, window, pid);
    CGRect box;
    if (!context || !geometry(notice, &box)) {
        row[@"error"] = @"actual notice has no verified transcript viewport"; return row;
    }
    CGRect clip = fromRectangle(context[@"viewport"]);
    row[@"notice_frame"] = rectangle(box); row[@"viewport_frame"] = context[@"viewport"];
    row[@"ancestors"] = context[@"ancestors"]; row[@"scrollarea_verified"] = @YES;
    row[@"notice_in_view"] = @(inView(box, clip));
    if (!inView(box, clip)) { row[@"error"] = @"reading notice is outside its clipped ancestry"; return row; }
    CGPoint point = CGPointMake(CGRectGetMidX(box), CGRectGetMidY(box));
    AXUIElementRef hit = NULL;
    AXError status = AXUIElementCopyElementAtPosition(app, point.x, point.y, &hit);
    BOOL verified = !status && realElement((__bridge id)hit, pid) && CFEqual(hit, notice);
    row[@"point"] = @{@"x": @(point.x), @"y": @(point.y)};
    row[@"hit_verified"] = @(verified); row[@"hit_status"] = @(status);
    if (hit) CFRelease(hit);
    if (!verified) { row[@"error"] = @"reading point did not hit the unique actual notice"; return row; }
    // Refresh without activation or focus. Revalidate bundle identity immediately
    // before the single event; an unknown refreshed target cannot receive input.
    [NSRunLoop.currentRunLoop runMode:NSDefaultRunLoopMode beforeDate:NSDate.date];
    NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
    NSString *bundle = NSProcessInfo.processInfo.environment[@"ZWAI_NATIVE_APP"].stringByResolvingSymlinksInPath;
    if (!running.active || running.processIdentifier != pid ||
        ![running.bundleURL.path.stringByResolvingSymlinksInPath isEqual:bundle]) {
        row[@"error"] = @"reading wheel requires the already-active private app"; return row;
    }
    CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 1, 80);
    if (!event) { row[@"error"] = @"native wheel allocation failed"; return row; }
    CGEventSetLocation(event, point); CGEventSetFlags(event, 0);
    row[@"wheel_delta"] = @(CGEventGetIntegerValueField(event, kCGScrollWheelEventDeltaAxis1));
    CGEventPostToPid(pid, event); CFRelease(event);
    row[@"wheel_posted"] = @YES; row[@"wheel_count"] = @1;
    return row;
}

static NSString *knownText(AXUIElementRef element, CFStringRef key, BOOL *known) {
    CFTypeRef raw = NULL;
    AXError status = AXUIElementCopyAttributeValue(element, key, &raw);
    id value = CFBridgingRelease(raw);
    if (status == kAXErrorNoValue || status == kAXErrorAttributeUnsupported) return @"";
    if (status || ![value isKindOfClass:NSString.class]) { *known = NO; return @""; }
    return value;
}

static void collect(AXUIElementRef element, NSMutableArray *matches, NSMutableArray *notices,
                    NSMutableArray *targets, NSString *needle, NSArray *path, CFMutableSetRef seen,
                    NSUInteger *visits, BOOL *truncated, pid_t pid, int depth) {
    if (*truncated) return;
    if (!realElement((__bridge id)element, pid)) { *truncated = YES; return; }
    if (++*visits > 5000) { *truncated = YES; return; }
    if (CFSetContainsValue(seen, element)) return;
    if (depth > 45) { *truncated = YES; return; }
    CFSetAddValue(seen, element);
    BOOL known = YES;
    NSString *role = knownText(element, kAXRoleAttribute, &known);
    if (!known || !role.length) { *truncated = YES; return; }
    NSString *label = @"";
    if ([role isEqual:@"AXButton"]) {
        label = knownText(element, kAXTitleAttribute, &known);
        if (!label.length) label = knownText(element, kAXDescriptionAttribute, &known);
        if (!known) { *truncated = YES; return; }
    }
    BOOL workLabel = [label isEqual:@"Thought"] || [label isEqual:@"思考"] ||
        [label hasPrefix:@"Thought · "] || [label hasPrefix:@"思考 · "] ||
        [label rangeOfString:@"^[0-9]+ (tools?|个工具)$" options:NSRegularExpressionSearch].location != NSNotFound;
    if ([role isEqual:@"AXButton"] && workLabel)
        [matches addObject:(__bridge id)element];
    NSString *content = @"";
    if ([@[@"AXStaticText", @"AXHeading"] containsObject:role]) {
        content = knownText(element, kAXValueAttribute, &known);
        if (!content.length) content = knownText(element, kAXTitleAttribute, &known);
        if (!known) { *truncated = YES; return; }
    }
    // These are localized UI receipts, never the user's objective or answer.
    if ([@[@"AXStaticText", @"AXHeading"] containsObject:role] &&
        [@[@"继续追求目标。", @"Continuing the standing objective.", @"Resuming the standing objective."] containsObject:content])
        [notices addObject:(__bridge id)element];
    if (needle.length && [@[@"AXStaticText", @"AXHeading"] containsObject:role] &&
        [normalized(content) containsString:needle]) [targets addObject:@{@"element": (__bridge id)element, @"path": path}];
    CFTypeRef value = NULL;
    AXError status = AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &value);
    id children = CFBridgingRelease(value);
    if (status == kAXErrorNoValue || status == kAXErrorAttributeUnsupported) return;
    if (status || ![children isKindOfClass:NSArray.class]) { *truncated = YES; return; }
    for (NSUInteger index = 0; index < [children count]; index++)
        collect((__bridge AXUIElementRef)children[index], matches, notices, targets, needle, [path arrayByAddingObject:@(index)], seen, visits, truncated, pid, depth + 1);
}

int main(int argc, const char **argv) { @autoreleasepool {
    int virtualized = 0; size_t size = sizeof(virtualized);
    if (sysctlbyname("kern.hv_vmm_present", &virtualized, &size, NULL, 0) || virtualized != 1)
        return emit(@{@"error": @"macOS VM required; physical host refused"}, 2);
    if (argc != 4 && argc != 5) return emit(@{@"error": @"expected private pid, action, bundle and optional text"}, 2);
    char *tail = NULL; long parsed = strtol(argv[1], &tail, 10);
    if (!tail || *tail || parsed <= 0 || parsed > INT_MAX)
        return emit(@{@"error": @"invalid private app PID"}, 2);
    pid_t pid = (pid_t)parsed;
    NSString *action = @(argv[2]);
    if (![@[@"startup", @"inspect", @"reveal-notice", @"read-up", @"reveal-text", @"inspect-text"] containsObject:action])
        return emit(@{@"error": @"unsupported session action"}, 2);
    BOOL textAction = [@[@"reveal-text", @"inspect-text"] containsObject:action];
    id options = argc == 5 ? [NSJSONSerialization JSONObjectWithData:[@(argv[4]) dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil] : nil;
    NSString *needle = [options isKindOfClass:NSDictionary.class] && [options[@"witness"] isKindOfClass:NSString.class] ? normalized(options[@"witness"]) : @"";
    id unique = [options isKindOfClass:NSDictionary.class] ? options[@"unique"] : nil;
    if (textAction ? (!needle.length || !unique || CFGetTypeID((__bridge CFTypeRef)unique) != CFBooleanGetTypeID()) : argc != 4)
        return emit(@{@"error": @"unexpected witness arguments"}, 2);
    NSArray *requestedPath = [options isKindOfClass:NSDictionary.class] ? options[@"path"] : nil;
    if ([action isEqual:@"inspect-text"] && (![requestedPath isKindOfClass:NSArray.class] || !requestedPath.count))
        return emit(@{@"error": @"inspection requires the revealed target path"}, 2);
    if (requestedPath) {
        if (![requestedPath isKindOfClass:NSArray.class] || requestedPath.count > 45)
            return emit(@{@"error": @"target path is malformed"}, 2);
        for (id index in requestedPath)
            if (![index isKindOfClass:NSNumber.class] || CFGetTypeID((__bridge CFTypeRef)index) == CFBooleanGetTypeID() ||
                CFNumberIsFloatType((__bridge CFNumberRef)index) || [index longLongValue] < 0)
                return emit(@{@"error": @"target path index is malformed"}, 2);
    }
    NSString *bundle = [@(argv[3]) stringByResolvingSymlinksInPath];
    NSString *expected = NSProcessInfo.processInfo.environment[@"ZWAI_NATIVE_APP"];
    NSString *caseID = NSProcessInfo.processInfo.environment[@"ZWAI_NATIVE_CASE_ID"];
    NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
    if (!caseID.length || !expected.length || ![bundle isEqual:expected.stringByResolvingSymlinksInPath])
        return emit(@{@"error": @"private bundle or Accessibility identity refused"}, 2);
    NSString *observed = running.bundleURL.path.stringByResolvingSymlinksInPath;
    if (observed.length && ![observed isEqual:bundle])
        return emit(@{@"error": @"registered native target bundle identity mismatch", @"pid": @(pid),
                      @"bundle": bundle, @"observed_bundle": observed}, 2);
    if ([action isEqual:@"startup"])
        return emit(@{@"pid": @(pid), @"bundle": bundle, @"action": action,
                      @"registered": observed.length ? @YES : @NO,
                      @"observed_bundle": observed ?: NSNull.null,
                      @"running_application_present": running ? @YES : @NO,
                      @"finished_launching": running ? @(running.finishedLaunching) : NSNull.null}, 0);
    if (!observed.length || !AXIsProcessTrusted())
        return emit(@{@"error": @"private bundle or Accessibility identity refused"}, 2);
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    AXUIElementSetMessagingTimeout(app, 2);
    NSArray *windows = attribute(app, kAXWindowsAttribute);
    if (![windows isKindOfClass:NSArray.class] || windows.count != 1) {
        CFRelease(app); return emit(@{@"error": @"one private native window required"}, 2);
    }
    __attribute__((objc_precise_lifetime)) id windowOwner = windows[0];
    pid_t owner = 0;
    if (CFGetTypeID((__bridge CFTypeRef)windowOwner) != AXUIElementGetTypeID() ||
        ![text((__bridge AXUIElementRef)windowOwner, kAXRoleAttribute) isEqual:@"AXWindow"] ||
        AXUIElementGetPid((__bridge AXUIElementRef)windowOwner, &owner) != kAXErrorSuccess || owner != pid) {
        CFRelease(app); return emit(@{@"error": @"private AX window type, role or owner refused"}, 2);
    }
    NSMutableArray *matches = [NSMutableArray array], *notices = [NSMutableArray array], *targets = [NSMutableArray array];
    CFMutableSetRef seen = CFSetCreateMutable(kCFAllocatorDefault, 0, &kCFTypeSetCallBacks);
    NSUInteger visits = 0; BOOL truncated = NO;
    collect((__bridge AXUIElementRef)windows[0], matches, notices, targets, needle, @[], seen, &visits, &truncated, pid, 0);
    CFRelease(seen);
    if (truncated) {
        CFRelease(app);
        return emit(@{@"error": @"complete bounded AX traversal required"}, 2);
    }
    NSMutableArray *folds = [NSMutableArray array];
    for (id candidate in matches) {
        AXUIElementRef fold = (__bridge AXUIElementRef)candidate;
        id expanded = attribute(fold, kAXExpandedAttribute);
        if (!expanded || CFGetTypeID((__bridge CFTypeRef)expanded) != CFBooleanGetTypeID()) {
            CFRelease(app); return emit(@{@"error": @"work disclosure lacks a boolean AXExpanded state"}, 2);
        }
        NSString *label = text(fold, kAXTitleAttribute);
        if (!label.length) label = text(fold, kAXDescriptionAttribute);
        [folds addObject:@{@"label": label, @"expanded": expanded}];
    }
    AXError error = kAXErrorSuccess;
    NSMutableDictionary *result = [@{@"pid": @(pid), @"bundle": bundle, @"action": action,
                             @"folds": folds, @"pressed": @NO, @"ax_error": @(error)} mutableCopy];
    if ([action isEqual:@"read-up"]) {
        if (notices.count != 1) { CFRelease(app); return emit(@{@"error": @"one actual continuation notice required before reading", @"wheel_posted": @NO}, 2); }
        [result addEntriesFromDictionary:readUp(app, (__bridge AXUIElementRef)windowOwner, (__bridge AXUIElementRef)notices[0], pid)];
    }
    if (textAction) {
        if (!targets.count || ([unique boolValue] && targets.count != 1)) {
            CFRelease(app); return emit(@{@"error": @"text witness must have its required complete target count", @"matched_count": @(targets.count)}, 2);
        }
        // Shared provider closing prose is only common tail evidence. Select one
        // actual path once; it never establishes either turn's answer identity.
        NSDictionary *selected = nil;
        for (NSDictionary *candidate in targets)
            if (!requestedPath || [candidate[@"path"] isEqual:requestedPath]) {
                if (selected) { CFRelease(app); return emit(@{@"error": @"text target path is ambiguous"}, 2); }
                selected = candidate;
                if (!requestedPath) break;
            }
        if (!selected) { CFRelease(app); return emit(@{@"error": @"revealed target path no longer matches its witness"}, 2); }
        AXUIElementRef target = (__bridge AXUIElementRef)selected[@"element"];
        BOOL revealText = [action isEqual:@"reveal-text"];
        if (revealText) error = AXUIElementPerformAction(target, CFSTR("AXScrollToVisible"));
        CGRect bounds;
        NSDictionary *context = scrollContext(target, (__bridge AXUIElementRef)windowOwner, pid);
        if (!context || !geometry(target, &bounds)) { CFRelease(app); return emit(@{@"error": @"target geometry or ancestry is unknown"}, 2); }
        NSString *content = text(target, kAXValueAttribute);
        if (!content.length) content = text(target, kAXTitleAttribute);
        NSDictionary *targetRow = @{@"text": content, @"path": selected[@"path"], @"frame": rectangle(bounds),
            @"viewport": context[@"viewport"], @"ancestors": context[@"ancestors"],
            @"in_view": @(inView(bounds, fromRectangle(context[@"viewport"]))), @"owner_pid": @(pid)};
        [result addEntriesFromDictionary:@{@"matched_count": @(targets.count), @"target_count": @1,
            @"selected_path": selected[@"path"], @"selection": [unique boolValue] ? @"unique opening" : @"common prose evidence",
            @"reveal_count": revealText ? @1 : @0, @"targets": @[targetRow], @"ax_error": @(error)}];
    }
    if ([action isEqual:@"reveal-notice"]) {
        if (notices.count != 1) {
            CFRelease(app); return emit(@{@"error": @"one visible continuation notice required before reveal"}, 2);
        }
        AXUIElementRef notice = (__bridge AXUIElementRef)notices[0];
        CGRect before = frame(notice), clip = viewport(notice, (__bridge AXUIElementRef)windows[0]);
        BOOL visible = inView(before, clip);
        if (!visible) error = AXUIElementPerformAction(notice, CFSTR("AXScrollToVisible"));
        CGRect after = frame(notice);
        clip = viewport(notice, (__bridge AXUIElementRef)windows[0]);
        [result addEntriesFromDictionary:@{@"notice_count": @(notices.count), @"scroll_requested": visible ? @NO : @YES,
            @"notice_was_in_view": visible ? @YES : @NO, @"notice_in_view": inView(after, clip) ? @YES : @NO,
            @"notice_frame": rectangle(after), @"viewport_frame": rectangle(clip), @"ax_error": @(error)}];
    }
    CFRelease(app);
    return emit(result, result[@"error"] ? 2 : error ? 3 : 0);
} }
