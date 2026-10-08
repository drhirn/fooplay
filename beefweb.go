package main

// beefweb.go - client for the REST API of the foobar2000 plugin beefweb.
// Documentation: https://github.com/hyperblast/beefweb (docs/player-api.yml)

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // cover decoding
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"time"
)

type beefwebResponse struct {
	Player struct {
		ActiveItem *struct {
			Columns    []string `json:"columns"`
			Duration   float64  `json:"duration"`
			Index      int      `json:"index"`
			PlaylistID string   `json:"playlistId"`
			Position   float64  `json:"position"`
		} `json:"activeItem"`
		PlaybackMode  int      `json:"playbackMode"`
		PlaybackModes []string `json:"playbackModes"`
		PlaybackState string   `json:"playbackState"`
		Volume        struct {
			IsMuted bool    `json:"isMuted"`
			Value   float64 `json:"value"`
		} `json:"volume"`
	} `json:"player"`
}

type Beefweb struct {
	base   string
	client *http.Client
}

func NewBeefweb(base string) *Beefweb {
	return &Beefweb{
		base:   base,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Fetch queries the current player state. On errors the returned
// ViewModel has State == "error" and a message.
func (b *Beefweb) Fetch() ViewModel {
	vm := ViewModel{FetchedAt: time.Now()}
	trcolumns := url.QueryEscape("%artist%,%title%,%album%,%date%,%tracknumber%")
	u := fmt.Sprintf("%s/api/query?player=true&trcolumns=%s", b.base, trcolumns)

	resp, err := b.client.Get(u)
	if err != nil {
		vm.State = "error"
		vm.ErrText = err.Error()
		return vm
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		vm.State = "error"
		vm.ErrText = fmt.Sprintf("HTTP status %d from %s", resp.StatusCode, b.base)
		return vm
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		vm.State = "error"
		vm.ErrText = err.Error()
		return vm
	}
	var r beefwebResponse
	if err := json.Unmarshal(body, &r); err != nil {
		vm.State = "error"
		vm.ErrText = "unreadable response: " + err.Error()
		return vm
	}

	pl := r.Player
	vm.PlaybackMode = modeName(pl.PlaybackModes, pl.PlaybackMode)
	vm.Volume = pl.Volume.Value
	vm.Muted = pl.Volume.IsMuted
	vm.State = pl.PlaybackState
	if vm.State == "" {
		vm.State = "stopped"
	}

	ai := pl.ActiveItem
	if ai == nil {
		vm.State = "stopped"
		return vm
	}
	vm.Duration = ai.Duration
	vm.Position = ai.Position
	vm.TrackIndex = ai.Index
	vm.PlaylistID = ai.PlaylistID
	get := func(i int) string {
		if i < len(ai.Columns) {
			return ai.Columns[i]
		}
		return ""
	}
	vm.Artist = get(0)
	vm.Title = get(1)
	vm.Album = get(2)
	vm.Year = get(3)
	vm.TrackNo = get(4)
	return vm
}

func modeName(modes []string, i int) string {
	if i >= 0 && i < len(modes) {
		return modes[i]
	}
	return ""
}

// PositionNow extrapolates the position between two polls, so the
// progress bar runs smoothly on the LCD.
func (vm ViewModel) PositionNow() float64 {
	pos := vm.Position
	if vm.State == "playing" {
		pos += time.Since(vm.FetchedAt).Seconds()
	}
	if pos < 0 {
		return 0
	}
	if vm.Duration > 0 && pos > vm.Duration {
		return vm.Duration
	}
	return pos
}

// FetchCover downloads the album cover of the currently playing track.
// ok is false if there is no cover (HTTP 404) or an error occurred.
func (b *Beefweb) FetchCover() (img image.Image, ok bool) {
	resp, err := b.client.Get(b.base + "/api/artwork/current")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	img, _, err = image.Decode(resp.Body)
	if err != nil {
		return nil, false
	}
	return img, true
}
