package desktop

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var errBadMark = errors.New("desktop appicon: corrupt")

func imagesEqual(a, b image.Image) bool {
	return imagesClose(a, b, 1)
}

func imagesClose(a, b image.Image, slop uint8) bool {
	ab, bb := a.Bounds(), b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return false
	}
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			gc := color.NRGBAModel.Convert(a.At(ab.Min.X+x, ab.Min.Y+y)).(color.NRGBA)
			wc := color.NRGBAModel.Convert(b.At(bb.Min.X+x, bb.Min.Y+y)).(color.NRGBA)
			if !nrgbaClose(gc, wc, slop) {
				return false
			}
		}
	}
	return true
}

func nrgbaClose(a, b color.NRGBA, slop uint8) bool {
	return chanClose(a.R, b.R, slop) && chanClose(a.G, b.G, slop) && chanClose(a.B, b.B, slop) && chanClose(a.A, b.A, slop)
}

func chanClose(a, b, slop uint8) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	return d <= int(slop)
}

func firstPixelDiff(a, b image.Image) (int, int, color.NRGBA, color.NRGBA) {
	ab, bb := a.Bounds(), b.Bounds()
	for y := 0; y < ab.Dy() && y < bb.Dy(); y++ {
		for x := 0; x < ab.Dx() && x < bb.Dx(); x++ {
			gc := color.NRGBAModel.Convert(a.At(ab.Min.X+x, ab.Min.Y+y)).(color.NRGBA)
			wc := color.NRGBAModel.Convert(b.At(bb.Min.X+x, bb.Min.Y+y)).(color.NRGBA)
			if gc != wc {
				return x, y, gc, wc
			}
		}
	}
	return -1, -1, color.NRGBA{}, color.NRGBA{}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func pngIHDR(b []byte) (int, int) {
	if len(b) < 24 || b[0] != 0x89 || b[1] != 0x50 {
		return 0, 0
	}
	return int(b[16])<<24 | int(b[17])<<16 | int(b[18])<<8 | int(b[19]),
		int(b[20])<<24 | int(b[21])<<16 | int(b[22])<<8 | int(b[23])
}

func markOnWhite(img image.Image) bool {
	b := img.Bounds()
	corner := color.NRGBAModel.Convert(img.At(b.Min.X, b.Min.Y)).(color.NRGBA)
	if corner.A != 255 || corner.R < 240 || corner.G < 240 || corner.B < 240 {
		return false
	}
	x0, x1 := b.Min.X+b.Dx()/4, b.Min.X+3*b.Dx()/4
	y0, y1 := b.Min.Y+b.Dy()/4, b.Min.Y+3*b.Dy()/4
	stepX := (x1 - x0) / 32
	if stepX < 1 {
		stepX = 1
	}
	stepY := (y1 - y0) / 32
	if stepY < 1 {
		stepY = 1
	}
	for y := y0; y < y1; y += stepY {
		for x := x0; x < x1; x += stepX {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A > 200 && c.R < 80 {
				return true
			}
		}
	}
	return false
}

func TestPhoneLauncherSlotsMatchTheDesktopMark(t *testing.T) {
	// cap add left Capacitor's cyan lattice in the iOS/Android icon
	// slots. The shipped files have to be this generator's output from
	// appicon.png, or a rebuild still shows someone else's logo.
	// One LSB of slop: `go test -cover` and `go run` can round bilinear
	// samples differently (FMA), and that is not a wrong mark. Splash
	// canvases are millions of pixels; a byte match is enough, and a
	// miss only samples for a dark mark on white (the lattice would fail).
	root := repoRoot(t)
	mark, err := loadAppMark()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	var small []phoneIconSlot
	var splash []phoneIconSlot
	for _, slot := range phoneIconSlots() {
		if slot.kind == phoneSplash {
			splash = append(splash, slot)
			continue
		}
		small = append(small, slot)
	}
	if len(small) < 10 || len(splash) < 3 {
		t.Fatalf("too few slots: %d small %d splash", len(small), len(splash))
	}
	if err := writePhoneIconSlots(tmp, mark, small); err != nil {
		t.Fatal(err)
	}
	for _, slot := range small {
		got, err := os.ReadFile(filepath.Join(tmp, slot.rel))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(root, slot.rel))
		if err != nil {
			t.Fatalf("shipped %s: %v", slot.rel, err)
		}
		gw, gh := pngIHDR(got)
		if gw != slot.w || gh != slot.h {
			t.Fatalf("%s IHDR is %dx%d, want %dx%d", slot.rel, gw, gh, slot.w, slot.h)
		}
		if bytes.Equal(got, want) {
			continue
		}
		gotImg, err := png.Decode(bytes.NewReader(got))
		if err != nil {
			t.Fatal(err)
		}
		wantImg, err := png.Decode(bytes.NewReader(want))
		if err != nil {
			t.Fatal(err)
		}
		if !imagesEqual(gotImg, wantImg) {
			x, y, gc, wc := firstPixelDiff(gotImg, wantImg)
			t.Fatalf("%s does not match WritePhoneIcons at %d,%d gen=%+v shipped=%+v; run go run ./mobile/scripts/genicons.go", slot.rel, x, y, gc, wc)
		}
	}
	for _, slot := range splash {
		want, err := os.ReadFile(filepath.Join(root, slot.rel))
		if err != nil {
			t.Fatalf("shipped %s: %v", slot.rel, err)
		}
		gw, gh := pngIHDR(want)
		if gw != slot.w || gh != slot.h {
			t.Fatalf("%s IHDR is %dx%d, want %dx%d", slot.rel, gw, gh, slot.w, slot.h)
		}
		img, err := png.Decode(bytes.NewReader(want))
		if err != nil {
			t.Fatal(err)
		}
		if !markOnWhite(img) {
			t.Fatalf("%s is not the zwai mark on white; run go run ./mobile/scripts/genicons.go", slot.rel)
		}
	}
}

