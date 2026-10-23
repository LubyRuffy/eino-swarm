// CI-only native UI helper. Never run outside a private VM case.
#import <AppKit/AppKit.h>
#import <CommonCrypto/CommonDigest.h>
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
static BOOL validPasteboardPayload(NSData *source, BOOL stored, NSData *actual,
                                  NSInteger ownedCount, NSInteger observedCount) {
    return source.length > 0 && stored && [actual isEqualToData:source]
        && ownedCount >= 0 && observedCount == ownedCount;
}
static BOOL matchesPasteMenu(NSString *role, NSString *title, BOOL edit) {
    return [role isEqual:edit ? @"AXMenuBarItem" : @"AXMenuItem"]
        && [(edit ? @[@"Edit", @"编辑"] : @[@"Paste", @"粘贴"]) containsObject:title ?: @""];
}
static BOOL validPasteMenuTarget(NSUInteger matches, pid_t owner, pid_t target,
                                id enabled, pid_t frontmost) {
    return target > 0 && matches == 1 && owner == target && frontmost == target
        && [enabled isKindOfClass:NSNumber.class] && [enabled boolValue];
}
static NSString *pasteDigest(NSData *data) {
    if (!data) return @"unavailable";
    unsigned char bytes[CC_SHA256_DIGEST_LENGTH];
    CC_SHA256(data.bytes, (CC_LONG)data.length, bytes);
    NSMutableString *result = [NSMutableString string];
    for (NSUInteger i = 0; i < sizeof(bytes); i++) [result appendFormat:@"%02x", bytes[i]];
    return result;
}
static BOOL savePasteReceipt(NSDictionary *receipt) {
    NSString *path = [env(@"ZWAI_NATIVE_EVIDENCE") stringByAppendingPathComponent:@"paste-transport-observation.json"];
    NSData *data = [NSJSONSerialization dataWithJSONObject:receipt options:NSJSONWritingPrettyPrinted error:nil];
    return data && [data writeToFile:path options:NSDataWritingAtomic error:nil];
}
static BOOL findPasteMenu(AXUIElementRef item, NSMutableArray *matches, NSUInteger depth, NSUInteger *visited) {
    if (depth > 5 || ++*visited > 1000) return NO;
    if (matches.count > 1) return YES;
    if (matchesPasteMenu(string(attr(item, kAXRoleAttribute)), string(attr(item, kAXTitleAttribute)), NO))
        [matches addObject:(__bridge id)item];
    id children = attr(item, kAXChildrenAttribute);
    if ([children isKindOfClass:NSArray.class]) for (id child in children)
        if (!findPasteMenu((__bridge AXUIElementRef)child, matches, depth+1, visited)) return NO;
    return YES;
}
static NSDictionary *requestNativePaste(AXUIElementRef app, pid_t pid, NSPasteboard *pasteboard,
                                       NSData *source, NSInteger ownedCount) {
    NSMutableArray *edits = [NSMutableArray array], *pastes = [NSMutableArray array];
    id bar = attr(app, kAXMenuBarAttribute);
    id children = bar ? attr((__bridge AXUIElementRef)bar, kAXChildrenAttribute) : nil;
    if ([children isKindOfClass:NSArray.class]) for (id child in children)
        if (matchesPasteMenu(string(attr((__bridge AXUIElementRef)child, kAXRoleAttribute)),
                             string(attr((__bridge AXUIElementRef)child, kAXTitleAttribute)), YES)) [edits addObject:child];
    pid_t editOwner = 0, pasteOwner = 0;
    id editEnabled = edits.count == 1 ? attr((__bridge AXUIElementRef)edits[0], kAXEnabledAttribute) : nil;
    if (edits.count == 1) AXUIElementGetPid((__bridge AXUIElementRef)edits[0], &editOwner);
    AXError opened = kAXErrorFailure, pressed = kAXErrorFailure;
    pid_t editFrontmost = NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier;
    BOOL complete = NO; NSUInteger visited = 0;
    if (validPasteMenuTarget(edits.count, editOwner, pid, editEnabled, editFrontmost)) {
        opened = AXUIElementPerformAction((__bridge AXUIElementRef)edits[0], kAXPressAction);
        if (opened == kAXErrorSuccess) complete = findPasteMenu((__bridge AXUIElementRef)edits[0], pastes, 0, &visited);
    }
    id pasteEnabled = pastes.count == 1 ? attr((__bridge AXUIElementRef)pastes[0], kAXEnabledAttribute) : nil;
    if (pastes.count == 1) AXUIElementGetPid((__bridge AXUIElementRef)pastes[0], &pasteOwner);
    NSData *actual = [pasteboard dataForType:NSPasteboardTypePNG];
    NSInteger observedCount = pasteboard.changeCount;
    BOOL unchanged = validPasteboardPayload(source, YES, actual, ownedCount, observedCount);
    pid_t pasteFrontmost = NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier;
    if (opened == kAXErrorSuccess && complete && unchanged
        && validPasteMenuTarget(pastes.count, pasteOwner, pid, pasteEnabled, pasteFrontmost))
        pressed = AXUIElementPerformAction((__bridge AXUIElementRef)pastes[0], kAXPressAction);
    return @{@"edit_count": @(edits.count), @"edit_owner_pid": @(editOwner), @"edit_enabled": editEnabled ?: NSNull.null,
             @"edit_open_error": @(opened), @"paste_count": @(pastes.count), @"paste_owner_pid": @(pasteOwner),
             @"paste_enabled": pasteEnabled ?: NSNull.null, @"paste_press_error": @(pressed),
             @"edit_frontmost_pid": @(editFrontmost), @"paste_frontmost_pid": @(pasteFrontmost),
             @"menu_search_complete": @(complete), @"clipboard_unchanged_before_paste": @(unchanged),
             @"change_count_before_paste": @(observedCount)};
}
static int fail(NSString *message) { fprintf(stderr, "%s\n", message.UTF8String); return 2; }
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

