package main

// render.go - draws the whole screen: cover on the left, artist, title,
// album and year stacked next to it, progress bar and status icon below.
// All sizes are relative to the resolution, so it is crisp on 800x480
// (e-paper) as well as on Full-HD LCDs.

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	xdraw "golang.org/x/image/draw"
)

type ViewModel struct {
	State        string // "playing", "paused", "stopped", "error"
	ErrText      string
	Artist       string
	Title        string
	Album        string
	Year         string
	TrackNo      string
	Duration     float64
	Position     float64
	Volume       float64
	Muted        bool
	PlaybackMode string
	TrackIndex   int
	PlaylistID   string
	FetchedAt    time.Time
	Cover        image.Image
}

// Key identifies the structural state (for the e-paper full refresh).
// Progress (position) is intentionally NOT included.
func (vm ViewModel) Key() string {
	if vm.State == "error" {
		return "error|" + vm.ErrText
	}
	return fmt.Sprintf("%s|%d|%s|%s|%s|%s|vol:%.0f|mode:%s",
		vm.PlaylistID, vm.TrackIndex, vm.State,
		vm.Artist, vm.Title, vm.Album, vm.Volume, vm.PlaybackMode)
}

// TrackKey identifies the track (for cover caching).
func (vm ViewModel) TrackKey() string {
	return fmt.Sprintf("%s|%d|%s|%s", vm.PlaylistID, vm.TrackIndex, vm.Artist, vm.Title)
}

// ---------- helpers ----------

func fillRect(img *image.RGBA, x, y, w, h int, c color.Color) {
	r := image.Rect(x, y, x+w, y+h).Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	draw.Draw(img, r, &image.Uniform{c}, image.Point{}, draw.Src)
}

// render draws the screen and additionally returns the rectangle of the
// cover art: that region is dithered when converted for the e-paper,
// everything else (text) is thresholded sharply.
func render(vm ViewModel, W, H int, invert bool) (*image.RGBA, image.Rectangle) {
	coverRect := image.Rectangle{}
	bg := color.RGBA{255, 255, 255, 255}
	fg := color.RGBA{0, 0, 0, 255}
	if invert {
		bg, fg = fg, bg
	}
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	m := float64(W) / 40.0 // outer margin

	// Connection error, stopped state or empty metadata: blank screen
	// in the background color (white by default, black with -invert)
	if vm.State == "error" || vm.State == "stopped" || (vm.Title == "" && vm.Artist == "") {
		return img, coverRect
	}

	// Cover on the left, artist/album/year stacked to its right in the
	// upper zone; the song title spans the full width below.
	sArtist := float64(H) * 0.10
	sAlbum := float64(H) * 0.08
	sYear := float64(H) * 0.07
	sTitle := float64(H) * 0.15

	// title area at the bottom (up to two lines)
	titleLines := wrapText(vm.Title, sTitle, float64(W)-2*m, true, 2)
	titleBlock := float64(len(titleLines)) * sTitle * 1.15

	// upper zone with cover + info, vertically centered
	contentTop := m
	contentBot := float64(H) - m - titleBlock - m*0.5
	zoneH := contentBot - contentTop

	coverSize := zoneH
	if maxCover := float64(H) * 0.62; coverSize > maxCover {
		coverSize = maxCover
	}
	coverX := m
	coverY := contentTop + (zoneH-coverSize)/2
	coverRect = image.Rect(int(coverX), int(coverY), int(coverX+coverSize), int(coverY+coverSize))
	drawCover(img, vm.Cover, coverX, coverY, coverSize, bg, fg)

	tx := coverX + coverSize + 2*m
	tw := float64(W) - tx - m
	if tw < 80 { // very narrow display
		tx = m
		tw = float64(W) - 2*m
	}

	infos := []textLine{{vm.Artist, sArtist, true}, {vm.Album, sAlbum, false}}
	if vm.Year != "" {
		infos = append(infos, textLine{vm.Year, sYear, false})
	}
	ib := 0.0
	for _, l := range infos {
		ib += l.size * 1.15
	}
	iy := contentTop + (zoneH-ib)/2
	for _, l := range infos {
		drawText(img, tx, iy, l.s, l.size, tw, fg, l.bold)
		iy += l.size * 1.15
	}

	// title below cover and info, full width
	ty := contentBot + m*0.5
	for _, l := range titleLines {
		drawText(img, m, ty, l, sTitle, float64(W)-2*m, fg, true)
		ty += sTitle * 1.15
	}

	return img, coverRect
}

// textLine describes one line in the text block next to the cover.
type textLine struct {
	s    string
	size float64
	bold bool
}

// drawCover draws the album cover (or a vinyl placeholder) without a
// frame, directly onto the area.
func drawCover(img *image.RGBA, cover image.Image, x, y, size float64, bg, fg color.Color) {
	xi, yi, si := int(x), int(y), int(size)
	if cover != nil {
		// crop a centered square from the image, then scale
		sb := cover.Bounds()
		w, h := sb.Dx(), sb.Dy()
		switch {
		case w > h:
			sb = image.Rect(sb.Min.X+(w-h)/2, sb.Min.Y, sb.Min.X+(w+h)/2, sb.Max.Y)
		case h > w:
			sb = image.Rect(sb.Min.X, sb.Min.Y+(h-w)/2, sb.Max.X, sb.Min.Y+(h+w)/2)
		}
		dst := image.NewRGBA(image.Rect(0, 0, si, si))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), cover, sb, draw.Src, nil)
		draw.Draw(img, image.Rect(xi, yi, xi+si, yi+si), dst, image.Point{}, draw.Src)
		return
	}
	// placeholder: vinyl record (disc + center hole)
	cx := float64(xi+si) / 2
	cy := float64(yi+si) / 2
	for _, ring := range []struct {
		r float64
		c color.Color
	}{
		{float64(si) * 0.38, fg},
		{float64(si) * 0.10, bg},
	} {
		for dy := -ring.r; dy <= ring.r; dy++ {
			hw := math.Sqrt(ring.r*ring.r - dy*dy)
			fillRect(img, int(cx-hw), int(cy+dy), int(2*hw)+1, 1, ring.c)
		}
	}
}