func TestRenderPhoneIconKeepsTheMarkOnWhite(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	ink := color.NRGBA{A: 255}
	src.SetNRGBA(1, 1, ink)
	src.SetNRGBA(2, 2, ink)
	launch := renderPhoneIcon(src, phoneIconSlot{w: 8, h: 8, kind: phoneLauncher})
	if launch.Bounds().Dx() != 8 {
		t.Fatal("launcher size")
	}
	if c := launch.NRGBAAt(0, 0); c.A != 255 || c.R != 255 {
		t.Fatalf("launcher corner should be opaque white, got %+v", c)
	}
	fg := renderPhoneIcon(src, phoneIconSlot{w: 12, h: 12, kind: phoneForeground})
	if fg.NRGBAAt(0, 0).A != 0 {
		t.Fatal("adaptive padding must stay transparent so the XML background shows")
	}
	paper := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	fillWhite(paper)
	paper.SetNRGBA(0, 0, color.NRGBA{R: 252, G: 252, B: 252, A: 255})
	fgPaper := renderPhoneIcon(paper, phoneIconSlot{w: 12, h: 12, kind: phoneForeground})
	if fgPaper.NRGBAAt(6, 6).A != 0 {
		t.Fatal("near-white paper on the adaptive foreground must punch through")
	}
	splash := renderPhoneIcon(src, phoneIconSlot{w: 20, h: 10, kind: phoneSplash})
	if splash.NRGBAAt(0, 0).R != 255 || splash.NRGBAAt(0, 0).A != 255 {
		t.Fatal("splash must be white, not a letterbox")
	}
}

