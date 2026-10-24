package screen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

func TestNilCatalogLookups(t *testing.T) {
	var catalog *Catalog
	if _, ok := catalog.Get("tv"); ok {
		t.Fatal("nil catalog should not resolve screens")
	}
	if catalog.List() != nil || !catalog.Empty() {
		t.Fatalf("nil catalog = %+v %v", catalog.List(), catalog.Empty())
	}
}

func TestLoadCatalogTrimsWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screens.json")
	data := `[
		{"screen_id":"  tv  ","name":"\t客厅\n","target_id":"\r target-1 \n"},
		{"screen_id":"\tstudy\r","name":"   ","target_id":"target-2"}
	]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	tv, ok := catalog.Get("tv")
	if !ok {
		t.Fatalf("screens = %+v", catalog.List())
	}
	if tv.Name != "客厅" || tv.TargetID != "target-1" {
		t.Fatalf("tv = %+v", tv)
	}
	study, ok := catalog.Get("study")
	if !ok || study.Name != "study" || study.TargetID != "target-2" {
		t.Fatalf("study = %+v, %v", study, ok)
	}
	// List returns a copy that callers cannot use to mutate the catalog.
	list := catalog.List()
	list[0].Name = "mutated"
	if again, _ := catalog.Get("tv"); again.Name != "客厅" {
		t.Fatal("List exposed internal state")
	}
}

func TestLoadCatalogRejectsWhitespaceOnlyFields(t *testing.T) {
	for name, data := range map[string]string{
		"blank id":     `[{"screen_id":"   ","target_id":"t"}]`,
		"blank target": `[{"screen_id":"s","target_id":"\t\n"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "screens.json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCatalog(path); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSupportsRejectsIncompleteProfiles(t *testing.T) {
	source := casting.StreamSource{Format: "mpegts", VideoCodec: "h264", AudioCodec: "aac"}
	cases := map[string]casting.Capabilities{
		"not streaming":  {Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}},
		"format absent":  {Streaming: true, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}},
		"video absent":   {Streaming: true, Formats: []string{"mpegts"}, AudioCodecs: []string{"aac"}},
		"audio absent":   {Streaming: true, Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}},
		"audio mismatch": {Streaming: true, Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"opus"}},
	}
	for name, capabilities := range cases {
		t.Run(name, func(t *testing.T) {
			if Supports(casting.Endpoint{Capabilities: capabilities}, source) {
				t.Fatal("incomplete profile should not be supported")
			}
		})
	}
	if !Supports(casting.Endpoint{Capabilities: casting.Capabilities{
		Streaming: true, Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"},
	}}, source) {
		t.Fatal("complete profile should be supported")
	}
	// Video-only streams do not require an audio codec.
	if !Supports(casting.Endpoint{Capabilities: casting.Capabilities{
		Streaming: true, Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"},
	}}, casting.StreamSource{Format: "mpegts", VideoCodec: "h264"}) {
		t.Fatal("video-only profile should be supported")
	}

	offline := casting.Target{ID: "t", Endpoints: []casting.Endpoint{{Capabilities: casting.Capabilities{Streaming: false, Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}}}}}
	if _, ok := SelectEndpoint(offline, source); ok {
		t.Fatal("offline endpoint should not be selected")
	}
	if _, ok := DiscoverableEndpoint(casting.Target{ID: "empty"}); ok {
		t.Fatal("target without endpoints should not be discoverable")
	}
}
