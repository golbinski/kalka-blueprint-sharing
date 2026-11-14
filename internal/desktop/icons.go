package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// TrayIcon draws the tray icon for a state, so the paused and offline
// states are visible at a glance (spec 22.3). template is the macOS menu
// bar form: black with alpha, which the system tints; elsewhere the icon is
// coloured. The shape carries the state without colour: a filled dot
// online, a ring offline, two bars paused, a small dot starting.
func TrayIcon(state TrayState, template bool) []byte {
	const size = 44
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	ink := color.NRGBA{0, 0, 0, 255}
	if !template {
		switch state {
		case TrayOnline:
			ink = color.NRGBA{0x2f, 0x6f, 0xd0, 255}
		case TrayPaused:
			ink = color.NRGBA{0xc2, 0x7c, 0x0e, 255}
		default:
			ink = color.NRGBA{0x6b, 0x72, 0x80, 255}
		}
	}
	c := float64(size) / 2
	// The frame: a ring around every state.
	disc(img, c, c, 20, 17, ink)
	switch state {
	case TrayOnline:
		disc(img, c, c, 10, 0, ink)
	case TrayOffline:
		disc(img, c, c, 10, 7, ink)
	case TrayPaused:
		bar(img, 15, 13, 20, 31, ink)
		bar(img, 24, 13, 29, 31, ink)
	default:
		disc(img, c, c, 4, 0, ink)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// disc fills the annulus between inner and outer radius, anti-aliased.
func disc(img *image.NRGBA, cx, cy, outer, inner float64, ink color.NRGBA) {
	r := img.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			a := clamp(outer-d+0.5) * clamp(d-inner+0.5)
			if inner == 0 {
				a = clamp(outer - d + 0.5)
			}
			blend(img, x, y, ink, a)
		}
	}
}

func bar(img *image.NRGBA, x0, y0, x1, y1 int, ink color.NRGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			blend(img, x, y, ink, 1)
		}
	}
}

func blend(img *image.NRGBA, x, y int, ink color.NRGBA, a float64) {
	if a <= 0 {
		return
	}
	old := img.NRGBAAt(x, y)
	na := uint8(math.Min(255, float64(old.A)+a*255))
	img.SetNRGBA(x, y, color.NRGBA{ink.R, ink.G, ink.B, na})
}

func clamp(v float64) float64 { return math.Max(0, math.Min(1, v)) }