func TestScaleAndCompositeEdges(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	src.SetNRGBA(1, 1, color.NRGBA{B: 255, A: 255})
	got := scaleNRGBA(src, 4, 4)
	if got.Bounds().Dx() != 4 {
		t.Fatal("scale size")
	}
	empty := scaleNRGBA(image.NewNRGBA(image.Rect(0, 0, 0, 0)), 3, 3)
	if empty.Bounds().Dx() != 3 {
		t.Fatal("empty source should still return the canvas")
	}
	if c := overWhite(color.NRGBA{R: 0, A: 0}); c.R != 255 || c.A != 255 {
		t.Fatalf("clear over white %+v", c)
	}
	if c := overWhite(color.NRGBA{R: 252, G: 253, B: 251, A: 255}); c.R != 255 || c.A != 255 {
		t.Fatalf("off-white paper must flatten, got %+v", c)
	}
	if c := flattenPaper(color.NRGBA{R: 249, G: 249, B: 249, A: 255}); c.R != 255 {
		t.Fatal("249 is still paper; cover vs go run used to cliff here")
	}
	if w, h := pngIHDR([]byte("nope")); w != 0 || h != 0 {
		t.Fatal("short buffer is not a png")
	}
	dot := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	fillWhite(dot)
	dot.SetNRGBA(4, 4, color.NRGBA{A: 255})
	if !markOnWhite(dot) {
		t.Fatal("white canvas with centre ink is the launcher shape")
	}
	blank := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	fillWhite(blank)
	if markOnWhite(blank) {
		t.Fatal("plain white is Capacitor-empty, not a mark")
	}
	teal := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	fillWhite(teal)
	teal.SetNRGBA(0, 0, color.NRGBA{G: 200, B: 255, A: 255})
	if markOnWhite(teal) {
		t.Fatal("cyan lattice corner is not paper")
	}
	hdr := make([]byte, 24)
	hdr[0], hdr[1] = 0x89, 0x50
	hdr[18], hdr[22] = 4, 4 // 1024x1024
	if w, h := pngIHDR(hdr); w != 1024 || h != 1024 {
		t.Fatalf("IHDR 1024, got %dx%d", w, h)
	}
	if c := overWhite(color.NRGBA{R: 10, A: 255}); c.R != 10 {
		t.Fatalf("opaque ink passthrough %+v", c)
	}
	mid := overWhite(color.NRGBA{R: 0, A: 128})
	if mid.A != 255 || mid.R == 0 || mid.R == 255 {
		t.Fatalf("half-alpha should blend, got %+v", mid)
	}
	rgba := image.NewRGBA(image.Rect(0, 0, 1, 1))
	rgba.Set(0, 0, color.RGBA{G: 9, A: 255})
	converted := nrgbaOf(rgba)
	if converted.NRGBAAt(0, 0).G != 9 {
		t.Fatal("nrgbaOf must copy a non-NRGBA source")
	}
	if nrgbaOf(src) != src {
		t.Fatal("nrgbaOf should keep an NRGBA as-is")
	}
	tiny := fitOnCanvas(src, 4, 4, 0, true)
	if tiny.Bounds().Dx() != 4 {
		t.Fatal("zero content still needs a canvas")
	}
	clipped := fitOnCanvas(src, 2, 2, 8, true)
	if clipped.Bounds().Dx() != 2 {
		t.Fatal("a mark larger than the canvas must clip, not panic")
	}
	if minInt(3, 8) != 3 || minInt(9, 2) != 2 {
		t.Fatal("minInt")
	}
	if c := bilinearNRGBA(src, -5, -5); c.R != 255 {
		t.Fatalf("negative sample should clamp to the red corner, got %+v", c)
	}
	if c := bilinearNRGBA(src, 99, 99); c.B != 255 {
		t.Fatalf("overflow sample should clamp to the blue corner, got %+v", c)
	}
	if imagesEqual(src, scaleNRGBA(src, 3, 3)) {
		t.Fatal("different sizes must not compare equal")
	}
	if !nrgbaClose(color.NRGBA{R: 10}, color.NRGBA{R: 11}, 1) || nrgbaClose(color.NRGBA{R: 10}, color.NRGBA{R: 13}, 1) {
		t.Fatal("channel slop")
	}
}

func TestWritePNGReportsABlockedPath(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	if err := writePNG(filepath.Join(blocked, "x.png"), img); err == nil {
		t.Fatal("write through a file should fail")
	}
	mark := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	err := writePhoneIconSlots(dir, mark, []phoneIconSlot{{
		rel:  filepath.Join("blocked", "icon.png"),
		w:    2,
		h:    2,
		kind: phoneLauncher,
	}})
	if err == nil {
		t.Fatal("slot write through a file should fail")
	}
}

func TestWritePhoneIconsPaintsTheEmbeddedMark(t *testing.T) {
	prev := iconSlots
	t.Cleanup(func() { iconSlots = prev })
	iconSlots = func() []phoneIconSlot {
		return []phoneIconSlot{{rel: "icon.png", w: 16, h: 16, kind: phoneLauncher}}
	}
	dir := t.TempDir()
	if err := WritePhoneIcons(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	w, h := pngIHDR(raw)
	if w != 16 || h != 16 {
		t.Fatalf("wrote %dx%d", w, h)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !markOnWhite(img) {
		t.Fatal("WritePhoneIcons must paint the embedded zwai mark")
	}
}

func TestWritePhoneIconsReportsABadMark(t *testing.T) {
	prev := appMark
	t.Cleanup(func() { appMark = prev })
	appMark = func() (*image.NRGBA, error) {
		return nil, errBadMark
	}
	if err := WritePhoneIcons(t.TempDir()); err == nil {
		t.Fatal("a corrupt mark must not write slots")
	}
	if _, err := decodeAppMark([]byte("nope")); err == nil {
		t.Fatal("garbage bytes are not an appicon")
	}
}