static AXError filePanelAttribute(AXUIElementRef item, CFStringRef key, id *value) {
    CFTypeRef raw = NULL;
    AXError status = AXUIElementCopyAttributeValue(item, key, &raw);
    *value = raw ? CFBridgingRelease(raw) : nil;
    return status;
}
static BOOL filePanelText(AXUIElementRef item, CFStringRef key, NSString **text, NSString **failure) {
    id raw = nil;
    AXError status = filePanelAttribute(item, key, &raw);
    if (status == kAXErrorAttributeUnsupported || status == kAXErrorNoValue) { *text = @""; return YES; }
    if (status != kAXErrorSuccess) {
        *failure = [NSString stringWithFormat:@"native panel %@ read failed: %d", (__bridge NSString *)key, status];
        return NO;
    }
    *text = string(raw);
    return YES;
}
static BOOL filePanelVisit(AXUIElementRef item, NSUInteger *visits, NSString **failure) {
    if (++*visits > 5000) { *failure = @"native panel exceeded 5000 visits"; return NO; }
    if (!item || CFGetTypeID(item) != AXUIElementGetTypeID()) {
        *failure = @"native panel node is not an AX element"; return NO;
    }
    return YES;
}
static BOOL scanFilePanel(AXUIElementRef item, NSString *name, NSMutableArray *opens, BOOL *found,
                          CFMutableSetRef seen, NSUInteger *visits, NSUInteger depth, NSString **failure) {
    if (!filePanelVisit(item, visits, failure)) return NO;
    if (CFSetContainsValue(seen, item)) return YES;
    if (depth > 45) { *failure = @"native panel exceeded depth 45"; return NO; }
    CFSetAddValue(seen, item);
    id role = nil;
    AXError status = filePanelAttribute(item, kAXRoleAttribute, &role);
    if (status != kAXErrorSuccess || ![role isKindOfClass:NSString.class] || ![role length]) {
        *failure = [NSString stringWithFormat:@"native panel role is unavailable or malformed: %d", status]; return NO;
    }
    NSString *title = nil;
    if ([role isEqual:@"AXButton"]) {
        if (!filePanelText(item, kAXTitleAttribute, &title, failure)) return NO;
        if ([@[@"Open", @"打开"] containsObject:title]) [opens addObject:(__bridge id)item];
    }
    if (!*found) for (NSString *key in @[@"AXTitle", @"AXDescription", @"AXValue"]) {
        NSString *text = title && [key isEqual:@"AXTitle"] ? title : nil;
        if (!text && !filePanelText(item, (__bridge CFStringRef)key, &text, failure)) return NO;
        if ([text containsString:name]) { *found = YES; break; }
    }
    id children = nil;
    status = filePanelAttribute(item, kAXChildrenAttribute, &children);
    if (status == kAXErrorAttributeUnsupported || status == kAXErrorNoValue) return YES;
    if (status != kAXErrorSuccess || ![children isKindOfClass:NSArray.class]) {
        *failure = [NSString stringWithFormat:@"native panel children are unreadable or malformed: %d", status]; return NO;
    }
    for (id child in children)
        if (!scanFilePanel((__bridge AXUIElementRef)child, name, opens, found, seen, visits, depth+1, failure)) return NO;
    return YES;
}

