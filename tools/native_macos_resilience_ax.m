// CI-only native UI helper. Never run outside a private VM case.
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <libproc.h>
#include <sys/proc_info.h>
#include <sys/sysctl.h>

static BOOL guest(void) {
    int flag = 0; size_t size = sizeof(flag);
    return sysctlbyname("kern.hv_vmm_present", &flag, &size, NULL, 0) == 0 && flag == 1;
}
static NSString *env(NSString *key) { return NSProcessInfo.processInfo.environment[key] ?: @""; }
static NSDictionary *identity(pid_t pid) {
    struct proc_bsdinfo info; char path[PROC_PIDPATHINFO_MAXSIZE] = {0};
    if (pid <= 0 || proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info)) != sizeof(info)
        || proc_pidpath(pid, path, sizeof(path)) <= 0) return nil;
    return @{ @"pid": @(pid), @"birth": @[@(info.pbi_start_tvsec), @(info.pbi_start_tvusec)],
              @"executable": @(path) };
}
static BOOL bound(pid_t pid) {
    NSDictionary *info = identity(pid);
    NSString *executable = [info[@"executable"] stringByResolvingSymlinksInPath];
    NSString *root = [[[env(@"ZWAI_NATIVE_APP") stringByResolvingSymlinksInPath] stringByDeletingLastPathComponent] stringByAppendingString:@"/"];
    return guest() && env(@"ZWAI_NATIVE_CASE_ID").length > 0 && root.length > 10
        && [executable hasPrefix:root] && [executable containsString:@".app/Contents/MacOS/"];
}
static id attr(AXUIElementRef item, CFStringRef key) {
    CFTypeRef result = NULL;
    return AXUIElementCopyAttributeValue(item, key, &result) == kAXErrorSuccess ? CFBridgingRelease(result) : nil;
}
static NSString *string(id value) { return [value isKindOfClass:NSString.class] ? value : @""; }
static void collect(AXUIElementRef item, NSMutableArray *areas, NSMutableArray *buttons,
                    NSMutableString *text, NSUInteger *count, BOOL *web) {
    if (++*count > 20000 || text.length > 100000) return;
    NSString *role = string(attr(item, kAXRoleAttribute));
    if ([role isEqual:@"AXWebArea"]) *web = YES;
    if ([role isEqual:@"AXTextArea"]) [areas addObject:(__bridge id)item];
    if ([role isEqual:@"AXButton"]) [buttons addObject:(__bridge id)item];
    for (id key in @[(__bridge id)kAXTitleAttribute, (__bridge id)kAXDescriptionAttribute, (__bridge id)kAXValueAttribute]) {
        NSString *value = string(attr(item, (__bridge CFStringRef)key));
        if (value.length) [text appendFormat:@"%@\n", value];
    }
    id children = attr(item, kAXChildrenAttribute);
    if ([children isKindOfClass:NSArray.class]) for (id child in children)
        collect((__bridge AXUIElementRef)child, areas, buttons, text, count, web);
}
static void describe(AXUIElementRef item, NSMutableArray *rows, NSUInteger depth) {
    if (depth > 8 || rows.count >= 400) return;
    NSMutableDictionary *row = [@{@"depth": @(depth)} mutableCopy];
    CFArrayRef actions = NULL;
    if (AXUIElementCopyActionNames(item, &actions) == kAXErrorSuccess && actions)
        row[@"AXActions"] = CFBridgingRelease(actions);
    for (NSString *key in @[@"AXRole", @"AXSubrole", @"AXTitle", @"AXDescription", @"AXValue"]) {
        NSString *value = string(attr(item, (__bridge CFStringRef)key));
        row[key] = [value substringToIndex:MIN(180,value.length)];
    }
    [rows addObject:row];
    id children = attr(item, kAXChildrenAttribute);
    if ([children isKindOfClass:NSArray.class]) for (id child in [children reverseObjectEnumerator])
        describe((__bridge AXUIElementRef)child, rows, depth+1);
}
static NSArray *sheets(id windows) {
    NSMutableArray *result = [NSMutableArray array];
    if (![windows isKindOfClass:NSArray.class]) return result;
    for (id item in windows) {
        AXUIElementRef window = (__bridge AXUIElementRef)item;
        id children = attr(window, kAXChildrenAttribute);
        if ([children isKindOfClass:NSArray.class]) for (id child in children)
            if ([string(attr((__bridge AXUIElementRef)child, kAXRoleAttribute)) isEqual:@"AXSheet"])
                [result addObject:child];
    }
    return result;
}
static id nestedSheet(AXUIElementRef app) {
    for (id panel in sheets(attr(app, kAXWindowsAttribute))) {
        id children = attr((__bridge AXUIElementRef)panel, kAXChildrenAttribute);
        if ([children isKindOfClass:NSArray.class]) for (id child in children)
            if ([string(attr((__bridge AXUIElementRef)child, kAXRoleAttribute)) isEqual:@"AXSheet"]) return child;
    }
    return nil;
}
static void editable(AXUIElementRef item, NSMutableArray *fields, NSUInteger depth) {
    if (depth > 12 || fields.count > 10) return;
    NSString *role = string(attr(item, kAXRoleAttribute));
    if ([role isEqual:@"AXComboBox"]) { [fields addObject:(__bridge id)item]; return; }
    if ([role isEqual:@"AXTextField"] && ![string(attr(item, kAXSubroleAttribute)) isEqual:@"AXSearchField"])
        [fields addObject:(__bridge id)item];
    id children = attr(item, kAXChildrenAttribute);
    if ([children isKindOfClass:NSArray.class]) for (id child in children)
        editable((__bridge AXUIElementRef)child, fields, depth+1);
}
static void openablePath(AXUIElementRef item, NSString *name, NSMutableArray *matches, NSUInteger depth) {
    if (depth > 12 || matches.count > 1) return;
    if ([string(attr(item, kAXValueAttribute)) isEqual:name]) {
        CFArrayRef actions = NULL;
        if (AXUIElementCopyActionNames(item, &actions) == kAXErrorSuccess && actions) {
            NSArray *names = CFBridgingRelease(actions);
            if ([names containsObject:@"AXOpen"]) [matches addObject:(__bridge id)item];
        }
    }
    id children = attr(item, kAXChildrenAttribute);
    if ([children isKindOfClass:NSArray.class]) for (id child in children)
        openablePath((__bridge AXUIElementRef)child, name, matches, depth+1);
}
static BOOL exactText(AXUIElementRef item, NSString *value, NSUInteger depth) {
    if (depth > 8) return NO;
    if ([string(attr(item, kAXValueAttribute)) isEqual:value] || [string(attr(item, kAXTitleAttribute)) isEqual:value]) return YES;
    id children = attr(item, kAXChildrenAttribute);
    if ([children isKindOfClass:NSArray.class]) for (id child in children)
        if (exactText((__bridge AXUIElementRef)child, value, depth+1)) return YES;
    return NO;
}
static void key(pid_t pid, CGKeyCode code, CGEventFlags flags) {
    for (int down = 1; down >= 0; down--) {
        CGEventRef event = CGEventCreateKeyboardEvent(NULL, code, down);
        CGEventSetFlags(event, flags); CGEventPostToPid(pid, event); CFRelease(event);
    }
}
static BOOL focusedKey(pid_t pid, CGKeyCode code, CGEventFlags flags) {
    if (!CGPreflightPostEventAccess()) return NO;
    for (int down = 1; down >= 0; down--) {
        if (NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier != pid) return NO;
        CGEventRef event = CGEventCreateKeyboardEvent(NULL, code, down);
        CGEventSetFlags(event, flags); CGEventPost(kCGSessionEventTap, event); CFRelease(event);
    }
    return YES;
}
static void type(pid_t pid, NSString *value) {
    for (NSUInteger i = 0; i < value.length; i++) {
        UniChar character = [value characterAtIndex:i];
        for (int down = 1; down >= 0; down--) {
            CGEventRef event = CGEventCreateKeyboardEvent(NULL, 0, down);
            CGEventSetFlags(event, 0); CGEventKeyboardSetUnicodeString(event, 1, &character);
            CGEventPostToPid(pid, event); CFRelease(event);
        }
        usleep(5000);
    }
}
static int fail(NSString *message) { fprintf(stderr, "%s\n", message.UTF8String); return 2; }
int main(int argc, const char **argv) { @autoreleasepool {
    if (argc < 3 || !guest() || !env(@"ZWAI_NATIVE_CASE_ID").length) return fail(@"guest case required");
    NSString *action = @(argv[1]); pid_t pid = atoi(argv[2]);
    if ([action isEqual:@"identity"]) {
        NSDictionary *row = identity(pid); if (!row) return fail(@"process identity unavailable");
        puts([[NSString alloc] initWithData:[NSJSONSerialization dataWithJSONObject:row options:0 error:nil]
                                   encoding:NSUTF8StringEncoding].UTF8String); return 0;
    }
    if (!bound(pid)) return fail([NSString stringWithFormat:@"case executable identity refused: target=%@ expected=%@", identity(pid), env(@"ZWAI_NATIVE_APP")]);
    if (!AXIsProcessTrusted()) return fail(@"guest accessibility is unavailable to this helper");
    NSString *value = argc > 3 ? @(argv[3]) : @"";
    if ([action isEqual:@"image"]) {
        NSString *root = [env(@"ZWAI_NATIVE_EVIDENCE") stringByAppendingString:@"/"];
        if (![[value stringByStandardizingPath] hasPrefix:root]) return fail(@"image evidence path refused");
        NSBitmapImageRep *image = [NSBitmapImageRep imageRepWithData:[NSData dataWithContentsOfFile:value]];
        if (!image || image.pixelsWide < 100 || image.pixelsHigh < 100) return fail(@"window image is not decodable");
        NSMutableDictionary *buckets = [NSMutableDictionary dictionary]; NSUInteger samples = 0, dominant = 0;
        for (NSInteger y = 40; y < image.pixelsHigh; y += 5) for (NSInteger x = 0; x < image.pixelsWide; x += 5) {
            NSColor *color = [[image colorAtX:x y:y] colorUsingColorSpace:NSColorSpace.deviceRGBColorSpace];
            NSString *bucket = [NSString stringWithFormat:@"%d,%d,%d", (int)(color.redComponent*15),
                                (int)(color.greenComponent*15), (int)(color.blueComponent*15)];
            NSUInteger n = [buckets[bucket] unsignedIntegerValue] + 1; buckets[bucket] = @(n);
            dominant = MAX(dominant, n); samples++;
        }
        NSDictionary *row = @{@"width": @(image.pixelsWide), @"height": @(image.pixelsHigh),
                              @"buckets": @(buckets.count), @"dominant_fraction": @((double)dominant/samples)};
        puts([[NSString alloc] initWithData:[NSJSONSerialization dataWithJSONObject:row options:0 error:nil]
                                   encoding:NSUTF8StringEncoding].UTF8String); return 0;
    }
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    id windows = attr(app, kAXWindowsAttribute);
    if ([action isEqual:@"activate"]) {
        if (![windows isKindOfClass:NSArray.class] || [windows count] != 1)
            { CFRelease(app); return fail(@"one private case window required for activation"); }
        NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        BOOL requested = [running activateWithOptions:0];
        AXError raised = AXUIElementPerformAction((__bridge AXUIElementRef)windows[0], kAXRaiseAction);
        usleep(200000);
        printf("{\"activated\":%s,\"active\":%s,\"pid\":%d,\"frontmost_pid\":%d,\"raise_error\":%d}\n",
               requested ? "true" : "false", running.active ? "true" : "false", pid,
               NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier, raised);
        CFRelease(app); return 0;
    }
    if ([action isEqual:@"quit"]) {
        NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        BOOL requested = [running terminate];
        printf("{\"quit_requested\":%s,\"pid\":%d}\n", requested ? "true" : "false", pid);
        CFRelease(app); return requested ? 0 : fail(@"native Quit request refused");
    }
    if ([action isEqual:@"dialogs"]) {
        NSMutableArray *rows = [NSMutableArray array];
        NSArray *targets = sheets(windows);
        if (!targets.count) targets = windows;
        if ([targets isKindOfClass:NSArray.class]) for (id item in targets)
            describe((__bridge AXUIElementRef)item, rows, 0);
        NSMutableArray *focus = [NSMutableArray array];
        id focused = attr(app, kAXFocusedUIElementAttribute);
        if (focused) describe((__bridge AXUIElementRef)focused, focus, 8);
        printf("%s\n", [[NSString alloc] initWithData:[NSJSONSerialization dataWithJSONObject:@{@"case_id": env(@"ZWAI_NATIVE_CASE_ID"), @"pid": @(pid), @"rows": rows, @"focused": focus} options:0 error:nil] encoding:NSUTF8StringEncoding].UTF8String);
        CFRelease(app); return 0;
    }
    if ([action isEqual:@"error-dialog"] || [action isEqual:@"dismiss-error"]) {
        NSMutableArray *alerts = [NSMutableArray arrayWithArray:sheets(windows)];
        if ([windows isKindOfClass:NSArray.class]) for (id item in windows) {
            AXUIElementRef element = (__bridge AXUIElementRef)item;
            if ([string(attr(element, kAXSubroleAttribute)) isEqual:@"AXDialog"]) [alerts addObject:item];
        }
        NSMutableString *text = [NSMutableString string]; NSMutableArray *areas = [NSMutableArray array];
        NSMutableArray *buttons = [NSMutableArray array]; NSUInteger count = 0; BOOL web = NO;
        for (id alert in alerts) collect((__bridge AXUIElementRef)alert, areas, buttons, text, &count, &web);
        if (![text containsString:@"已停止自动重启"]) { CFRelease(app); return fail(@"bounded recovery native error is not visible"); }
        if ([action isEqual:@"dismiss-error"]) {
            if (buttons.count != 1 || AXUIElementPerformAction((__bridge AXUIElementRef)buttons[0], kAXPressAction))
                { CFRelease(app); return fail(@"recognized native error has no unique dismiss action"); }
        }
        NSNumber *windowID = nil;
        for (NSDictionary *window in CFBridgingRelease(CGWindowListCopyWindowInfo(kCGWindowListOptionAll, kCGNullWindowID))) {
            if ([window[(__bridge id)kCGWindowOwnerPID] intValue] == pid && [window[(__bridge id)kCGWindowLayer] intValue] == 0)
                { windowID = window[(__bridge id)kCGWindowNumber]; break; }
        }
        if (!windowID) { CFRelease(app); return fail(@"native error screenshot window unavailable"); }
        printf("%s\n", [[NSString alloc] initWithData:[NSJSONSerialization dataWithJSONObject:
            @{@"error_dialog": @YES, @"pid": @(pid), @"case_id": env(@"ZWAI_NATIVE_CASE_ID"), @"text": text, @"window_id": windowID}
            options:0 error:nil] encoding:NSUTF8StringEncoding].UTF8String);
        CFRelease(app); return 0;
    }
    if ([action isEqual:@"choose-file"]) {
        NSString *root = [[env(@"ZWAI_NATIVE_WORKSPACE") stringByResolvingSymlinksInPath] stringByAppendingString:@"/"];
        BOOL panel = NO;
        pid_t panelPID = pid;
        if ([windows isKindOfClass:NSArray.class]) for (id item in windows) {
            AXUIElementRef element = (__bridge AXUIElementRef)item;
            id sheets = attr(element, CFSTR("AXSheets"));
            if ([sheets isKindOfClass:NSArray.class] && [sheets count]) panel = YES;
            id children = attr(element, kAXChildrenAttribute);
            if ([children isKindOfClass:NSArray.class]) for (id child in children)
                if ([string(attr((__bridge AXUIElementRef)child, kAXRoleAttribute)) isEqual:@"AXSheet"]) {
                    panel = YES;
                    AXUIElementGetPid((__bridge AXUIElementRef)child, &panelPID);
                }
            if ([string(attr(element, kAXSubroleAttribute)) isEqual:@"AXDialog"]) panel = YES;
        }
        if (!panel || ![[value stringByResolvingSymlinksInPath] hasPrefix:root])
            { CFRelease(app); return fail(@"native attachment panel or case path refused"); }
        if (panelPID != pid && ![identity(panelPID)[@"executable"] containsString:@"openAndSavePanelService"])
            { CFRelease(app); return fail(@"case-owned attachment sheet has an unexpected input process"); }
        NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        [running activateWithOptions:0];
        for (NSUInteger attempt = 0; attempt < 20; attempt++) {
            id focused = attr(app, kAXFocusedUIElementAttribute);
            NSString *role = focused ? string(attr((__bridge AXUIElementRef)focused, kAXRoleAttribute)) : @"";
            if ([@[@"AXList", @"AXOutline", @"AXBrowser", @"AXSheet"] containsObject:role]) break;
            if (attempt == 19) { CFRelease(app); return fail(@"native attachment panel has not received input focus"); }
            usleep(50000);
        }
        if (!nestedSheet(app) && !focusedKey(pid, 5, kCGEventFlagMaskCommand | kCGEventFlagMaskShift))
            { CFRelease(app); return fail(@"private case is not the active guest input target"); }
        NSMutableArray *fields = [NSMutableArray array];
        for (NSUInteger attempt = 0; attempt < 10 && !fields.count; attempt++) {
            usleep(100000);
            id go = nestedSheet(app);
            if (go) editable((__bridge AXUIElementRef)go, fields, 0);
        }
        // The actual path control must appear before any pathname is entered.
        // A sent shortcut alone is never a successful native selection.
        if (fields.count != 1) { CFRelease(app); return fail([NSString stringWithFormat:@"Go to Folder requires one actual path field; found %lu active=%d frontmost=%d panel=%d", fields.count, running.active, NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier, panelPID]); }
        AXUIElementRef field = (__bridge AXUIElementRef)fields[0];
        if (AXUIElementSetAttributeValue(field, kAXValueAttribute, (__bridge CFStringRef)value)
            || ![string(attr(field, kAXValueAttribute)) isEqual:value])
            { CFRelease(app); return fail(@"native Go to Folder did not retain the exact case path"); }
        if (AXUIElementPerformAction(field, kAXConfirmAction))
            { CFRelease(app); return fail(@"native Go to Folder has no confirm action"); }
        NSMutableArray *matches = [NSMutableArray array];
        for (NSUInteger attempt = 0; attempt < 10 && !matches.count; attempt++) {
            id go = nestedSheet(app);
            if (go) openablePath((__bridge AXUIElementRef)go, value.lastPathComponent, matches, 0);
            if (!matches.count) usleep(100000);
        }
        if (matches.count != 1 || AXUIElementPerformAction((__bridge AXUIElementRef)matches[0], CFSTR("AXOpen")))
            { CFRelease(app); return fail(@"native Go to Folder has no unique exact-file AXOpen action"); }
        if (AXUIElementSetAttributeValue(field, kAXFocusedAttribute, kCFBooleanTrue)
            || !focusedKey(pid, 36, 0))
            { CFRelease(app); return fail(@"native exact-path confirmation lost its guest input target"); }
        for (NSUInteger attempt = 0; attempt < 15 && nestedSheet(app); attempt++) usleep(100000);
        if (nestedSheet(app)) { CFRelease(app); return fail(@"native Go to Folder did not commit the exact path"); }
        NSMutableArray *areas = [NSMutableArray array], *buttons = [NSMutableArray array];
        NSMutableString *panelText = [NSMutableString string]; NSUInteger count = 0; BOOL web = NO;
        for (id sheet in sheets(attr(app, kAXWindowsAttribute))) collect((__bridge AXUIElementRef)sheet, areas, buttons, panelText, &count, &web);
        if (![panelText containsString:value.lastPathComponent]) { CFRelease(app); return fail(@"native panel did not reach the exact selected filename"); }
        NSMutableArray *opens = [NSMutableArray array];
        for (id button in buttons) if ([@[@"Open", @"打开"] containsObject:string(attr((__bridge AXUIElementRef)button, kAXTitleAttribute))]) [opens addObject:button];
        if (opens.count != 1 || AXUIElementPerformAction((__bridge AXUIElementRef)opens[0], kAXPressAction))
            { CFRelease(app); return fail(@"native selected file has no unique Open action"); }
        for (NSUInteger attempt = 0; attempt < 20 && sheets(attr(app, kAXWindowsAttribute)).count; attempt++) usleep(100000);
        if (sheets(attr(app, kAXWindowsAttribute)).count) { CFRelease(app); return fail(@"native Open did not close its attachment panel"); }
        printf("{\"chosen\":true,\"panel_pid\":%d,\"active\":%s,\"frontmost_pid\":%d}\n", panelPID,
               running.active ? "true" : "false", NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier);
        CFRelease(app); return 0;
    }
    if (![windows isKindOfClass:NSArray.class] || [windows count] != 1) { CFRelease(app); return fail(@"one case window required"); }
    AXUIElementRef window = (__bridge AXUIElementRef)windows[0];
    NSMutableArray *areas = [NSMutableArray array], *buttons = [NSMutableArray array];
    NSMutableString *text = [NSMutableString string]; NSUInteger count = 0; BOOL web = NO;
    collect(window, areas, buttons, text, &count, &web);
    NSMutableArray *values = [NSMutableArray array];
    for (id area in areas) [values addObject:string(attr((__bridge AXUIElementRef)area, kAXValueAttribute))];
    NSMutableArray *native = [NSMutableArray array];
    CGPoint position = CGPointZero; CGSize size = CGSizeZero;
    id axPosition = attr(window, kAXPositionAttribute), axSize = attr(window, kAXSizeAttribute);
    BOOL frame = axPosition && axSize && AXValueGetValue((__bridge AXValueRef)axPosition, kAXValueCGPointType, &position)
        && AXValueGetValue((__bridge AXValueRef)axSize, kAXValueCGSizeType, &size);
    for (NSDictionary *row in CFBridgingRelease(CGWindowListCopyWindowInfo(kCGWindowListOptionAll, kCGNullWindowID))) {
        CGRect bounds = CGRectZero;
        BOOL geometry = CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)row[(__bridge id)kCGWindowBounds], &bounds);
        if ([row[(__bridge id)kCGWindowOwnerPID] intValue] == pid && [row[(__bridge id)kCGWindowLayer] intValue] == 0
            && geometry && bounds.size.width >= 100 && bounds.size.height >= 100
            && [row[(__bridge id)kCGWindowAlpha] doubleValue] > 0
            && (!frame || (fabs(bounds.origin.x-position.x)<2 && fabs(bounds.origin.y-position.y)<2
                && fabs(bounds.size.width-size.width)<2 && fabs(bounds.size.height-size.height)<2)))
            [native addObject:row];
    }
    if (native.count != 1) { CFRelease(app); return fail([NSString stringWithFormat:@"unique case screenshot window unavailable: candidates=%@ ax_frame=%@", native, NSStringFromRect(NSMakeRect(position.x,position.y,size.width,size.height))]); }
    NSMutableDictionary *result = [@{@"case_id": env(@"ZWAI_NATIVE_CASE_ID"), @"pid": @(pid),
        @"has_web_area": @(web), @"textareas": @(areas.count), @"values": values, @"text": text,
        @"window_id": native[0][(__bridge id)kCGWindowNumber], @"identity": identity(pid)} mutableCopy];
    if ([action isEqual:@"fill"] || [action isEqual:@"press"] || [action isEqual:@"select-thread"]) {
        NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        [running activateWithOptions:0];
        AXUIElementPerformAction(window, kAXRaiseAction);
    }
    if ([action isEqual:@"fill"]) {
        if (areas.count != 1 || value.length > 2048) { CFRelease(app); return fail(@"one bounded composer required"); }
        if (AXUIElementSetAttributeValue((__bridge AXUIElementRef)areas[0], kAXFocusedAttribute, kCFBooleanTrue))
            { CFRelease(app); return fail(@"cannot focus case composer"); }
        if (AXUIElementSetAttributeValue((__bridge AXUIElementRef)areas[0], kAXValueAttribute, (__bridge CFStringRef)value) == kAXErrorSuccess) {
            result[@"filled"] = @YES; result[@"input_method"] = @"accessibility_value";
            puts([[NSString alloc] initWithData:[NSJSONSerialization dataWithJSONObject:result options:0 error:nil] encoding:NSUTF8StringEncoding].UTF8String);
            CFRelease(app); return 0;
        }
        // AX focus acknowledgement precedes WebKit's DOM focus event. Sending
        // Command-A immediately can select the page instead of its composer.
        usleep(100000);
        key(pid, 0, kCGEventFlagMaskCommand);
        usleep(50000);
        type(pid, value);
        result[@"filled"] = @YES;
    } else if ([action isEqual:@"press"] || [action isEqual:@"select-thread"]) {
        BOOL thread = [action isEqual:@"select-thread"];
        if (thread && (![value hasSuffix:env(@"ZWAI_NATIVE_CASE_ID")] || value.length > 200))
            { CFRelease(app); return fail(@"native conversation selection requires its exact case title"); }
        NSMutableArray *matches = [NSMutableArray array];
        for (id button in buttons) {
            AXUIElementRef element = (__bridge AXUIElementRef)button;
            NSString *suffix = [@" " stringByAppendingString:value];
            if ([string(attr(element, kAXTitleAttribute)) isEqual:value]
                || [string(attr(element, kAXDescriptionAttribute)) isEqual:value]
                || (thread && (exactText(element, value, 0) || [string(attr(element, kAXTitleAttribute)) hasSuffix:suffix]
                    || [string(attr(element, kAXDescriptionAttribute)) hasSuffix:suffix]))) [matches addObject:button];
        }
        if (matches.count != 1 || AXUIElementPerformAction((__bridge AXUIElementRef)matches[0], kAXPressAction))
            { CFRelease(app); return fail([NSString stringWithFormat:@"native action %@ requires one enabled button; matches=%lu", value, matches.count]); }
        result[@"pressed"] = value;
    } else if ([action isEqual:@"paste-file"]) {
        NSString *root = [[env(@"ZWAI_NATIVE_WORKSPACE") stringByResolvingSymlinksInPath] stringByAppendingString:@"/"];
        if (areas.count != 1 || ![[value stringByResolvingSymlinksInPath] hasPrefix:root])
            { CFRelease(app); return fail(@"case attachment path refused"); }
        NSPasteboard *pasteboard = NSPasteboard.generalPasteboard; [pasteboard clearContents];
        if ([value.pathExtension.lowercaseString isEqual:@"png"])
            [pasteboard setData:[NSData dataWithContentsOfFile:value] forType:NSPasteboardTypePNG];
        else [pasteboard writeObjects:@[[NSURL fileURLWithPath:value]]];
        if (AXUIElementSetAttributeValue((__bridge AXUIElementRef)areas[0], kAXFocusedAttribute, kCFBooleanTrue))
            { CFRelease(app); return fail(@"native image paste cannot focus its case composer"); }
        BOOL focused = NO;
        for (NSUInteger attempt = 0; attempt < 20; attempt++) {
            id current = attr(app, kAXFocusedUIElementAttribute);
            if (current && CFEqual((__bridge CFTypeRef)current, (__bridge CFTypeRef)areas[0])) { focused = YES; break; }
            usleep(25000);
        }
        if (!focused || !focusedKey(pid, 9, kCGEventFlagMaskCommand))
            { CFRelease(app); return fail(@"native image paste lost its exact guest composer target"); }
        result[@"pasted"] = @YES;
    } else if (![action isEqual:@"snapshot"]) { CFRelease(app); return fail(@"unsupported test action"); }
    puts([[NSString alloc] initWithData:[NSJSONSerialization dataWithJSONObject:result options:0 error:nil]
                               encoding:NSUTF8StringEncoding].UTF8String);
    CFRelease(app); return 0;
} }
