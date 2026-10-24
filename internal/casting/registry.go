package casting

import (
	"slices"
	"sync"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// Device holds protocol details that are never exposed in the public Target.
type Device struct {
	Target     api.Target
	ControlURL string
	ExpiresAt  time.Time
}

type Registry struct {
	mu      sync.RWMutex
	devices map[string]Device
}

func NewRegistry() *Registry { return &Registry{devices: make(map[string]Device)} }

func clone(d Device) Device {
	d.Target.Endpoints = slices.Clone(d.Target.Endpoints)
	for i := range d.Target.Endpoints {
		c := &d.Target.Endpoints[i].Capabilities
		c.Formats = slices.Clone(c.Formats)
		c.VideoCodecs = slices.Clone(c.VideoCodecs)
		c.AudioCodecs = slices.Clone(c.AudioCodecs)
	}
	return d
}

func (r *Registry) Upsert(d Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[d.Target.ID] = clone(d)
}

func (r *Registry) Get(id string) (Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.devices[id]
	if !d.ExpiresAt.IsZero() && !time.Now().Before(d.ExpiresAt) {
		d.Target.State = api.TargetOffline
	}
	return clone(d), ok
}

func (r *Registry) List() []api.Target {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]api.Target, 0, len(r.devices))
	for _, d := range r.devices {
		if !d.ExpiresAt.IsZero() && !time.Now().Before(d.ExpiresAt) {
			d.Target.State = api.TargetOffline
		}
		out = append(out, clone(d).Target)
	}
	slices.SortFunc(out, func(a, b api.Target) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out
}
