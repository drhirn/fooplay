package main

// font.go - text engine based on DejaVu Sans Bold, embedded into the
// binary (strong strokes, designed for low-resolution rendering).
// Anti-aliased and arbitrarily scalable, so it stays crisp on
// Full-HD LCDs as well. The font is bold by itself - no simulated
// overdraw, so no gray halos on 1-bit displays.

import (
	_ "embed"
	"image"
	"image/color"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed DejaVuSans-Bold.ttf
var fontData []byte

var (
	goFont    *opentype.Font
	faceMu    sync.Mutex
	faceCache = map[int]font.Face{}
)

func init() {
	f, err := opentype.Parse(fontData)
	if err != nil {
		panic(err)
	}
	goFont = f
}

// getFace returns a cached font face for the requested size in pixels.
func getFace(sizePx float64) font.Face {
	key := int(sizePx * 4) // 0.25 px steps are sufficient
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[key]; ok {
		return f
	}
	f, err := opentype.NewFace(goFont, &opentype.FaceOptions{
		Size: sizePx, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		panic(err)
	}
	faceCache[key] = f
	return f
}

func textWidth(s string, sizePx float64, bold bool) float64 {
	return float64(font.MeasureString(getFace(sizePx), s).Ceil())
}

func fitText(s string, sizePx, maxWidth float64, bold bool) string {
	if textWidth(s, sizePx, bold) <= maxWidth {
		return s
	}
	r := []rune(s)
	for len(r) > 4 && textWidth(string(r)+"...", sizePx, bold) > maxWidth {
		r = r[:len(r)-1]
	}
	return string(r) + "..."
}

// drawText draws s; y is the TOP of the line. maxWidth in pixels.
func drawText(img *image.RGBA, x, y float64, s string, sizePx, maxWidth float64, c color.Color, bold bool) {
	s = fitText(s, sizePx, maxWidth, bold)
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: getFace(sizePx),
	}
	d.Dot = fixed.P(int(x), int(y+sizePx*0.88))
	d.DrawString(s)
}

// wrapText breaks s at spaces so that no line exceeds maxWidth.
// maxLines limits the number of lines; if exceeded, the overflow is
// merged into the last line and truncated with "...".
func wrapText(s string, sizePx, maxWidth float64, bold bool, maxLines int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := ""
	for _, w := range words {
		trial := w
		if cur != "" {
			trial = cur + " " + w
		}
		if textWidth(trial, sizePx, bold) <= maxWidth {
			cur = trial
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		cur = w
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > maxLines {
		last := fitText(strings.Join(lines[maxLines-1:], " "), sizePx, maxWidth, bold)
		lines = append(lines[:maxLines-1], last)
	}
	return lines
}
