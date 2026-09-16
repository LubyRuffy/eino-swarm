//go:build darwin

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// Centre the traffic lights in a title-bar container that matches the HTML
// header, and return the zoom button's right edge in points so the front end
// can pad to it. AppKit will shove these back on resize and on Tahoe focus
// changes; the Go side re-runs this.
double positionTrafficLights(void* nsWindow, double headerHeight) {
	if (nsWindow == NULL || headerHeight <= 0) {
		return 0;
	}
	NSWindow *window = (NSWindow *)nsWindow;
	NSButton *close = [window standardWindowButton:NSWindowCloseButton];
	NSButton *mini = [window standardWindowButton:NSWindowMiniaturizeButton];
	NSButton *zoom = [window standardWindowButton:NSWindowZoomButton];
	if (close == nil || mini == nil || zoom == nil || close.superview == nil) {
		return 0;
	}
	NSView *container = close.superview.superview;
	if (container == nil) {
		container = close.superview;
	}
	if (container == nil) {
		return 0;
	}

	CGFloat buttonWidth = NSWidth(close.frame);
	CGFloat buttonHeight = NSHeight(close.frame);
	CGFloat padding = NSMinX(mini.frame) - NSMaxX(close.frame);
	CGFloat start = NSMinX(close.frame);
	if (buttonWidth <= 0 || buttonHeight <= 0 || buttonHeight > headerHeight) {
		return 0;
	}
	if (padding < 1) {
		padding = 8;
	}
	if (start < 1) {
		start = 16;
	}

	NSRect bounds = container.frame;
	bounds.size.height = headerHeight;
	bounds.origin.y = NSHeight(window.frame) - headerHeight;
	[container setFrame:bounds];

	CGFloat y = (headerHeight - buttonHeight) / 2.0;
	[close setFrameOrigin:NSMakePoint(start, y)];
	[mini setFrameOrigin:NSMakePoint(start + buttonWidth + padding, y)];
	[zoom setFrameOrigin:NSMakePoint(start + 2.0 * (buttonWidth + padding), y)];

	return NSMaxX(zoom.frame);
}
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func keepTrafficLightsAligned(win application.Window) {
	apply := func(_ *application.WindowEvent) {
		// Wails fires these on a worker goroutine. NSWindow.setFrame from
		// there aborts the process.
		application.InvokeAsync(func() {
			applyTrafficLightInset(win.NativeWindow(), win.ExecJS)
		})
	}
	for _, ev := range []events.WindowEventType{
		events.Common.WindowRuntimeReady,
		events.Common.WindowDidResize,
		events.Mac.WebViewDidFinishNavigation,
		events.Mac.WindowDidDeminiaturize,
		events.Mac.WindowDidExitFullScreen,
		events.Mac.WindowDidBecomeKey,
	} {
		win.OnWindowEvent(ev, apply)
	}
}

// runOnMain is Wails' main-thread hop. Tests replace it so a fake pointer
// never reaches AppKit.
var runOnMain = func(fn func() float64) float64 {
	return application.InvokeSyncWithResult(fn)
}

func positionTrafficLights(nsWindow unsafe.Pointer) float64 {
	if nsWindow == nil {
		return 0
	}
	return runOnMain(func() float64 {
		return float64(C.positionTrafficLights(nsWindow, C.double(TitlebarHeight)))
	})
}
