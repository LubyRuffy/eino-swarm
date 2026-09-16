package desktop

import "testing"

func TestTrafficButtonYCentresInTheHeader(t *testing.T) {
	// The HTML toggle is items-center in h-12. If the native lights use a
	// different vertical origin they look like they belong to a different bar.
	if got := trafficButtonY(TitlebarHeight, 14); got != 17 {
		t.Fatalf("trafficButtonY(48, 14) = %v, want 17", got)
	}
}

func TestTrafficButtonYRejectsUnusableSizes(t *testing.T) {
	if got := trafficButtonY(0, 14); got != 0 {
		t.Fatalf("zero header = %v, want 0", got)
	}
	if got := trafficButtonY(TitlebarHeight, 0); got != 0 {
		t.Fatalf("zero button = %v, want 0", got)
	}
	if got := trafficButtonY(TitlebarHeight, 64); got != 0 {
		t.Fatalf("button taller than the bar = %v, want 0", got)
	}
}

func TestTrafficInsetLeavesACodexGap(t *testing.T) {
	if got := trafficInset(70); got != 78 {
		t.Fatalf("trafficInset(70) = %v, want 78", got)
	}
	if got := trafficInset(0); got != 0 {
		t.Fatalf("missing lights = %v, want 0", got)
	}
}

func TestApplyTrafficLightInsetSkipsWithoutLights(t *testing.T) {
	called := false
	applyTrafficLightInset(nil, func(string) { called = true })
	if called {
		t.Fatal("wrote CSS without native traffic lights")
	}
	applyMeasuredInset(0, func(string) { t.Fatal("wrote CSS for a zero inset") })
	applyMeasuredInset(70, nil)
}

func TestApplyMeasuredInsetWritesThePadding(t *testing.T) {
	var got string
	applyMeasuredInset(70, func(js string) { got = js })
	want := trafficInsetJS(trafficInset(70))
	if got != want {
		t.Fatalf("js = %q, want %q", got, want)
	}
}

func TestTrafficInsetJSWritesPixels(t *testing.T) {
	got := trafficInsetJS(78.4)
	want := "document.documentElement.style.setProperty('--traffic-light-inset','78px')"
	if got != want {
		t.Fatalf("js = %q, want %q", got, want)
	}
	if got := trafficInsetJS(0); got != "" {
		t.Fatalf("empty inset still produced JS: %q", got)
	}
}
