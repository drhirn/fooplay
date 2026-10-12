package main

// multisource.go - combines two player sources with priority: the
// primary (foobar2000 via beefweb) wins whenever it is playing; on
// stop, pause or connection problems the secondary (jellyfin) is
// consulted instead.

import "image"

type fallbackSource struct {
	primary   PlayerSource
	secondary PlayerSource
	last      PlayerSource // source that produced the last Fetch
}

func (m *fallbackSource) Fetch() ViewModel {
	vm := m.primary.Fetch()
	if vm.State == "playing" {
		m.last = m.primary
		return vm
	}
	vm2 := m.secondary.Fetch()
	if vm2.State == "playing" {
		m.last = m.secondary
		return vm2
	}
	// Neither is playing. Preserve "paused" (the display keeps the
	// paused song's image); report "stopped" only when both sources are
	// idle/reachable, and an error only when both are unreachable.
	if vm.State == "paused" {
		m.last = m.primary
		return vm
	}
	if vm2.State == "paused" {
		m.last = m.secondary
		return vm2
	}
	if vm.State != "error" || vm2.State != "error" {
		m.last = m.primary
		vm.State = "stopped"
		return vm
	}
	m.last = m.primary
	return vm
}

// FetchCover returns the cover of the source that served the last
// Fetch (its current item may have changed since).
func (m *fallbackSource) FetchCover() (image.Image, bool) {
	if m.last == nil {
		m.last = m.primary
	}
	return m.last.FetchCover()
}