static BOOL filePanelRoots(AXUIElementRef app, NSMutableArray *roots, NSUInteger *visits, NSString **failure) {
    if (!filePanelVisit(app, visits, failure)) return NO;
    id windows = nil;
    AXError status = filePanelAttribute(app, kAXWindowsAttribute, &windows);
    if (status != kAXErrorSuccess || ![windows isKindOfClass:NSArray.class]) {
        *failure = [NSString stringWithFormat:@"native panel windows are unreadable or malformed: %d", status]; return NO;
    }
    for (id window in windows) {
        AXUIElementRef element = (__bridge AXUIElementRef)window;
        if (!filePanelVisit(element, visits, failure)) return NO;
        id children = nil;
        status = filePanelAttribute(element, kAXChildrenAttribute, &children);
        if (status == kAXErrorAttributeUnsupported || status == kAXErrorNoValue) continue;
        if (status != kAXErrorSuccess || ![children isKindOfClass:NSArray.class]) {
            *failure = [NSString stringWithFormat:@"native panel window children are unreadable or malformed: %d", status]; return NO;
        }
        for (id child in children) {
            AXUIElementRef candidate = (__bridge AXUIElementRef)child;
            if (!filePanelVisit(candidate, visits, failure)) return NO;
            id role = nil;
            status = filePanelAttribute(candidate, kAXRoleAttribute, &role);
            if (status != kAXErrorSuccess || ![role isKindOfClass:NSString.class] || ![role length]) {
                *failure = [NSString stringWithFormat:@"native panel root role is unavailable or malformed: %d", status]; return NO;
            }
            if ([role isEqual:@"AXSheet"]) [roots addObject:child];
        }
    }
    return YES;
}

