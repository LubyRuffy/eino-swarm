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

static NSDictionary *rectangle(CGRect value) {
    return @{@"x": @(value.origin.x), @"y": @(value.origin.y),
             @"width": @(value.size.width), @"height": @(value.size.height)};
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

static BOOL realElement(id value, pid_t *owner) {
    *owner = 0;
    return value && CFGetTypeID((__bridge CFTypeRef)value) == AXUIElementGetTypeID() &&
        AXUIElementGetPid((__bridge AXUIElementRef)value, owner) == kAXErrorSuccess && *owner > 0;
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
// WebKit may expose remote-process children and canonical parents from another
// Children branch. Bind the selected node through both actual graphs instead
// of assuming that every descendant shares the native window's owner PID.
static NSDictionary *scrollContext(AXUIElementRef element, AXUIElementRef window, NSSet *reachable, pid_t pid, BOOL allowOffscreen) {
    if (!CFSetContainsValue((__bridge CFSetRef)reachable, element)) return nil;
    CGRect clip, targetBounds;
    if (!geometry(window, &clip) || !geometry(element, &targetBounds)) return nil;
    id current = (__bridge id)element, scrollOwner = nil;
    NSMutableArray *ancestors = [NSMutableArray array];
    NSMutableSet *parents = CFBridgingRelease(CFSetCreateMutable(kCFAllocatorDefault, 0, &kCFTypeSetCallBacks));
    pid_t targetOwner = 0;
    for (int depth = 0; current && depth < 45; depth++) {
        pid_t owner = 0;
        if (!realElement(current, &owner) || CFSetContainsValue((__bridge CFSetRef)parents, (__bridge CFTypeRef)current)) return nil;
        CFSetAddValue((__bridge CFMutableSetRef)parents, (__bridge CFTypeRef)current);
        if (!depth) targetOwner = owner;
        AXUIElementRef parent = (__bridge AXUIElementRef)current;
        NSString *role = text(parent, kAXRoleAttribute);
        if (!role.length) return nil;
        if (CFEqual(parent, window)) {
            if (owner != pid || ![role isEqual:@"AXWindow"] || !scrollOwner) return nil;
            return @{@"element": scrollOwner, @"viewport": rectangle(clip), @"ancestors": ancestors,
                @"viewport_basis": @"native_window_scrollarea_webarea",
                @"owner_pid": @(targetOwner), @"binding": @{@"children_reachable": @YES, @"parent_chain_root": @YES,
                    @"root_owner_pid": @(owner), @"root_role": role}};
        }
        if (!CFEqual(parent, element)) {
            // AXGroup layout bounds do not establish CSS clipping. Native viewport
            // geometry cannot establish every CSS clip; retain those bounds for
            // diagnostics and use the actual window PNG for visual acceptance.
            CGRect bounds;
            if (!geometry(parent, &bounds)) return nil;
            if (!scrollOwner && [role isEqual:@"AXScrollArea"]) scrollOwner = current;
            BOOL viewportClip = [@[@"AXScrollArea", @"AXWebArea"] containsObject:role];
            if (viewportClip) {
                clip = CGRectIntersection(clip, bounds);
                if (CGRectIsEmpty(clip) || CGRectIsNull(clip)) {
                    // A known offscreen scroller still needs one reveal, with
                    // every remaining ancestor's geometry and binding checked.
                    if (!allowOffscreen) return nil;
                    clip = CGRectZero;
                }
            }
            [ancestors addObject:@{@"role": role, @"owner_pid": @(owner), @"frame": rectangle(bounds), @"used_for_native_clip": @(viewportClip)}];
        }
        current = attribute(parent, kAXParentAttribute);
    }
    return nil;
}

static CGRect fromRectangle(NSDictionary *value) {
    return CGRectMake([value[@"x"] doubleValue], [value[@"y"] doubleValue],
                      [value[@"width"] doubleValue], [value[@"height"] doubleValue]);
}

static NSDictionary *readUp(AXUIElementRef app, AXUIElementRef window, AXUIElementRef notice, NSSet *reachable, pid_t pid) {
    // Reveal the unique receipt once so the wheel can hit that exact native
    // node. A window-wide scroll area is not enough to exclude the sidebar.
    NSMutableDictionary *row = [@{@"wheel_posted": @NO, @"wheel_count": @0,
        @"notice_count": @1, @"notice_reveal_requested": @NO} mutableCopy];
    NSDictionary *context = scrollContext(notice, window, reachable, pid, YES);
    if (!context) { row[@"error"] = @"actual notice has no verified root binding"; return row; }
    AXError reveal = AXUIElementPerformAction(notice, CFSTR("AXScrollToVisible"));
    row[@"notice_reveal_requested"] = @YES; row[@"notice_reveal_status"] = @(reveal);
    if (reveal) { row[@"error"] = @"unique reading notice could not be revealed"; return row; }
    context = scrollContext(notice, window, reachable, pid, NO);
    CGRect box;
    if (!context || !geometry(notice, &box)) {
        row[@"error"] = @"actual notice has no verified transcript viewport"; return row;
    }
    CGRect clip = fromRectangle(context[@"viewport"]);
    row[@"notice_frame"] = rectangle(box); row[@"viewport_frame"] = context[@"viewport"];
    row[@"viewport_basis"] = context[@"viewport_basis"];
    row[@"notice_owner_pid"] = context[@"owner_pid"]; row[@"binding"] = context[@"binding"];
    row[@"ancestors"] = context[@"ancestors"]; row[@"scrollarea_verified"] = @YES;
    row[@"notice_in_view"] = @(inView(box, clip));
    if (!inView(box, clip)) { row[@"error"] = @"reading notice is outside its native scroller viewport"; return row; }
    CGPoint point = CGPointMake(CGRectGetMidX(box), CGRectGetMidY(box));
    AXUIElementRef hit = NULL;
    AXError status = AXUIElementCopyElementAtPosition(app, point.x, point.y, &hit);
    pid_t hitOwner = 0;
    BOOL verified = !status && realElement((__bridge id)hit, &hitOwner) && CFEqual(hit, notice) &&
        hitOwner == [context[@"owner_pid"] intValue];
    row[@"point"] = @{@"x": @(point.x), @"y": @(point.y)};
    row[@"hit_verified"] = @(verified); row[@"hit_status"] = @(status);
    row[@"hit_owner_pid"] = hitOwner > 0 ? @(hitOwner) : NSNull.null;
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
    NSDictionary *wheelContext = scrollContext(notice, window, reachable, pid, NO);
    if (!wheelContext || ![wheelContext[@"owner_pid"] isEqual:context[@"owner_pid"]]) {
        row[@"error"] = @"reading notice root binding changed before the wheel"; return row;
    }
    row[@"binding"] = wheelContext[@"binding"]; row[@"ancestors"] = wheelContext[@"ancestors"];
    CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 1, 80);
    if (!event) { row[@"error"] = @"native wheel allocation failed"; return row; }
    CGEventSetLocation(event, point); CGEventSetFlags(event, 0);
    row[@"wheel_delta"] = @(CGEventGetIntegerValueField(event, kCGScrollWheelEventDeltaAxis1));
    CGEventPostToPid(pid, event); CFRelease(event);
    row[@"wheel_posted"] = @YES; row[@"wheel_count"] = @1;
    return row;
}

static NSDictionary *readDetails(CFStringRef key, AXError status, CFTypeRef raw) {
    return @{@"key": (__bridge NSString *)key, @"status": @(status), @"raw_null": @(raw == NULL),
             @"cf_null": @(raw && CFGetTypeID(raw) == CFNullGetTypeID()),
             @"cf_type": raw ? @(CFGetTypeID(raw)) : NSNull.null};
}

static void refuseScan(NSMutableDictionary *first, BOOL *truncated, NSString *predicate,
                       NSArray *path, NSUInteger visits, int depth, NSString *role,
                       NSDictionary *details, pid_t expected) {
    *truncated = YES;
    if (first.count) return;
    [first addEntriesFromDictionary:@{@"predicate": predicate, @"path": path, @"visit": @(visits),
        @"depth": @(depth), @"visit_cap": @5000, @"depth_cap": @45,
        @"role": role ?: NSNull.null, @"expected_pid": @(expected), @"observed_pid": NSNull.null}];
    [first addEntriesFromDictionary:details];
}

static NSString *knownText(AXUIElementRef element, CFStringRef key, BOOL *known, NSMutableDictionary *reading) {
    CFTypeRef raw = NULL;
    AXError status = AXUIElementCopyAttributeValue(element, key, &raw);
    if (*known) { [reading removeAllObjects]; [reading addEntriesFromDictionary:readDetails(key, status, raw)]; }
    id value = CFBridgingRelease(raw);
    if (status == kAXErrorNoValue || status == kAXErrorAttributeUnsupported) return @"";
    if (status || ![value isKindOfClass:NSString.class]) { *known = NO; return @""; }
    return value;
}

static NSString *knownContent(AXUIElementRef element, NSString *role, BOOL *known, NSMutableDictionary *reading) {
    // WebKit's Heading AXValue is the heading level, not its rendered text.
    // Its textual children are collected independently through AXChildren.
    if ([role isEqual:@"AXHeading"]) return knownText(element, kAXTitleAttribute, known, reading);
    if (![role isEqual:@"AXStaticText"]) { *known = NO; return @""; }
    NSString *content = knownText(element, kAXValueAttribute, known, reading);
    if (!content.length) content = knownText(element, kAXTitleAttribute, known, reading);
    return content;
}

static void collect(AXUIElementRef element, NSMutableArray *matches, NSMutableArray *notices,
                    NSMutableArray *targets, NSString *needle, NSArray *path, CFMutableSetRef seen,
                    NSUInteger *visits, BOOL *truncated, NSMutableDictionary *first, pid_t pid, int depth) {
    if (*truncated) return;
    BOOL typed = element && CFGetTypeID(element) == AXUIElementGetTypeID();
    pid_t owner = 0;
    AXError ownerStatus = typed ? AXUIElementGetPid(element, &owner) : kAXErrorInvalidUIElement;
    if (!typed || ownerStatus || owner <= 0) {
        NSMutableDictionary *details = [readDetails(CFSTR("AXPID"), ownerStatus, element) mutableCopy];
        details[@"observed_pid"] = ownerStatus ? NSNull.null : @(owner);
        refuseScan(first, truncated, typed ? @"node_owner" : @"node_type", path, *visits + 1, depth, nil, details, pid); return;
    }
    if (++*visits > 5000) { refuseScan(first, truncated, @"visit_limit", path, *visits, depth, nil, @{}, pid); return; }
    if (CFSetContainsValue(seen, element)) return;
    if (depth > 45) { refuseScan(first, truncated, @"depth_limit", path, *visits, depth, nil, @{}, pid); return; }
    CFSetAddValue(seen, element);
    BOOL known = YES;
    NSMutableDictionary *reading = [NSMutableDictionary dictionary];
    NSString *role = knownText(element, kAXRoleAttribute, &known, reading);
    if (!known || !role.length) { refuseScan(first, truncated, @"role_read", path, *visits, depth, nil, reading, pid); return; }
    NSString *label = @"";
    if ([role isEqual:@"AXButton"]) {
        label = knownText(element, kAXTitleAttribute, &known, reading);
        if (!label.length) label = knownText(element, kAXDescriptionAttribute, &known, reading);
        if (!known) { refuseScan(first, truncated, @"button_label_read", path, *visits, depth, role, reading, pid); return; }
    }
    BOOL workLabel = [label isEqual:@"Thought"] || [label isEqual:@"思考"] ||
        [label hasPrefix:@"Thought · "] || [label hasPrefix:@"思考 · "] ||
        [label rangeOfString:@"^[0-9]+ (tools?|个工具)$" options:NSRegularExpressionSearch].location != NSNotFound;
    if ([role isEqual:@"AXButton"] && workLabel)
        [matches addObject:(__bridge id)element];
    NSString *content = @"";
    if ([@[@"AXStaticText", @"AXHeading"] containsObject:role]) {
        content = knownContent(element, role, &known, reading);
        if (!known) { refuseScan(first, truncated, @"text_read", path, *visits, depth, role, reading, pid); return; }
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
    if (status || ![children isKindOfClass:NSArray.class]) {
        refuseScan(first, truncated, @"children_read", path, *visits, depth, role, readDetails(kAXChildrenAttribute, status, (__bridge CFTypeRef)children), pid); return;
    }
    for (NSUInteger index = 0; index < [children count]; index++)
        collect((__bridge AXUIElementRef)children[index], matches, notices, targets, needle, [path arrayByAddingObject:@(index)], seen, visits, truncated, first, pid, depth + 1);
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
    NSMutableDictionary *first = [NSMutableDictionary dictionary];
    collect((__bridge AXUIElementRef)windows[0], matches, notices, targets, needle, @[], seen, &visits, &truncated, first, pid, 0);
    NSSet *reachable = CFBridgingRelease(seen);
    if (truncated) {
        CFRelease(app);
        return emit(@{@"error": @"complete bounded AX traversal required", @"first_refusal": first,
                      @"pid": @(pid), @"action": action, @"pressed": @NO, @"wheel_posted": @NO, @"reveal_count": @0}, 2);
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
                             @"root_window": @{@"role": @"AXWindow", @"owner_pid": @(owner)},
                             @"folds": folds, @"pressed": @NO, @"ax_error": @(error)} mutableCopy];
    if ([action isEqual:@"read-up"]) {
        if (notices.count != 1) { CFRelease(app); return emit(@{@"error": @"one actual continuation notice required before reading", @"wheel_posted": @NO}, 2); }
        [result addEntriesFromDictionary:readUp(app, (__bridge AXUIElementRef)windowOwner, (__bridge AXUIElementRef)notices[0], reachable, pid)];
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
        NSDictionary *context = scrollContext(target, (__bridge AXUIElementRef)windowOwner, reachable, pid, revealText);
        if (!context) { CFRelease(app); return emit(@{@"error": @"target has no verified root binding"}, 2); }
        if (revealText) error = AXUIElementPerformAction(target, CFSTR("AXScrollToVisible"));
        CGRect bounds;
        context = scrollContext(target, (__bridge AXUIElementRef)windowOwner, reachable, pid, NO);
        if (!context || !geometry(target, &bounds)) { CFRelease(app); return emit(@{@"error": @"target geometry or ancestry is unknown"}, 2); }
        BOOL known = YES;
        NSMutableDictionary *reading = [NSMutableDictionary dictionary];
        NSString *role = knownText(target, kAXRoleAttribute, &known, reading);
        NSString *content = knownContent(target, role, &known, reading);
        if (!known) { CFRelease(app); return emit(@{@"error": @"target text readback is unknown", @"reading": reading}, 2); }
        NSDictionary *targetRow = @{@"text": content, @"path": selected[@"path"], @"frame": rectangle(bounds),
            @"viewport": context[@"viewport"], @"ancestors": context[@"ancestors"],
            @"viewport_basis": context[@"viewport_basis"],
            @"in_view": @(inView(bounds, fromRectangle(context[@"viewport"]))),
            @"owner_pid": context[@"owner_pid"], @"binding": context[@"binding"]};
        [result addEntriesFromDictionary:@{@"matched_count": @(targets.count), @"target_count": @1,
            @"selected_path": selected[@"path"], @"selection": [unique boolValue] ? @"unique opening" : @"common prose evidence",
            @"reveal_count": revealText ? @1 : @0, @"targets": @[targetRow], @"ax_error": @(error)}];
    }
    if ([action isEqual:@"reveal-notice"]) {
        if (notices.count != 1) {
            CFRelease(app); return emit(@{@"error": @"one visible continuation notice required before reveal"}, 2);
        }
        AXUIElementRef notice = (__bridge AXUIElementRef)notices[0];
        NSDictionary *context = scrollContext(notice, (__bridge AXUIElementRef)windowOwner, reachable, pid, YES);
        CGRect before;
        if (!context || !geometry(notice, &before)) { CFRelease(app); return emit(@{@"error": @"notice geometry or root binding is unknown"}, 2); }
        CGRect clip = fromRectangle(context[@"viewport"]);
        BOOL visible = inView(before, clip);
        if (!visible) error = AXUIElementPerformAction(notice, CFSTR("AXScrollToVisible"));
        CGRect after;
        context = scrollContext(notice, (__bridge AXUIElementRef)windowOwner, reachable, pid, NO);
        if (!context || !geometry(notice, &after)) { CFRelease(app); return emit(@{@"error": @"notice geometry or root binding is unknown after reveal"}, 2); }
        clip = fromRectangle(context[@"viewport"]);
        [result addEntriesFromDictionary:@{@"notice_count": @(notices.count), @"scroll_requested": visible ? @NO : @YES,
            @"notice_was_in_view": visible ? @YES : @NO, @"notice_in_view": inView(after, clip) ? @YES : @NO,
            @"notice_owner_pid": context[@"owner_pid"], @"binding": context[@"binding"], @"ancestors": context[@"ancestors"],
            @"viewport_basis": context[@"viewport_basis"],
            @"notice_frame": rectangle(after), @"viewport_frame": rectangle(clip), @"ax_error": @(error)}];
    }
    CFRelease(app);
    return emit(result, result[@"error"] ? 2 : error ? 3 : 0);
} }
