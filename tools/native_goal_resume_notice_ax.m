#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
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

static void collect(AXUIElementRef element, NSMutableArray *matches, NSMutableArray *notices,
                    CFMutableSetRef seen, NSUInteger *visits, BOOL *truncated, int depth) {
    if (*truncated) return;
    if (++*visits > 5000) { *truncated = YES; return; }
    if (CFSetContainsValue(seen, element)) return;
    if (depth > 45) { *truncated = YES; return; }
    CFSetAddValue(seen, element);
    NSString *label = text(element, kAXTitleAttribute);
    if (!label.length) label = text(element, kAXDescriptionAttribute);
    BOOL workLabel = [label isEqual:@"Thought"] || [label isEqual:@"思考"] ||
        [label hasPrefix:@"Thought · "] || [label hasPrefix:@"思考 · "] ||
        [label rangeOfString:@"^[0-9]+ (tools?|个工具)$" options:NSRegularExpressionSearch].location != NSNotFound;
    if ([text(element, kAXRoleAttribute) isEqual:@"AXButton"] && workLabel)
        [matches addObject:(__bridge id)element];
    NSString *content = text(element, kAXValueAttribute);
    if (!content.length) content = text(element, kAXTitleAttribute);
    // These are localized UI receipts, never the user's objective or answer.
    if ([@[@"AXStaticText", @"AXHeading"] containsObject:text(element, kAXRoleAttribute)] &&
        [@[@"继续追求目标。", @"Continuing the standing objective.", @"Resuming the standing objective."] containsObject:content])
        [notices addObject:(__bridge id)element];
    id children = attribute(element, kAXChildrenAttribute);
    if ([children isKindOfClass:NSArray.class])
        for (id child in children) collect((__bridge AXUIElementRef)child, matches, notices, seen, visits, truncated, depth + 1);
}

int main(int argc, const char **argv) { @autoreleasepool {
    int virtualized = 0; size_t size = sizeof(virtualized);
    if (sysctlbyname("kern.hv_vmm_present", &virtualized, &size, NULL, 0) || virtualized != 1)
        return emit(@{@"error": @"macOS VM required; physical host refused"}, 2);
    if (argc != 4) return emit(@{@"error": @"expected private pid, action and bundle"}, 2);
    char *tail = NULL; long parsed = strtol(argv[1], &tail, 10);
    if (!tail || *tail || parsed <= 0 || parsed > INT_MAX)
        return emit(@{@"error": @"invalid private app PID"}, 2);
    pid_t pid = (pid_t)parsed;
    NSString *action = @(argv[2]);
    if (![@[@"startup", @"inspect", @"reveal-notice"] containsObject:action])
        return emit(@{@"error": @"unsupported session action"}, 2);
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
    NSMutableArray *matches = [NSMutableArray array], *notices = [NSMutableArray array];
    CFMutableSetRef seen = CFSetCreateMutable(kCFAllocatorDefault, 0, &kCFTypeSetCallBacks);
    NSUInteger visits = 0; BOOL truncated = NO;
    collect((__bridge AXUIElementRef)windows[0], matches, notices, seen, &visits, &truncated, 0);
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
    return emit(result, error ? 3 : 0);
} }
