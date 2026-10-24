package screen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

func TestLoadCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screens.json")
	data := `[{"screen_id":"tv","name":"客厅","target_id":"target-1"},{"screen_id":"study","target_id":"target-2"}]`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	item, ok := catalog.Get("tv")
	if !ok || item.TargetID != "target-1" || item.Name != "客厅" {
		t.Fatalf("screen = %+v, %v", item, ok)
	}
	study, _ := catalog.Get("study")
	if study.Name != "study" {
		t.Fatalf("missing name should default to id: %+v", study)
	}
	if len(catalog.List()) != 2 || catalog.Empty() {
		t.Fatalf("catalog = %+v", catalog.List())
	}
}

func TestLoadCatalogEmpty(t *testing.T) {
	catalog, err := LoadCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.Empty() || catalog.List() != nil {
		t.Fatal("empty path should yield an empty catalog")
	}
	if _, ok := catalog.Get("x"); ok {
		t.Fatal("empty catalog should not resolve")
	}
	var nilCatalog *Catalog
	if nilCatalog.List() != nil || !nilCatalog.Empty() {
		t.Fatal("nil catalog")
	}
}

func TestLoadCatalogRejectsInvalid(t *testing.T) {
	tests := map[string]string{
		"invalid json":   `{`,
		"missing id":     `[{"target_id":"t"}]`,
		"missing target": `[{"screen_id":"s"}]`,
		"duplicate":      `[{"screen_id":"s","target_id":"a"},{"screen_id":"s","target_id":"b"}]`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "screens.json")
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCatalog(path); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	if _, err := LoadCatalog(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestEndpointSelection(t *testing.T) {
	target := casting.Target{
		ID:    "target",
		State: casting.TargetOnline,
		Endpoints: []casting.Endpoint{
			{
				ID:       "video",
				Protocol: "dlna",
				Capabilities: casting.Capabilities{
					Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, Streaming: true,
				},
			},
			{
				ID:       "audio",
				Protocol: "dlna",
				Capabilities: casting.Capabilities{
					Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Streaming: true,
				},
			},
		},
	}
	video := casting.StreamSource{Type: "stream", Format: "mpegts", VideoCodec: "h264"}
	endpoint, ok := SelectEndpoint(target, video)
	if !ok || endpoint.ID != "video" {
		t.Fatalf("video endpoint = %+v, %v", endpoint, ok)
	}
	withAudio := video
	withAudio.AudioCodec = "aac"
	endpoint, ok = SelectEndpoint(target, withAudio)
	if !ok || endpoint.ID != "audio" {
		t.Fatalf("audio endpoint = %+v, %v", endpoint, ok)
	}
	if _, ok := DiscoverableEndpoint(target); !ok {
		t.Fatal("target should be discoverable")
	}
	if _, ok := SelectEndpoint(target, casting.StreamSource{Format: "h264", VideoCodec: "h264"}); ok {
		t.Fatal("unknown format should not match")
	}
	if !Supports(target.Endpoints[1], withAudio) {
		t.Fatal("Supports should accept aac")
	}
}
