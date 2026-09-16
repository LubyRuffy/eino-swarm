package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// macIconCorner is Apple's app-icon corner radius as a fraction of the
// content square. setApplicationIconImage will not apply this mask for us.
const macIconCorner = 0.2237

// macIconContent is Apple's macOS app-icon grid: the squircle is 824px
// on a 1024px canvas. A bundled .app gets that inset for free. A naked
// `go run` binary does not, so a full-bleed PNG looks larger in the Dock
// than the same mark in ZWAI.app.
const macIconContent = 824.0 / 1024.0

func roundPNG(src []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	r := macIconRadius(w, h)
	if r <= 0 {
		return src, nil
	}
	ox, oy, cw, ch := macIconContentRect(w, h)
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px := float64(x) + 0.5
			py := float64(y) + 0.5
			cover := roundedRectCoverage(px-ox, py-oy, cw, ch, r)
			if cover <= 0 {
				continue
			}
			sx := (px - ox) / macIconContent
			sy := (py - oy) / macIconContent
			c := sampleNRGBA(img, b, sx, sy)
			c.A = uint8(float64(c.A)*cover + 0.5)
			out.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func macIconContentRect(w, h int) (ox, oy, cw, ch float64) {
	cw = float64(w) * macIconContent
	ch = float64(h) * macIconContent
	ox = (float64(w) - cw) / 2
	oy = (float64(h) - ch) / 2
	return ox, oy, cw, ch
}

func macIconRadius(w, h int) float64 {
	if w < 2 || h < 2 {
		return 0
	}
	_, _, cw, ch := macIconContentRect(w, h)
	short := cw
	if ch < cw {
		short = ch
	}
	if short < 2 {
		return 0
	}
	return short * macIconCorner
}

func sampleNRGBA(img image.Image, b image.Rectangle, sx, sy float64) color.NRGBA {
	x := b.Min.X + clampInt(int(math.Floor(sx)), 0, b.Dx()-1)
	y := b.Min.Y + clampInt(int(math.Floor(sy)), 0, b.Dy()-1)
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func roundedRectCoverage(cx, cy, w, h, r float64) float64 {
	if w <= 0 || h <= 0 {
		return 0
	}
	dist := roundedBoxSDF(cx-w/2, cy-h/2, w/2, h/2, r)
	v := 0.5 - dist
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

func roundedBoxSDF(px, py, hx, hy, r float64) float64 {
	if r < 0 {
		r = 0
	}
	if r > hx {
		r = hx
	}
	if r > hy {
		r = hy
	}
	qx := math.Abs(px) - (hx - r)
	qy := math.Abs(py) - (hy - r)
	ox, oy := qx, qy
	if ox < 0 {
		ox = 0
	}
	if oy < 0 {
		oy = 0
	}
	return math.Hypot(ox, oy) + math.Min(math.Max(qx, qy), 0) - r
}
