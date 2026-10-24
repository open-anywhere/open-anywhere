// Package screen maps Linx screen IDs to casting targets and selects endpoints
// for the media Render produces.
package screen

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

// Screen is one physical screen configured for the control plane.
type Screen struct {
	ID       string `json:"screen_id"`
	Name     string `json:"name"`
	TargetID string `json:"target_id"`
}

// Catalog is the configured screen mapping.
type Catalog struct {
	screens []Screen
	byID    map[string]Screen
}

// LoadCatalog reads a JSON array of screens. An empty path yields an empty
// catalog; callers may fall back to discovered casting targets.
func LoadCatalog(path string) (*Catalog, error) {
	catalog := &Catalog{byID: make(map[string]Screen)}
	if path == "" {
		return catalog, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("screens: %w", err)
	}
	var screens []Screen
	if err := json.Unmarshal(data, &screens); err != nil {
		return nil, fmt.Errorf("screens: invalid JSON: %w", err)
	}
	for i, item := range screens {
		item.ID = trimSpace(item.ID)
		item.Name = trimSpace(item.Name)
		item.TargetID = trimSpace(item.TargetID)
		if item.ID == "" || item.TargetID == "" {
			return nil, fmt.Errorf("screens: entry %d needs screen_id and target_id", i)
		}
		if _, exists := catalog.byID[item.ID]; exists {
			return nil, fmt.Errorf("screens: duplicate screen_id %q", item.ID)
		}
		if item.Name == "" {
			item.Name = item.ID
		}
		screens[i] = item
		catalog.byID[item.ID] = item
	}
	catalog.screens = screens
	return catalog, nil
}

// Get returns the configured screen.
func (c *Catalog) Get(id string) (Screen, bool) {
	if c == nil {
		return Screen{}, false
	}
	item, ok := c.byID[id]
	return item, ok
}

// List returns all configured screens in file order.
func (c *Catalog) List() []Screen {
	if c == nil {
		return nil
	}
	return append([]Screen(nil), c.screens...)
}

// Empty reports whether no screen mapping was configured.
func (c *Catalog) Empty() bool { return c == nil || len(c.screens) == 0 }

// Supports reports whether endpoint can carry source.
func Supports(endpoint casting.Endpoint, source casting.StreamSource) bool {
	cap := endpoint.Capabilities
	if !cap.Streaming || !slices.Contains(cap.Formats, source.Format) || !slices.Contains(cap.VideoCodecs, source.VideoCodec) {
		return false
	}
	if source.AudioCodec != "" && !slices.Contains(cap.AudioCodecs, source.AudioCodec) {
		return false
	}
	return true
}

// SelectEndpoint returns the first endpoint able to carry source.
func SelectEndpoint(target casting.Target, source casting.StreamSource) (casting.Endpoint, bool) {
	for _, endpoint := range target.Endpoints {
		if Supports(endpoint, source) {
			return endpoint, true
		}
	}
	return casting.Endpoint{}, false
}

// DiscoverableEndpoint reports whether the target has any endpoint that can
// carry the video profile Render produces.
func DiscoverableEndpoint(target casting.Target) (casting.Endpoint, bool) {
	return SelectEndpoint(target, casting.StreamSource{Type: "stream", Format: "mpegts", VideoCodec: "h264"})
}

func trimSpace(value string) string {
	for len(value) > 0 && (value[0] == ' ' || value[0] == '\t' || value[0] == '\n' || value[0] == '\r') {
		value = value[1:]
	}
	for len(value) > 0 {
		last := value[len(value)-1]
		if last != ' ' && last != '\t' && last != '\n' && last != '\r' {
			break
		}
		value = value[:len(value)-1]
	}
	return value
}
