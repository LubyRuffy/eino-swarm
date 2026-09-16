package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestMacIconRadiusScalesWithTheShortEdge(t *testing.T) {
	if got := macIconRadius(1, 100); got != 0 {
		t.Fatalf("tiny canvas still rounded: %v", got)
	}
	if got := macIconRadius(0, 0); got != 0 {
		t.Fatalf("empty canvas still rounded: %v", got)
	}
	if got := macIconRadius(2, 2); got != 0 {
		t.Fatalf("content square smaller than 2px still rounded: %v", got)
	}
	got := macIconRadius(100, 200)
	want := 100 * macIconContent * macIconCorner
	if got != want {
		t.Fatalf("radius %v, want %v (short content edge)", got, want)
	}
	if macIconRadius(200, 100) != want {
		t.Fatal("a tall canvas must use the short edge")
	}
}

func TestMacIconContentRectIsApplesGrid(t *testing.T) {
	ox, oy, cw, ch := macIconContentRect(1024, 1024)
	if cw != 824 || ch != 824 {
		t.Fatalf("content %v×%v, want 824×824", cw, ch)
	}
	if ox != 100 || oy != 100 {
		t.Fatalf("origin %v,%v, want 100,100", ox, oy)
	}
}

func TestRoundedBoxKeepsTheCentreAndCutsTheCorner(t *testing.T) {
	// 100×100, radius 20, origin at the centre.
	if d := roundedBoxSDF(0, 0, 50, 50, 20); d >= 0 {
		t.Fatalf("centre is outside: %v", d)
	}
	if d := roundedBoxSDF(49, 0, 50, 50, 20); d >= 0 {
		t.Fatalf("mid-edge is outside: %v", d)
	}
	if d := roundedBoxSDF(49, 49, 50, 50, 20); d <= 0 {
		t.Fatalf("square corner survived the round: %v", d)
	}
	if d := roundedBoxSDF(0, 0, 50, 50, -5); d >= 0 {
		t.Fatalf("negative radius must still contain the centre: %v", d)
	}
	if d := roundedBoxSDF(0, 0, 10, 50, 80); d >= 0 {
		t.Fatalf("radius larger than the half-width must clamp: %v", d)
	}
	if d := roundedBoxSDF(0, 0, 50, 10, 40); d >= 0 {
		t.Fatalf("radius larger than the half-height must clamp: %v", d)
	}
}

func TestRoundedRectCoverageAntialiasesTheEdge(t *testing.T) {
	if got := roundedRectCoverage(50, 50, 100, 100, 20); got != 1 {
		t.Fatalf("centre coverage %v", got)
	}
	if got := roundedRectCoverage(0.5, 0.5, 100, 100, 20); got != 0 {
		t.Fatalf("outer corner still opaque: %v", got)
	}
	if got := roundedRectCoverage(50, 0.5, 100, 100, 20); got != 1 {
		t.Fatalf("mid-top punched out: %v", got)
	}
	if got := roundedRectCoverage(10, 10, 0, 100, 20); got != 0 {
		t.Fatalf("zero-width canvas leaked: %v", got)
	}
	foundBlend := false
	for y := 0; y < 100 && !foundBlend; y++ {
		for x := 0; x < 100; x++ {
			c := roundedRectCoverage(float64(x)+0.5, float64(y)+0.5, 100, 100, 20)
			if c > 0.1 && c < 0.9 {
				foundBlend = true
				break
			}
		}
	}
	if !foundBlend {
		t.Fatal("corner arc has no antialias")
	}
}

func TestRoundPNGCutsSquareCorners(t *testing.T) {
	src := solidPNG(t, 32, 32, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	out, err := roundPNG(src)
	if err != nil {
		t.Fatal(err)
	}
	img := decodeNRGBA(t, out)
	if img.NRGBAAt(0, 0).A != 0 {
		t.Fatal("top-left corner is still opaque")
	}
	if img.NRGBAAt(31, 0).A != 0 {
		t.Fatal("top-right corner is still opaque")
	}
	c := img.NRGBAAt(16, 16)
	if c.A < 250 || c.R < 250 {
		t.Fatalf("centre was damaged: %+v", c)
	}
	if img.NRGBAAt(16, 0).A != 0 {
		t.Fatal("icon bleeds to the canvas edge")
	}
}

func TestRoundPNGAcceptsPalettedSource(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, 16, 16), color.Palette{color.White, color.Black})
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetColorIndex(x, y, 0)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, err := roundPNG(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got := decodeNRGBA(t, out)
	if got.NRGBAAt(0, 0).A != 0 {
		t.Fatal("paletted square kept its corners")
	}
}

func TestRoundPNGRejectsGarbageAndLeavesTinyImages(t *testing.T) {
	if _, err := roundPNG([]byte("not a png")); err == nil {
		t.Fatal("garbage decoded as an icon")
	}
	tiny := solidPNG(t, 1, 1, color.NRGBA{A: 255, R: 1})
	got, err := roundPNG(tiny)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, tiny) {
		t.Fatal("a 1×1 icon should be left alone")
	}
	two := solidPNG(t, 2, 2, color.NRGBA{A: 255, R: 1})
	got, err = roundPNG(two)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, two) {
		t.Fatal("a 2×2 icon is smaller than the content square and should be left alone")
	}
}

func TestLoadDockIconFallsBackWhenTheBytesAreNotAnImage(t *testing.T) {
	src := []byte("nope")
	if !bytes.Equal(loadDockIcon(src), src) {
		t.Fatal("a broken source must stay the broken source")
	}
}

func TestLoadDockIconRoundsAValidPNG(t *testing.T) {
	src := solidPNG(t, 32, 32, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	got := loadDockIcon(src)
	if bytes.Equal(got, src) {
		t.Fatal("a square source was handed to Wails unchanged")
	}
}

func TestDockIconCornersAreTransparent(t *testing.T) {
	// go run is not an .app, so Dock will draw whatever alpha we give it.
	img := decodeNRGBA(t, dockIcon())
	b := img.Bounds()
	if img.NRGBAAt(b.Min.X, b.Min.Y).A != 0 {
		t.Fatal("Dock will show a square: the corner is still opaque")
	}
	cx := (b.Min.X + b.Max.X) / 2
	cy := (b.Min.Y + b.Max.Y) / 2
	if img.NRGBAAt(cx, cy).A < 250 {
		t.Fatal("the mark itself was punched out")
	}
	if img.NRGBAAt(cx, b.Min.Y).A != 0 {
		t.Fatal("icon fills the canvas; Dock will draw it larger than a .app")
	}
}

func TestClampIntHitsBothEnds(t *testing.T) {
	if got := clampInt(-3, 0, 10); got != 0 {
		t.Fatalf("low clamp %d", got)
	}
	if got := clampInt(99, 0, 10); got != 10 {
		t.Fatalf("high clamp %d", got)
	}
	if got := clampInt(4, 0, 10); got != 4 {
		t.Fatalf("passthrough %d", got)
	}
}

func TestSampleNRGBAClampsPastTheEdge(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 1, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{B: 2, A: 255})
	b := img.Bounds()
	got := sampleNRGBA(img, b, -4, -4)
	if got.R != 1 {
		t.Fatalf("negative sample %+v", got)
	}
	got = sampleNRGBA(img, b, 40, 40)
	if got.B != 2 {
		t.Fatalf("past-end sample %+v", got)
	}
}

func solidPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeNRGBA(t *testing.T, src []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	n, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("png decoded as %T, want NRGBA", img)
	}
	return n
}