// BEGIN PRIVATE CHOOSE TRACE
static void chooseTrace(NSString *stage, NSDictionary *identityRow) {
    static NSUInteger emitted = 0;
    if (emitted++ >= 64) return;
    NSMutableDictionary *row = [@{@"stage":stage, @"monotonic":@(NSProcessInfo.processInfo.systemUptime)} mutableCopy];
    if (identityRow) row[@"identity"] = identityRow;
    NSData *data = [NSJSONSerialization dataWithJSONObject:row options:0 error:nil];
    NSString *encoded = data ? [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding] : @"{}";
    fprintf(stderr, "ZWAI_CHOOSE %s\n", encoded.UTF8String);
    fflush(stderr);
}
static void chooseExit(void) { @autoreleasepool { chooseTrace(@"process.exit.after_pool", nil); } }
// END PRIVATE CHOOSE TRACE

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
    if ([action isEqual:@"choose-file"]) { chooseTrace(@"entry", identity(getpid())); atexit(chooseExit); }
    if ([action isEqual:@"choose-file"]) chooseTrace(@"windows.before", nil);
    id windows = attr(app, kAXWindowsAttribute);
    if ([action isEqual:@"choose-file"]) chooseTrace(@"windows.after", nil);
    if ([action isEqual:@"activate"]) {
        if (![windows isKindOfClass:NSArray.class] || [windows count] != 1)
            { CFRelease(app); return fail(@"one private case window required for activation"); }
        NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        double activationStarted = NSProcessInfo.processInfo.systemUptime;
        double activationDeadline = activationStarted + 2;
        BOOL requested = [running activateWithOptions:0];
        AXError raised = AXUIElementPerformAction((__bridge AXUIElementRef)windows[0], kAXRaiseAction);
        BOOL activationReady = waitForActivation(running, activationDeadline);
        double elapsed = NSProcessInfo.processInfo.systemUptime - activationStarted;
        BOOL active = running.active;
        pid_t frontmostPID = NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier;
        printf("{\"activated\":%s,\"active\":%s,\"pid\":%d,\"frontmost_pid\":%d,\"raise_error\":%d,\"activation_ready\":%s,\"activation_elapsed_seconds\":%.6f}\n",
               requested ? "true" : "false", active ? "true" : "false", pid, frontmostPID, raised,
               activationReady ? "true" : "false", elapsed);
        if (!activationReady || !active)
            fprintf(stderr, "native activation not confirmed: activated=%s active=%s pid=%d frontmost_pid=%d raise_error=%d elapsed=%.6f\n",
                    requested ? "true" : "false", active ? "true" : "false", pid, frontmostPID, raised, elapsed);
        CFRelease(app); return activationReady && active ? 0 : 2;
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
        chooseTrace(@"panel.scan.before", nil);
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
        chooseTrace(@"panel.scan.after", nil);
        if (!panel || ![[value stringByResolvingSymlinksInPath] hasPrefix:root])
            { CFRelease(app); return fail(@"native attachment panel or case path refused"); }
        if (panelPID != pid && ![identity(panelPID)[@"executable"] containsString:@"openAndSavePanelService"])
            { CFRelease(app); return fail(@"case-owned attachment sheet has an unexpected input process"); }
        NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        chooseTrace(@"activate.before", nil);
        [running activateWithOptions:0];
        chooseTrace(@"activate.after", nil);
        chooseTrace(@"focus.loop.before", nil);
        for (NSUInteger attempt = 0; attempt < 20; attempt++) {
            id focused = attr(app, kAXFocusedUIElementAttribute);
            NSString *role = focused ? string(attr((__bridge AXUIElementRef)focused, kAXRoleAttribute)) : @"";
            if ([@[@"AXList", @"AXOutline", @"AXBrowser", @"AXSheet"] containsObject:role]) break;
            if (attempt == 19) { CFRelease(app); return fail(@"native attachment panel has not received input focus"); }
            usleep(50000);
        }
        chooseTrace(@"focus.loop.after", nil);
        chooseTrace(@"goto.shortcut.before", nil);
        if (!nestedSheet(app) && !focusedKey(pid, 5, kCGEventFlagMaskCommand | kCGEventFlagMaskShift))
            { CFRelease(app); return fail(@"private case is not the active guest input target"); }
        chooseTrace(@"goto.shortcut.after", nil);
        NSMutableArray *fields = [NSMutableArray array];
        chooseTrace(@"goto.fields.loop.before", nil);
        for (NSUInteger attempt = 0; attempt < 10 && !fields.count; attempt++) {
            usleep(100000);
            id go = nestedSheet(app);
            if (go) editable((__bridge AXUIElementRef)go, fields, 0);
        }
        chooseTrace(@"goto.fields.loop.after", nil);
        // The actual path control must appear before any pathname is entered.
        // A sent shortcut alone is never a successful native selection.
        if (fields.count != 1) { CFRelease(app); return fail([NSString stringWithFormat:@"Go to Folder requires one actual path field; found %lu active=%d frontmost=%d panel=%d", fields.count, running.active, NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier, panelPID]); }
        AXUIElementRef field = (__bridge AXUIElementRef)fields[0];
        chooseTrace(@"path.set.before", nil);
        if (AXUIElementSetAttributeValue(field, kAXValueAttribute, (__bridge CFStringRef)value)
            || ![string(attr(field, kAXValueAttribute)) isEqual:value])
            { CFRelease(app); return fail(@"native Go to Folder did not retain the exact case path"); }
        chooseTrace(@"path.set.after", nil);
        chooseTrace(@"path.confirm.before", nil);
        if (AXUIElementPerformAction(field, kAXConfirmAction))
            { CFRelease(app); return fail(@"native Go to Folder has no confirm action"); }
        chooseTrace(@"path.confirm.after", nil);
        NSMutableArray *matches = [NSMutableArray array];
        chooseTrace(@"path.matches.loop.before", nil);
        for (NSUInteger attempt = 0; attempt < 10 && !matches.count; attempt++) {
            id go = nestedSheet(app);
            if (go) openablePath((__bridge AXUIElementRef)go, value.lastPathComponent, matches, 0);
            if (!matches.count) usleep(100000);
        }
        chooseTrace(@"path.matches.loop.after", nil);
        chooseTrace(@"path.open.before", nil);
        if (matches.count != 1 || AXUIElementPerformAction((__bridge AXUIElementRef)matches[0], CFSTR("AXOpen")))
            { CFRelease(app); return fail(@"native Go to Folder has no unique exact-file AXOpen action"); }
        chooseTrace(@"path.open.after", nil);
        chooseTrace(@"path.commit.before", nil);
        if (AXUIElementSetAttributeValue(field, kAXFocusedAttribute, kCFBooleanTrue)
            || !focusedKey(pid, 36, 0))
            { CFRelease(app); return fail(@"native exact-path confirmation lost its guest input target"); }
        chooseTrace(@"path.commit.after", nil);
        chooseTrace(@"goto.close.loop.before", nil);
        for (NSUInteger attempt = 0; attempt < 15 && nestedSheet(app); attempt++) usleep(100000);
        if (nestedSheet(app)) { CFRelease(app); return fail(@"native Go to Folder did not commit the exact path"); }
        chooseTrace(@"goto.close.loop.after", nil);
        chooseTrace(@"panel.collect.before", nil);
        NSMutableArray *opens = [NSMutableArray array];
        BOOL filenameFound = NO, complete = YES; NSUInteger visits = 0; NSString *scanFailure = nil;
        NSMutableArray *roots = [NSMutableArray array];
        complete = filePanelRoots(app, roots, &visits, &scanFailure);
        CFMutableSetRef seen = CFSetCreateMutable(kCFAllocatorDefault, 0, &kCFTypeSetCallBacks);
        if (complete) for (id sheet in roots)
            if (!scanFilePanel((__bridge AXUIElementRef)sheet, value.lastPathComponent, opens,
                               &filenameFound, seen, &visits, 0, &scanFailure)) { complete = NO; break; }
        CFRelease(seen);
        chooseTrace(@"panel.collect.after", nil);
        if (!complete) { CFRelease(app); return fail(scanFailure); }
        if (!filenameFound) { CFRelease(app); return fail(@"native panel did not reach the exact selected filename"); }
        chooseTrace(@"panel.open.before", nil);
        if (opens.count != 1 || AXUIElementPerformAction((__bridge AXUIElementRef)opens[0], kAXPressAction))
            { CFRelease(app); return fail(@"native selected file has no unique Open action"); }
        chooseTrace(@"panel.open.after", nil);
        chooseTrace(@"panel.close.loop.before", nil);
        for (NSUInteger attempt = 0; attempt < 20 && sheets(attr(app, kAXWindowsAttribute)).count; attempt++) usleep(100000);
        if (sheets(attr(app, kAXWindowsAttribute)).count) { CFRelease(app); return fail(@"native Open did not close its attachment panel"); }
        chooseTrace(@"panel.close.loop.after", nil);
        chooseTrace(@"chosen.stdout.before", nil);
        printf("{\"chosen\":true,\"panel_pid\":%d,\"active\":%s,\"frontmost_pid\":%d}\n", panelPID,
               running.active ? "true" : "false", NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier);
        fflush(stdout);
        chooseTrace(@"chosen.stdout.after", nil);
        chooseTrace(@"release.before", nil);
        CFRelease(app);
        chooseTrace(@"release.after", nil);
        chooseTrace(@"return.before_pool_drain", nil);
        return 0;
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
        if (areas.count != 1 || ![[value stringByResolvingSymlinksInPath] hasPrefix:root]
            || ![value.pathExtension.lowercaseString isEqual:@"png"])
            { CFRelease(app); return fail(@"case attachment path refused"); }
        NSData *source = [NSData dataWithContentsOfFile:value];
        NSMutableDictionary *receipt = [@{@"case_id":env(@"ZWAI_NATIVE_CASE_ID"), @"pid":@(pid),
            @"method":@"native-menu", @"source_length":@(source.length), @"source_sha256":pasteDigest(source)} mutableCopy];
        if (!source.length) {
            receipt[@"error"] = @"case PNG source is unavailable";
            if (!savePasteReceipt(receipt)) fprintf(stderr, "WARN native paste evidence could not be retained\n");
            CFRelease(app); return fail(@"case PNG source is unavailable");
        }
        NSString *failure = nil;
        NSPasteboard *pasteboard = NSPasteboard.generalPasteboard;
        [pasteboard clearContents];
        BOOL stored = [pasteboard setData:source forType:NSPasteboardTypePNG];
        NSInteger ownedCount = pasteboard.changeCount;
        NSData *actual = [pasteboard dataForType:NSPasteboardTypePNG];
        NSInteger observedCount = pasteboard.changeCount;
        receipt[@"clipboard"] = @{@"set_data_success":@(stored), @"change_count":@(ownedCount),
            @"png_length":@(actual.length), @"png_sha256":pasteDigest(actual), @"readback_count":@(observedCount)};
        if (!validPasteboardPayload(source, stored, actual, ownedCount, observedCount))
            failure = @"case PNG clipboard identity was not confirmed; no paste requested";
        if (!failure) {
            AXError focusError = AXUIElementSetAttributeValue((__bridge AXUIElementRef)areas[0], kAXFocusedAttribute, kCFBooleanTrue);
            receipt[@"focus_set_error"] = @(focusError);
            if (focusError) failure = @"native image paste cannot focus its case composer";
        }
        BOOL focused = NO;
        for (NSUInteger attempt = 0; !failure && attempt < 20; attempt++) {
            id current = attr(app, kAXFocusedUIElementAttribute);
            if (current && CFEqual((__bridge CFTypeRef)current, (__bridge CFTypeRef)areas[0])) { focused = YES; break; }
            usleep(25000);
        }
        if (!focused) failure = failure ?: @"native image paste lost its exact guest composer target";
        receipt[@"focused_composer"] = @(focused);
        if (!failure) {
            NSDictionary *menu = requestNativePaste(app, pid, pasteboard, source, ownedCount);
            receipt[@"menu"] = menu;
            if ([menu[@"paste_press_error"] intValue] != kAXErrorSuccess)
                failure = @"native Edit/Paste requires unique enabled menu items in the foreground case app";
        }
        if (failure) receipt[@"error"] = failure;
        if (!savePasteReceipt(receipt)) {
            if (failure) fprintf(stderr, "WARN native paste evidence could not be retained\n");
            else { CFRelease(app); return fail(@"native paste evidence could not be retained"); }
        }
        if (failure) { CFRelease(app); return fail(failure); }
        // This acknowledges one menu request. collect_draft independently waits for bytes and the saved UI.
        result[@"paste_method"] = @"native-menu";
        result[@"pasted"] = @YES;
    } else if (![action isEqual:@"snapshot"]) { CFRelease(app); return fail(@"unsupported test action"); }
    puts([[NSString alloc] initWithData:[NSJSONSerialization dataWithJSONObject:result options:0 error:nil]
                               encoding:NSUTF8StringEncoding].UTF8String);
    CFRelease(app); return 0;
} }
