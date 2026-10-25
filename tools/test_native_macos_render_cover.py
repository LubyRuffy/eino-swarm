"""Exercise the actual native probe entry without opening a host window."""
from pathlib import Path
import platform
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
FIXTURE = r'''
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdio.h>
static int probes = 0, preparations = 0;
static NSView *web;
static NSString *mode;
static void require(BOOL predicate, const char *reason) {
    if (!predicate) { fprintf(stderr, "%s\n", reason); exit(31); }
}
@interface FlippedParent : NSView
@end
@implementation FlippedParent
- (BOOL)isFlipped { return YES; }
@end
@interface FixtureWebView : NSView
- (void)evaluateJavaScript:(NSString *)script completionHandler:(void (^)(id, NSError *))handler;
- (void)takeSnapshotWithConfiguration:(WKSnapshotConfiguration *)configuration completionHandler:(void (^)(NSImage *, NSError *))handler;
@end
@implementation FixtureWebView
- (void)evaluateJavaScript:(NSString *)script completionHandler:(void (^)(id, NSError *))handler {
    if ([script isEqual:@"probe"]) {
        require(self.superview.subviews.count == 2, "probe painted before an opaque native cover existed");
        NSView *cover = self.superview.subviews.lastObject;
        require(cover != self && cover.isOpaque && !cover.isHidden, "cover must be an opaque visible sibling");
        require(!cover.acceptsFirstResponder && !cover.isAccessibilityElement && [cover hitTest:NSMakePoint(8,8)] == nil,
            "internal cover must not receive window input or enter accessibility");
        NSRect expected = [self convertRect:NSMakeRect(0,self.isFlipped ? 0 : NSHeight(self.bounds)-16,16,16) toView:self.superview];
        require(NSEqualRects(cover.frame, expected), "cover does not cover the WebView's physical top-left corner");
        probes++;
        if (handler) handler(nil, nil);
    } else {
        preparations++;
        id colour = @[@240, @241, @242];
        if ([mode isEqual:@"missing"]) colour = nil;
        if ([mode isEqual:@"count"]) colour = @[@240, @241];
        if ([mode isEqual:@"string"]) colour = @[@"240", @241, @242];
        if ([mode isEqual:@"low"]) colour = @[@(-1), @241, @242];
        if ([mode isEqual:@"high"]) colour = @[@256, @241, @242];
        if ([mode isEqual:@"fraction"]) colour = @[@0.5, @241, @242];
        if ([mode isEqual:@"nan"]) colour = @[@(NAN), @241, @242];
        if (handler) handler(colour, [mode isEqual:@"error"] ? [NSError errorWithDomain:@"fixture" code:1 userInfo:nil] : nil);
    }
}
- (void)takeSnapshotWithConfiguration:(WKSnapshotConfiguration *)configuration completionHandler:(void (^)(NSImage *, NSError *))handler {
    handler(nil, [NSError errorWithDomain:@"fixture" code:1 userInfo:nil]);
}
@end
@interface FixtureWindow : NSObject
@property(retain) NSView *contentView;
@end
@implementation FixtureWindow
@end
void desktopSnapshotResult(uint64_t request, int status, unsigned char *pixels, int length) {}
#define WKWebView FixtureWebView
#define NSWindow FixtureWindow
#include "recovery_snapshot_darwin.m"
int main(int argc, char **argv) { @autoreleasepool {
    mode = argc > 1 ? [NSString stringWithUTF8String:argv[1]] : @"normal";
    FixtureWindow *window = [FixtureWindow new];
    Class parentClass = [mode isEqual:@"flipped"] ? FlippedParent.class : NSView.class;
    window.contentView = [[[parentClass alloc] initWithFrame:NSMakeRect(0,0,500,400)] autorelease];
    web = [[[FixtureWebView alloc] initWithFrame:NSMakeRect(17,23,400,300)] autorelease];
    [window.contentView addSubview:web];
    zwaiRecoveryEvaluate(window, "probe", "prepare");
    if (![@[@"normal", @"flipped"] containsObject:mode]) {
        require(probes == 0 && preparations == 1 && window.contentView.subviews.count == 1,
            "failed colour preparation must neither paint nor create a cover");
        printf("preparation refused\n"); return 0;
    }
    require(probes == 1 && preparations == 1, "probe bypassed the colour preparation");
    NSView *cover = window.contentView.subviews.lastObject;
    require(fabs(((ZwaiRenderProbeCover *)cover).colour.alphaComponent - 1) < 0.001, "cover is transparent");
    [web setFrame:NSMakeRect(21,31,420,320)];
    zwaiRecoveryEvaluate(window, "probe", "prepare");
    require(probes == 2 && window.contentView.subviews.lastObject == cover, "resize created a second cover or lost paint");
    zwaiRecoveryCover(window, 10, 20, 30);
    NSColor *colour = [((ZwaiRenderProbeCover *)cover).colour colorUsingColorSpace:NSColorSpace.sRGBColorSpace];
    require(fabs(colour.redComponent - 10.0/255) < 0.001 && fabs(colour.greenComponent - 20.0/255) < 0.001 &&
        fabs(colour.blueComponent - 30.0/255) < 0.001 && colour.alphaComponent == 1, "live background update changed the colour or opacity");
    NSString *previousMode = mode;
    for (NSString *failure in @[@"missing", @"error", @"fraction"]) {
        mode = failure;
        zwaiRecoveryEvaluate(window, "probe", "prepare");
    }
    require(probes == 5, "a modal decoration failure disabled real pixel health checks behind an existing cover");
    mode = previousMode;
    [cover removeFromSuperview];
    zwaiRecoveryEvaluate(window, "probe", "prepare");
    require(probes == 5, "detached cover allowed another visible probe");
    zwaiRecoveryEvaluate(NULL, "probe", "prepare");
    zwaiRecoveryEvaluate(window, NULL, "prepare");
    zwaiRecoveryEvaluate(window, "probe", NULL);
    zwaiRecoveryCover(NULL, 1, 2, 3);
    window.contentView = [[[NSView alloc] initWithFrame:NSZeroRect] autorelease];
    zwaiRecoveryEvaluate(window, "probe", "prepare");
    zwaiRecoveryCover(window, 1, 2, 3);
    printf("covered before paint and after resize\n");
    return 0;
}}
'''


@unittest.skipUnless(platform.system() == "Darwin", "requires the macOS SDK")
class NativeRenderCoverTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        temporary = tempfile.TemporaryDirectory(prefix="render-cover-headless-")
        cls.addClassCleanup(temporary.cleanup)
        source = Path(temporary.name) / "test.m"
        cls.binary = Path(temporary.name) / "test"
        source.write_text(FIXTURE)
        subprocess.run([
            "clang", "-fblocks", "-I", str(ROOT / "internal/desktop"),
            "-framework", "Cocoa", "-framework", "WebKit",
            str(source), "-o", str(cls.binary),
        ], check=True, capture_output=True, text=True)

    def test_probe_is_covered_before_paint_and_after_resize(self):
        for mode in ("normal", "flipped"):
            with self.subTest(mode=mode):
                result = subprocess.run([str(self.binary), mode], capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("covered before paint and after resize", result.stdout)

    def test_missing_failed_or_malformed_colour_cannot_paint_a_probe(self):
        for mode in ("missing", "count", "string", "low", "high", "fraction", "nan", "error"):
            with self.subTest(mode=mode):
                result = subprocess.run([str(self.binary), mode], capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("preparation refused", result.stdout)


if __name__ == "__main__":
    unittest.main()
