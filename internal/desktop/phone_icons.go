package desktop

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// Android adaptive-icon safe zone is the centre 66% of the 108dp canvas.
const adaptiveSafe = 2.0 / 3.0

// Splash mark size as a fraction of the shorter canvas edge.
const splashFrac = 0.36

type phoneIconKind int

const (
	phoneLauncher phoneIconKind = iota
	phoneForeground
	phoneSplash
)

type phoneIconSlot struct {
	rel  string
	w, h int
	kind phoneIconKind
}

func phoneIconSlots() []phoneIconSlot {
	android := []phoneIconSlot{}
	for _, d := range []struct {
		dir        string
		launcher   int
		foreground int
	}{
		{"mipmap-mdpi", 48, 108},
		{"mipmap-hdpi", 72, 162},
		{"mipmap-xhdpi", 96, 216},
		{"mipmap-xxhdpi", 144, 324},
		{"mipmap-xxxhdpi", 192, 432},
	} {
		base := filepath.Join("mobile/android/app/src/main/res", d.dir)
		android = append(android,
			phoneIconSlot{filepath.Join(base, "ic_launcher.png"), d.launcher, d.launcher, phoneLauncher},
			phoneIconSlot{filepath.Join(base, "ic_launcher_round.png"), d.launcher, d.launcher, phoneLauncher},
			phoneIconSlot{filepath.Join(base, "ic_launcher_foreground.png"), d.foreground, d.foreground, phoneForeground},
		)
	}
	splash := []phoneIconSlot{
		{"mobile/android/app/src/main/res/drawable/splash.png", 480, 320, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-land-mdpi/splash.png", 480, 320, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-land-hdpi/splash.png", 800, 480, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-land-xhdpi/splash.png", 1280, 720, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-land-xxhdpi/splash.png", 1600, 960, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-land-xxxhdpi/splash.png", 1920, 1280, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-port-mdpi/splash.png", 320, 480, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-port-hdpi/splash.png", 480, 800, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-port-xhdpi/splash.png", 720, 1280, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-port-xxhdpi/splash.png", 960, 1600, phoneSplash},
		{"mobile/android/app/src/main/res/drawable-port-xxxhdpi/splash.png", 1280, 1920, phoneSplash},
		{"mobile/ios/App/App/Assets.xcassets/Splash.imageset/splash-2732x2732.png", 2732, 2732, phoneSplash},
		{"mobile/ios/App/App/Assets.xcassets/Splash.imageset/splash-2732x2732-1.png", 2732, 2732, phoneSplash},
		{"mobile/ios/App/App/Assets.xcassets/Splash.imageset/splash-2732x2732-2.png", 2732, 2732, phoneSplash},
	}
	ios := []phoneIconSlot{
		{"mobile/ios/App/App/Assets.xcassets/AppIcon.appiconset/AppIcon-512@2x.png", 1024, 1024, phoneLauncher},
	}
	return append(append(ios, android...), splash...)
}

// WritePhoneIcons paints the desktop app mark into the Capacitor iOS and
// Android launcher / splash slots. cap add left the default cyan lattice
// there; the phone must ship the same mark as the Dock.
func WritePhoneIcons(repoRoot string) error {
	mark, err := appMark()
	if err != nil {
		return err
	}
	return writePhoneIconSlots(repoRoot, mark, iconSlots())
}

// iconSlots is the Capacitor tree. Tests swap it so WritePhoneIcons can
// run without painting 2732px splashes under the race detector.
var iconSlots = phoneIconSlots

// appMark is the embedded Dock PNG. Tests swap it to reach the error return.
var appMark = loadAppMark

func loadAppMark() (*image.NRGBA, error) {
	return decodeAppMark(appIcon)
}

func decodeAppMark(raw []byte) (*image.NRGBA, error) {
	src, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("desktop appicon: %w", err)
	}
	return nrgbaOf(src), nil
}

func writePhoneIconSlots(repoRoot string, mark *image.NRGBA, slots []phoneIconSlot) error {
	for _, slot := range slots {
		img := renderPhoneIcon(mark, slot)
		if err := writePNG(filepath.Join(repoRoot, slot.rel), img); err != nil {
			return err
		}
	}
	return nil
}

