package casting

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/casting/dlna"
	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// Config wires the device's own casting to the local network.
type Config struct {
	// LocalIP is the IPv4 address SSDP is sent from. Empty uses the route the
	// kernel picks.
	LocalIP net.IP
	// Window bounds one discovery scan. Zero uses three seconds, the same
	// value the firmware and the old casting service waited.
	Window time.Duration
	// Client carries SOAP requests. Nil uses http.DefaultClient.
	Client *http.Client
}

// Caster is the device's casting implementation. It talks to renderers on the
// device's own LAN and keeps no state outside this process: there is no
// casting service to call.
type Caster interface {
	// List discovers renderers on the local network.
	List(ctx context.Context) ([]api.Target, error)
	// Resolve returns a cached screen by ID, or by an unambiguous name.
	Resolve(ctx context.Context, ref string) (api.Target, error)
	// Cast starts streamURL on the screen and returns the screen it used.
	Cast(ctx context.Context, targetID, streamURL string) (api.Target, error)
	// Stop stops the screen recorded by the most recent Cast. An empty
	// targetID means that screen; any other value must match it.
	Stop(ctx context.Context, targetID string) error
}

// Sentinel errors the tools turn into speakable Chinese.
var (
	ErrScreenNotFound = errors.New("casting: screen not found")
	ErrAmbiguousName  = errors.New("casting: screen name matches several screens")
	ErrNotCasting     = errors.New("casting: no screen is casting")
	ErrSessionGone    = errors.New("casting: the screen is no longer casting")
)

// discovery and control are the two network boundaries, kept as interfaces so
// unit tests can reproduce renderers without sending multicast.
type discovery interface {
	Scan(ctx context.Context) ([]dlna.Device, error)
}

type control interface {
	Start(ctx context.Context, device dlna.Device, source api.StreamSource) error
	Stop(ctx context.Context, device dlna.Device) error
}

// New returns a Caster that discovers and controls DLNA renderers directly.
func New(cfg Config) Caster {
	window := cfg.Window
	if window <= 0 {
		window = 3 * time.Second
	}
	return &siteCaster{
		discovery: &dlna.Discoverer{LocalIP: cfg.LocalIP, Window: window, Client: cfg.Client},
		control:   &dlna.Transport{Client: cfg.Client},
		devices:   map[string]dlna.Device{},
		targets:   map[string]api.Target{},
	}
}

type siteCaster struct {
	discovery discovery
	control   control

	mu      sync.Mutex
	devices map[string]dlna.Device
	targets map[string]api.Target
	last    string // screen the most recent Cast used
}

// List scans the LAN and replaces the cached screens with what answered.
func (c *siteCaster) List(ctx context.Context) ([]api.Target, error) {
	devices, err := c.discovery.Scan(ctx)
	if err != nil && len(devices) == 0 {
		return nil, err
	}
	return c.remember(devices), nil
}

// remember stores one scan's devices and returns their targets, sorted so the
// spoken list is stable between calls.
func (c *siteCaster) remember(devices []dlna.Device) []api.Target {
	targets := make([]api.Target, 0, len(devices))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.devices = make(map[string]dlna.Device, len(devices))
	c.targets = make(map[string]api.Target, len(devices))
	for _, device := range devices {
		target := device.Target
		if strings.TrimSpace(target.ID) == "" {
			continue
		}
		c.devices[target.ID] = device
		c.targets[target.ID] = target
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Name != targets[j].Name {
			return targets[i].Name < targets[j].Name
		}
		return targets[i].ID < targets[j].ID
	})
	return targets
}

// Resolve looks a screen up in the last scan. The model often repeats the name
// it just heard, so a unique name is accepted; a stale reference triggers one
// fresh scan before giving up.
func (c *siteCaster) Resolve(ctx context.Context, ref string) (api.Target, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return api.Target{}, ErrScreenNotFound
	}
	if target, err := c.lookup(ref); err == nil {
		return target, nil
	} else if !errors.Is(err, ErrScreenNotFound) {
		return api.Target{}, err
	}
	if _, err := c.List(ctx); err != nil {
		return api.Target{}, err
	}
	return c.lookup(ref)
}

func (c *siteCaster) lookup(ref string) (api.Target, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if target, ok := c.targets[ref]; ok {
		return target, nil
	}
	var (
		match api.Target
		hits  int
	)
	for _, target := range c.targets {
		if strings.TrimSpace(target.Name) != ref {
			continue
		}
		match = target
		hits++
	}
	switch hits {
	case 0:
		return api.Target{}, ErrScreenNotFound
	case 1:
		return match, nil
	default:
		return api.Target{}, ErrAmbiguousName
	}
}

// Cast sends SetAVTransportURI and Play, then records the screen as the one a
// later Stop without an ID refers to.
func (c *siteCaster) Cast(ctx context.Context, targetID, streamURL string) (api.Target, error) {
	target, err := c.Resolve(ctx, targetID)
	if err != nil {
		return api.Target{}, err
	}
	device, ok := c.device(target.ID)
	if !ok {
		if _, err := c.List(ctx); err != nil {
			return api.Target{}, err
		}
		if device, ok = c.device(target.ID); !ok {
			return api.Target{}, ErrScreenNotFound
		}
	}
	source := api.StreamSource{
		Type:       "stream",
		URL:        streamURL,
		Format:     streamFormat,
		VideoCodec: videoCodec,
		AudioCodec: audioCodec,
	}
	if err := c.control.Start(ctx, device, source); err != nil {
		return api.Target{}, err
	}
	c.mu.Lock()
	c.last = target.ID
	c.mu.Unlock()
	return target, nil
}

// Stop stops the screen recorded by the most recent Cast. UPnP 701 ("transition
// not available") means the screen is already stopped, which is success.
func (c *siteCaster) Stop(ctx context.Context, targetID string) error {
	targetID = strings.TrimSpace(targetID)
	c.mu.Lock()
	last := c.last
	c.mu.Unlock()
	if targetID == "" {
		if last == "" {
			return ErrNotCasting
		}
		targetID = last
	}
	if last == "" {
		// Nothing was cast since this process started; treat it as stopped
		// rather than asking the TV to stop something we never started.
		return nil
	}
	if targetID != last {
		return ErrSessionGone
	}
	device, ok := c.device(targetID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrScreenNotFound, targetID)
	}
	if err := c.control.Stop(ctx, device); err != nil {
		if upnpCode(err) == "701" {
			c.forget(targetID)
			return nil
		}
		return err
	}
	c.forget(targetID)
	return nil
}

func (c *siteCaster) device(id string) (dlna.Device, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	device, ok := c.devices[id]
	return device, ok
}

func (c *siteCaster) forget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == id {
		c.last = ""
	}
}

// upnpCode extracts the errorCode from a SOAP fault the transport reported as
// "action: UPnP 715 ...".
func upnpCode(err error) string {
	if err == nil {
		return ""
	}
	const marker = "UPnP "
	index := strings.Index(err.Error(), marker)
	if index < 0 {
		return ""
	}
	fields := strings.Fields(err.Error()[index+len(marker):])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
