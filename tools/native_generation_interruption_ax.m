#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <CoreGraphics/CoreGraphics.h>
#include <sys/sysctl.h>

static id attr(AXUIElementRef el, CFStringRef key) {
    CFTypeRef out = NULL;
    if (AXUIElementCopyAttributeValue(el, key, &out)) return nil;
    return CFBridgingRelease(out);
}
static NSString *str(AXUIElementRef el, CFStringRef key) {
    id out = attr(el, key);
    return [out isKindOfClass:NSString.class] ? out : @"";
}
static void walk(AXUIElementRef el, NSMutableArray *rows, NSMutableArray *matches,
                 NSString *action, NSString *needle, int depth) {
    if (depth > 50 || rows.count > 6000) return;
    NSMutableDictionary *row = [NSMutableDictionary dictionary];
    for (NSString *key in @[@"AXRole", @"AXTitle", @"AXDescription", @"AXValue", @"AXURL"]) {
        id value = attr(el, (__bridge CFStringRef)key);
        if ([value isKindOfClass:NSURL.class]) value = [value absoluteString];
        if ([value isKindOfClass:NSString.class] && [value length]) row[key] = value;
    }
    [rows addObject:row];
    NSString *role = str(el, kAXRoleAttribute);
    if ([action isEqual:@"fill"] && [role isEqual:@"AXTextArea"])
        [matches addObject:(__bridge id)el];
    else if ([action isEqual:@"reload"] && [role isEqual:@"AXMenuItem"] && [str(el, kAXTitleAttribute) isEqual:@"Reload"])
        [matches addObject:(__bridge id)el];
    else if ([action isEqual:@"press"] && needle.length &&
             ([role isEqual:@"AXButton"] || [role isEqual:@"AXRadioButton"] || [role isEqual:@"AXCheckBox"]) &&
             ([str(el, kAXTitleAttribute) containsString:needle] || [str(el, kAXDescriptionAttribute) containsString:needle]))
        [matches addObject:(__bridge id)el];
    id children = attr(el, kAXChildrenAttribute);
    if ([children isKindOfClass:NSArray.class])
        for (id child in children) walk((__bridge AXUIElementRef)child, rows, matches, action, needle, depth + 1);
}
static int emit(id row, int code) {
    NSData *raw = [NSJSONSerialization dataWithJSONObject:row options:NSJSONWritingPrettyPrinted error:nil];
    fwrite(raw.bytes, 1, raw.length, stdout); puts(""); return code;
}
int main(int argc, const char **argv) { @autoreleasepool {
    int vm = 0; size_t size = sizeof(vm);
    if (sysctlbyname("kern.hv_vmm_present", &vm, &size, NULL, 0) || vm != 1 || argc < 3)
        return emit(@{@"error": @"VM pid/action required"}, 2);
    pid_t pid = atoi(argv[1]); NSString *action = @(argv[2]);
    NSString *needle = argc == 4 ? @(argv[3]) : @"";
    NSString *expected = NSProcessInfo.processInfo.environment[@"ZWAI_NATIVE_APP"];
    NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
    if (!expected.length || ![running.bundleURL.path.stringByResolvingSymlinksInPath isEqual:expected.stringByResolvingSymlinksInPath] || !AXIsProcessTrusted())
        return emit(@{@"error": @"isolated bundle identity or Accessibility refused"}, 2);
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    AXUIElementSetAttributeValue(app, CFSTR("AXEnhancedUserInterface"), kCFBooleanTrue);
    AXUIElementSetAttributeValue(app, CFSTR("AXManualAccessibility"), kCFBooleanTrue);
    NSMutableArray *rows = [NSMutableArray array], *matches = [NSMutableArray array], *windows = [NSMutableArray array];
    walk(app, rows, matches, action, needle, 0);
    if ([action isEqual:@"press"]) {
        NSMutableArray *exact = [NSMutableArray array];
        for (id element in matches) {
            AXUIElementRef candidate = (__bridge AXUIElementRef)element;
            if ([str(candidate, kAXTitleAttribute) isEqual:needle] || [str(candidate, kAXDescriptionAttribute) isEqual:needle])
                [exact addObject:element];
        }
        if (exact.count) matches = exact;
    }
    NSArray *all = CFBridgingRelease(CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID));
    for (NSDictionary *row in all) {
        NSDictionary *bounds = row[(id)kCGWindowBounds];
        if ([row[(id)kCGWindowOwnerPID] intValue] == pid && [row[(id)kCGWindowLayer] intValue] == 0 && [bounds[@"Width"] intValue] > 500)
            [windows addObject:@{@"id": row[(id)kCGWindowNumber], @"bounds": bounds}];
    }
    AXError error = kAXErrorSuccess;
    if ([action isEqual:@"inspect"]) {}
    else if ([action isEqual:@"press"] && matches.count == 1)
        error = AXUIElementPerformAction((__bridge AXUIElementRef)matches[0], kAXPressAction);
    else if ([action isEqual:@"reload"] && matches.count == 1)
        error = AXUIElementPerformAction((__bridge AXUIElementRef)matches[0], kAXPressAction);
    else if ([action isEqual:@"fill"] && matches.count == 1 && str((__bridge AXUIElementRef)matches[0], kAXValueAttribute).length == 0) {
        error = AXUIElementSetAttributeValue((__bridge AXUIElementRef)matches[0], kAXFocusedAttribute, kCFBooleanTrue);
        if (!error) for (NSUInteger i = 0; i < needle.length; i++) {
            UniChar character = [needle characterAtIndex:i];
            for (int down = 1; down >= 0; down--) {
                CGEventRef event = CGEventCreateKeyboardEvent(NULL, 0, down);
                CGEventSetFlags(event, 0); CGEventKeyboardSetUnicodeString(event, 1, &character);
                CGEventPostToPid(pid, event); CFRelease(event);
            }
            usleep(12000);
        }
    } else if ([action isEqual:@"close"]) {
        NSArray *native = attr(app, kAXWindowsAttribute);
        id button = native.count == 1 ? attr((__bridge AXUIElementRef)native[0], kAXCloseButtonAttribute) : nil;
        error = button ? AXUIElementPerformAction((__bridge AXUIElementRef)button, kAXPressAction) : kAXErrorNoValue;
    } else error = kAXErrorNoValue;
    CFRelease(app);
    return emit(@{@"pid": @(pid), @"bundle": running.bundleURL.path, @"action": action,
                  @"matches": @(matches.count), @"ax_error": @(error), @"tree": rows, @"windows": windows}, error ? 3 : 0);
} }
