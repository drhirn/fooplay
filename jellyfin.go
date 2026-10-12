package main

// jellyfin.go - client for the Jellyfin media server REST API. It
// implements the same PlayerSource interface as the beefweb client,
// so rendering and displays stay source-agnostic.
//
// Authentication: an API key with admin rights (Dashboard ->
// Advanced -> API keys). Sent as X-Emby-Token header.

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // cover decoding
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type jellyfinSession struct {
	UserID   string `json:"UserId"`
	UserName string `json:"UserName"`
	NowPlayingItem *struct {
		Name           string   `json:"Name"`
		Album          string   `json:"Album"`
		Artists        []string `json:"Artists"`
		AlbumArtist    string   `json:"AlbumArtist"`
		ProductionYear int      `json:"ProductionYear"`
		ID             string   `json:"Id"`
		Type           string   `json:"Type"`
		RunTimeTicks   int64    `json:"RunTimeTicks"`
	} `json:"NowPlayingItem"`
	PlayState *struct {
		IsPaused      bool  `json:"IsPaused"`
		PositionTicks int64 `json:"PositionTicks"`
	} `json:"PlayState"`
}

// Jellyfin polls /Sessions and reports the first session that is
// currently playing audio. -jellyfin-user restricts the search to a
// specific user (name or id) when several people listen at once.
type Jellyfin struct {
	base   string
	apiKey string
	user   string // optional UserName or UserId filter
	client *http.Client
	itemID string // item from the last successful Fetch (for the cover)
}

func NewJellyfin(base, apiKey, user string) *Jellyfin {
	return &Jellyfin{
		base:   strings.TrimRight(base, "/"),
		apiKey: apiKey,
		user:   user,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (j *Jellyfin) get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", j.base+url, nil)
	if err != nil {
		return nil, err
	}
	// Both header variants: some Jellyfin versions reject X-Emby-Token
	// for API keys but accept the MediaBrowser authorization header.
	req.Header.Set("X-Emby-Token", j.apiKey)
	req.Header.Set("Authorization", `MediaBrowser Token="`+j.apiKey+`"`)
	return j.client.Do(req)
}

const ticksPerSecond = 10000000 // 1 Jellyfin tick = 100 ns

// Fetch queries all sessions and maps the matching one to a ViewModel.
// On errors State is "error" with a message; when nobody is playing
// audio, State is "stopped".
func (j *Jellyfin) Fetch() ViewModel {
	vm := ViewModel{FetchedAt: time.Now()}
	j.itemID = ""

	resp, err := j.get("/Sessions")
	if err != nil {
		vm.State = "error"
		vm.ErrText = err.Error()
		return vm
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		vm.State = "error"
		vm.ErrText = fmt.Sprintf("HTTP status %d from %s", resp.StatusCode, j.base)
		return vm
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		vm.State = "error"
		vm.ErrText = err.Error()
		return vm
	}
	var sessions []jellyfinSession
	if err := json.Unmarshal(body, &sessions); err != nil {
		vm.State = "error"
		vm.ErrText = "unreadable response: " + err.Error()
		return vm
	}

	for _, s := range sessions {
		item := s.NowPlayingItem
		if item == nil || item.Type != "Audio" {
			continue
		}
		if j.user != "" && s.UserName != j.user && s.UserID != j.user {
			continue
		}
		vm.State = "playing"
		vm.Title = item.Name
		vm.Album = item.Album
		if len(item.Artists) > 0 {
			vm.Artist = strings.Join(item.Artists, ", ")
		} else {
			vm.Artist = item.AlbumArtist
		}
		if item.ProductionYear > 0 {
			vm.Year = strconv.Itoa(item.ProductionYear)
		}
		vm.Duration = float64(item.RunTimeTicks) / ticksPerSecond
		vm.PlaylistID = item.ID
		if s.PlayState != nil {
			vm.Position = float64(s.PlayState.PositionTicks) / ticksPerSecond
			if s.PlayState.IsPaused {
				vm.State = "paused"
			}
		}
		j.itemID = item.ID
		return vm
	}
	vm.State = "stopped"
	return vm
}

// FetchCover downloads the album art of the item from the last Fetch.
func (j *Jellyfin) FetchCover() (image.Image, bool) {
	if j.itemID == "" {
		return nil, false
	}
	resp, err := j.get(fmt.Sprintf("/Items/%s/Images/Primary?maxWidth=600&quality=90", j.itemID))
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, false
	}
	return img, true
}
