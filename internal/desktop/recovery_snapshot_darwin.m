#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>
#include <math.h>
#include "recovery_snapshot_darwin.h"

extern void desktopSnapshotResult(uint64_t request, int status, unsigned char *pixels, int length);

// This sibling is omitted from WKWebView's own snapshot. The real nonce still
// paints in that view, while the native window presents only the page background.
@interface ZwaiRenderProbeCover : NSView
@property(retain) NSColor *colour;
@end

@implementation ZwaiRenderProbeCover
- (BOOL)isOpaque { return YES; }
- (BOOL)acceptsFirstResponder { return NO; }
- (BOOL)isAccessibilityElement { return NO; }
- (NSView *)hitTest:(NSPoint)point { return nil; }
- (void)drawRect:(NSRect)dirtyRect {
    // AppKit may invalidate outside our bounds; filling dirtyRect can erase
    // the entire window on macOS 14+, whose views no longer clip by default.
    [self.colour setFill];
    NSRectFill(self.bounds);
}
- (void)dealloc { [_colour release]; [super dealloc]; }
@end

static char zwaiRenderProbeCoverKey;

static WKWebView *zwaiFindWebView(NSView *view) {
    if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
    for (NSView *child in view.subviews) {
        WKWebView *found = zwaiFindWebView(child);
        if (found != nil) return found;
    }
    return nil;
}

static BOOL zwaiSetRenderCover(WKWebView *view, id value) {
    if (view.superview == nil || ![value isKindOfClass:[NSArray class]] || [value count] != 3) return NO;
    for (id channel in value) {
        if (![channel isKindOfClass:[NSNumber class]]) return NO;
        double number = [channel doubleValue];
        if (!isfinite(number) || number < 0 || number > 255 || number != floor(number)) return NO;
    }
    ZwaiRenderProbeCover *cover = objc_getAssociatedObject(view, &zwaiRenderProbeCoverKey);
    if (cover == nil) {
        cover = [[[ZwaiRenderProbeCover alloc] initWithFrame:NSZeroRect] autorelease];
        cover.clipsToBounds = YES;
        cover.translatesAutoresizingMaskIntoConstraints = NO;
        [view.superview addSubview:cover positioned:NSWindowAbove relativeTo:view];
        [NSLayoutConstraint activateConstraints:@[
            [cover.leadingAnchor constraintEqualToAnchor:view.leadingAnchor],
            [cover.topAnchor constraintEqualToAnchor:view.topAnchor],
            [cover.widthAnchor constraintEqualToConstant:16],
            [cover.heightAnchor constraintEqualToConstant:16],
        ]];
        objc_setAssociatedObject(view, &zwaiRenderProbeCoverKey, cover, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    }
    // Moving or removing the WebView must never leave a nonce without its cover.
    if (cover.superview != view.superview) return NO;
    cover.colour = [NSColor colorWithSRGBRed:[value[0] doubleValue] / 255.0
        green:[value[1] doubleValue] / 255.0 blue:[value[2] doubleValue] / 255.0 alpha:1];
    cover.hidden = NO;
    cover.needsDisplay = YES;
    [view.superview layoutSubtreeIfNeeded];
    return YES;
}

void zwaiRecoveryCover(void *native, unsigned char red, unsigned char green, unsigned char blue) {
    if (native == NULL) return;
    WKWebView *view = zwaiFindWebView(((NSWindow *)native).contentView);
    if (view != nil) zwaiSetRenderCover(view, @[@(red), @(green), @(blue)]);
}

void zwaiRecoveryEvaluate(void *native, const char *script, const char *coverScript) {
    if (native == NULL || script == NULL || coverScript == NULL) return;
    WKWebView *view = zwaiFindWebView(((NSWindow *)native).contentView);
    if (view == nil) return;
    NSString *probe = [NSString stringWithUTF8String:script];
    [view evaluateJavaScript:[NSString stringWithUTF8String:coverScript] completionHandler:^(id colour, NSError *error) {
        // Only the first paint needs a newly sampled colour. A modal transition
        // may temporarily make decoration unreadable without stopping WebKit.
        BOOL prepared = error == nil && zwaiSetRenderCover(view, colour);
        ZwaiRenderProbeCover *cover = objc_getAssociatedObject(view, &zwaiRenderProbeCoverKey);
        if (prepared || (cover != nil && cover.superview == view.superview && !cover.hidden)) {
            [view evaluateJavaScript:probe completionHandler:nil];
        }
    }];
}

void zwaiRecoverySnapshot(void *native, uint64_t request) {
    if (native == NULL) { desktopSnapshotResult(request, 1, NULL, 0); return; }
    WKWebView *view = zwaiFindWebView(((NSWindow *)native).contentView);
    if (view == nil || NSWidth(view.bounds) < 64 || NSHeight(view.bounds) < 64) {
        desktopSnapshotResult(request, 2, NULL, 0); return;
    }
    WKSnapshotConfiguration *configuration = [[WKSnapshotConfiguration alloc] init];
    CGFloat y = view.isFlipped ? 0 : NSHeight(view.bounds) - 64;
    configuration.rect = CGRectMake(0, y, 64, 64);
    configuration.snapshotWidth = @64;
    configuration.afterScreenUpdates = YES;
    [view takeSnapshotWithConfiguration:configuration completionHandler:^(NSImage *image, NSError *error) {
        if (error != nil || image == nil) { desktopSnapshotResult(request, 3, NULL, 0); return; }
        CGImageRef source = [image CGImageForProposedRect:NULL context:nil hints:nil];
        if (source == NULL || CGImageGetWidth(source) == 0 || CGImageGetHeight(source) == 0) {
            desktopSnapshotResult(request, 4, NULL, 0); return;
        }
        unsigned char pixels[64 * 64 * 4] = {0};
        CGColorSpaceRef space = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
        CGContextRef context = CGBitmapContextCreate(pixels, 64, 64, 8, 64 * 4, space,
            kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big);
        CGColorSpaceRelease(space);
        if (context == NULL) { desktopSnapshotResult(request, 5, NULL, 0); return; }
        CGContextDrawImage(context, CGRectMake(0, 0, 64, 64), source);
        CGContextRelease(context);
        desktopSnapshotResult(request, 0, pixels, sizeof(pixels));
    }];
    [configuration release];
}