func renderPhoneIcon(mark *image.NRGBA, slot phoneIconSlot) *image.NRGBA {
	switch slot.kind {
	case phoneForeground:
		content := int(float64(minInt(slot.w, slot.h))*adaptiveSafe + 0.5)
		return fitOnCanvas(mark, slot.w, slot.h, content, false)
	case phoneSplash:
		content := int(float64(minInt(slot.w, slot.h))*splashFrac + 0.5)
		return fitOnCanvas(mark, slot.w, slot.h, content, true)
	default:
		return fitOnCanvas(mark, slot.w, slot.h, minInt(slot.w, slot.h), true)
	}
}

func fitOnCanvas(src *image.NRGBA, cw, ch, content int, opaque bool) *image.NRGBA {
	if content < 1 {
		content = 1
	}
	out := image.NewNRGBA(image.Rect(0, 0, cw, ch))
	if opaque {
		fillWhite(out)
	}
	scaled := scaleNRGBA(src, content, content)
	ox := (cw - content) / 2
	oy := (ch - content) / 2
	for y := 0; y < content; y++ {
		dy := y + oy
		if dy < 0 || dy >= ch {
			continue
		}
		for x := 0; x < content; x++ {
			dx := x + ox
			if dx < 0 || dx >= cw {
				continue
			}
			c := scaled.NRGBAAt(x, y)
			if opaque {
				c = flattenPaper(c)
			} else if isPaper(c) {
				continue
			}
			out.SetNRGBA(dx, dy, c)
		}
	}
	return out
}

func scaleNRGBA(src *image.NRGBA, w, h int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw < 1 || sh < 1 || w < 1 || h < 1 {
		return out
	}
	for y := 0; y < h; y++ {
		sy := (float64(y)+0.5)*float64(sh)/float64(h) - 0.5
		for x := 0; x < w; x++ {
			sx := (float64(x)+0.5)*float64(sw)/float64(w) - 0.5
			out.SetNRGBA(x, y, bilinearNRGBA(src, sx, sy))
		}
	}
	return out
}

func bilinearNRGBA(src *image.NRGBA, x, y float64) color.NRGBA {
	b := src.Bounds()
	maxX := float64(b.Dx() - 1)
	maxY := float64(b.Dy() - 1)
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x > maxX {
		x = maxX
	}
	if y > maxY {
		y = maxY
	}
	x0 := int(x)
	y0 := int(y)
	x1 := x0 + 1
	y1 := y0 + 1
	if x1 > int(maxX) {
		x1 = int(maxX)
	}
	if y1 > int(maxY) {
		y1 = int(maxY)
	}
	fx := x - float64(x0)
	fy := y - float64(y0)
	c00 := src.NRGBAAt(b.Min.X+x0, b.Min.Y+y0)
	c10 := src.NRGBAAt(b.Min.X+x1, b.Min.Y+y0)
	c01 := src.NRGBAAt(b.Min.X+x0, b.Min.Y+y1)
	c11 := src.NRGBAAt(b.Min.X+x1, b.Min.Y+y1)
	return lerpNRGBA(lerpNRGBA(c00, c10, fx), lerpNRGBA(c01, c11, fx), fy)
}

func lerpNRGBA(a, b color.NRGBA, t float64) color.NRGBA {
	return color.NRGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t + 0.5),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t + 0.5),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t + 0.5),
		A: uint8(float64(a.A)*(1-t) + float64(b.A)*t + 0.5),
	}
}

func nrgbaOf(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.SetNRGBA(x-b.Min.X, y-b.Min.Y, color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA))
		}
	}
	return out
}

func fillWhite(img *image.NRGBA) {
	b := img.Bounds()
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetNRGBA(x, y, white)
		}
	}
}

func overWhite(c color.NRGBA) color.NRGBA {
	if isPaper(c) {
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	if c.A == 255 {
		return c
	}
	if c.A == 0 {
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	a := float64(c.A) / 255
	return color.NRGBA{
		R: uint8(float64(c.R)*a + 255*(1-a) + 0.5),
		G: uint8(float64(c.G)*a + 255*(1-a) + 0.5),
		B: uint8(float64(c.B)*a + 255*(1-a) + 0.5),
		A: 255,
	}
}

func flattenPaper(c color.NRGBA) color.NRGBA {
	if isPaper(c) {
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return overWhite(c)
}

func isPaper(c color.NRGBA) bool {
	// 240, not 250: bilinear + FMA can land a paper sample at 249 under
	// `go test -cover` and 250 under `go run`. A cliff there would paint
	// dirty off-white on one build and punch-through on the other.
	return c.R >= 240 && c.G >= 240 && c.B >= 240
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
