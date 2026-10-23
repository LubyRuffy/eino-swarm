#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include "recovery_snapshot_darwin.h"

extern void desktopSnapshotResult(uint64_t request, int status, unsigned char *pixels, int length);

static WKWebView *zwaiFindWebView(NSView *view) {
    if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
    for (NSView *child in view.subviews) {
        WKWebView *found = zwaiFindWebView(child);
        if (found != nil) return found;
    }
    return nil;
}

void zwaiRecoveryEvaluate(void *native, const char *script) {
    if (native == NULL || script == NULL) return;
    WKWebView *view = zwaiFindWebView(((NSWindow *)native).contentView);
    if (view != nil) [view evaluateJavaScript:[NSString stringWithUTF8String:script] completionHandler:nil];
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
