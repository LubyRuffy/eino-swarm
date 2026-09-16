package desktop

import (
	"fmt"
	"math"
	"unsafe"
)

// TitlebarHeight is the HTML header (h-12). Native traffic lights are centred
// in a container of this height so they share a baseline with the sidebar
// toggle instead of sitting in AppKit's default strip.
const TitlebarHeight = 48

// TrafficLightGap is the space between the zoom button and the first HTML
// control. Tight like Codex, not a leftover column.
const TrafficLightGap = 8

// trafficButtonY is the AppKit origin (from the bottom of the title-bar
// container) that vertically centres a button of the given height.
func trafficButtonY(headerHeight, buttonHeight float64) float64 {
	if headerHeight <= 0 || buttonHeight <= 0 || buttonHeight > headerHeight {
		return 0
	}
	return (headerHeight - buttonHeight) / 2
}

// trafficInset is the CSS padding that starts the HTML toggle just after the
// zoom button. zoomMaxX is the right edge of that button in window points.
func trafficInset(zoomMaxX float64) float64 {
	if zoomMaxX <= 0 {
		return 0
	}
	return zoomMaxX + TrafficLightGap
}

// trafficInsetJS sets the CSS variable the header reads. The value is a
// formatted float, not a string the page supplied.
func trafficInsetJS(px float64) string {
	if px <= 0 {
		return ""
	}
	return fmt.Sprintf(
		"document.documentElement.style.setProperty('--traffic-light-inset','%.0fpx')",
		math.Round(px),
	)
}

// applyTrafficLightInset measures the native lights and tells the page where
// the HTML toggle may start. No window, or lights AppKit has not laid out yet,
// is a no-op — the CSS fallback stays.
func applyTrafficLightInset(native unsafe.Pointer, execJS func(string)) {
	applyMeasuredInset(positionTrafficLights(native), execJS)
}

func applyMeasuredInset(zoomMaxX float64, execJS func(string)) {
	if execJS == nil {
		return
	}
	js := trafficInsetJS(trafficInset(zoomMaxX))
	if js == "" {
		return
	}
	execJS(js)
}
