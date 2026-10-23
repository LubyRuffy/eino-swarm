package desktop

import (
	"encoding/hex"
	"image/color"
	"strings"
	"testing"
)

func probePixels(challenge string, first, second color.NRGBA, left, top int, flipped bool) []byte {
	bytes, _ := hex.DecodeString(challenge)
	pixels := make([]byte, recoverySnapshotSide*recoverySnapshotSide*4)
	for bit := 0; bit < 64; bit++ {
		row := bit / 8
		if flipped {
			row = 7 - row
		}
		ink := first
		if bytes[bit/8]&(1<<uint(7-bit%8)) != 0 {
			ink = second
		}
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				offset := ((top+row*2+y)*recoverySnapshotSide + left + (bit%8)*2 + x) * 4
				copy(pixels[offset:], []byte{ink.R, ink.G, ink.B, ink.A})
			}
		}
	}
	return pixels
}

func TestRendererPaintProofRequiresEveryOrderedChallengeBit(t *testing.T) {
	first := color.NRGBA{R: 50, G: 70, B: 90, A: 255}
	second := color.NRGBA{R: 205, G: 185, B: 165, A: 255}
	challenge := "a53c9e1172d4b608"
	for _, position := range [][2]int{{0, 0}, {7, 11}, {48, 48}} {
		for _, flipped := range []bool{false, true} {
			pixels := probePixels(challenge, first, second, position[0], position[1], flipped)
			if !snapshotMatchesProbe(pixels, first, second, challenge) {
				t.Fatalf("current ordered pixels rejected at %v flipped=%v", position, flipped)
			}
			for bit := 0; bit < 64; bit++ {
				changed, _ := hex.DecodeString(challenge)
				changed[bit/8] ^= 1 << uint(7-bit%8)
				if snapshotMatchesProbe(pixels, first, second, hex.EncodeToString(changed)) {
					t.Fatalf("stale nonce bit %d proved the current renderer healthy", bit)
				}
			}
		}
	}
}

func TestNearbyPreviousPaintColoursCannotProveADifferentNonce(t *testing.T) {
	oldFirst := color.NRGBA{R: 40, G: 60, B: 80, A: 255}
	oldSecond := color.NRGBA{R: 215, G: 195, B: 175, A: 255}
	first := color.NRGBA{R: 50, G: 70, B: 90, A: 255}
	second := color.NRGBA{R: 205, G: 185, B: 165, A: 255}
	pixels := probePixels("a53c9e1172d4b608", oldFirst, oldSecond, 0, 0, false)
	if snapshotMatchesProbe(pixels, first, second, "a53c9e1172d4b609") {
		t.Fatal("a prior nonce passed because both colour pairs were within tolerance")
	}
	if !snapshotMatchesProbe(pixels, first, second, "a53c9e1172d4b608") {
		t.Fatal("the nonce checker lost the native colour tolerance")
	}
}

func TestEveryNonceCellNeedsEnoughOpaqueMatchingPixels(t *testing.T) {
	first := color.NRGBA{R: 50, G: 70, B: 90, A: 255}
	second := color.NRGBA{R: 205, G: 185, B: 165, A: 255}
	challenge := "a53c9e1172d4b608"
	pixels := probePixels(challenge, first, second, 0, 0, false)
	pixels[3] = 0
	if !snapshotMatchesProbe(pixels, first, second, challenge) {
		t.Fatal("one antialiased cell pixel prevented native paint acceptance")
	}
	pixels[7] = 0
	if snapshotMatchesProbe(pixels, first, second, challenge) {
		t.Fatal("a half-painted nonce cell was accepted")
	}
	for _, bad := range []string{"", "nonce", "abcd", "000000000000000000", "zzzzzzzzzzzzzzzz"} {
		if snapshotMatchesProbe(pixels, first, second, bad) {
			t.Fatalf("invalid nonce accepted: %q", bad)
		}
	}
	if snapshotMatchesProbe(nil, first, second, challenge) {
		t.Fatal("missing native pixels were accepted")
	}
	if snapshotMatchesProbe(probePixels(challenge, first, first, 0, 0, false), first, first, challenge) {
		t.Fatal("indistinguishable palette colours cannot encode nonce bits")
	}
}

func TestNativePaintProbeEncodesTheWholeNonceWithoutSVG(t *testing.T) {
	script := recoveryProbeScript("http://127.0.0.1:1234", "session", 2, "a53c9e1172d4b608", color.NRGBA{R: 40, G: 60, B: 80}, color.NRGBA{R: 215, G: 195, B: 175})
	for _, want := range []string{"repeat(8,2px)", "width:16px;height:16px", "i<64", "7-(i&7)", "requestAnimationFrame", "a53c9e1172d4b608"} {
		if !strings.Contains(script, want) {
			t.Fatalf("probe omits %q", want)
		}
	}
	if strings.Contains(strings.ToLower(script), "svg") || strings.Count(script, "rgb(") != 2 {
		t.Fatal("probe needs simple native pixels and two inspectable colour constants")
	}
}
