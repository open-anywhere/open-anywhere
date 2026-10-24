package tools

import (
	"strings"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

func TestSetVolumeValidatesAndPublishes(t *testing.T) {
	var got []voice.Event
	d := New(func(e voice.Event) { got = append(got, e) })
	if _, err := d.setVolume(map[string]any{"volume": 30.0}); err != nil || d.Volume() != 30 {
		t.Fatalf("err=%v volume=%d", err, d.Volume())
	}
	if len(got) != 1 || got[0].Type != "volume" || got[0].Text != "30" {
		t.Fatalf("events=%v", got)
	}
	for _, bad := range []any{101.0, -1.0, 2.5, "50", nil} {
		if _, err := d.setVolume(map[string]any{"volume": bad}); err == nil {
			t.Fatalf("volume %v accepted", bad)
		}
	}
	if d.Volume() != 30 {
		t.Fatalf("volume changed to %d", d.Volume())
	}
}

func TestDeviceToolsListAndGetVolume(t *testing.T) {
	d := New(func(voice.Event) {})
	tools := d.List()
	if len(tools) != 2 || tools[0].Name != "self.audio_speaker.set_volume" {
		t.Fatalf("tools=%+v", tools)
	}
	_, _ = d.setVolume(map[string]any{"volume": 40.0})
	if text, err := tools[1].Call(nil); err != nil || !strings.Contains(text, "40") {
		t.Fatalf("get_volume=%q err=%v", text, err)
	}
}
