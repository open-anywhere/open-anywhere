// Package tools defines the device-side MCP tools reported to Linx.
package tools

import (
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

// Set holds the tools reported to Linx. The volume tools drive the web
// player's gain, so they only change what the console plays.
type Set struct {
	volume  atomic.Int32
	publish func(voice.Event)
}

// New returns a Set at full volume; publish is called when Linx changes the volume.
func New(publish func(voice.Event)) *Set {
	d := &Set{publish: publish}
	d.volume.Store(100)
	return d
}

// Volume returns the current volume, 0-100.
func (d *Set) Volume() int { return int(d.volume.Load()) }

// List returns the tools to register on a Linx session.
func (d *Set) List() []linx.Tool {
	return []linx.Tool{
		{
			Name:        "self.audio_speaker.set_volume",
			Description: "设置扬声器（网页播放器）音量，范围 0-100",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"volume": map[string]any{"type": "integer", "minimum": 0, "maximum": 100, "description": "音量大小，范围0-100"},
				},
				"required": []any{"volume"},
			},
			Call: d.setVolume,
		},
		{
			Name:        "self.audio_speaker.get_volume",
			Description: "获取扬声器（网页播放器）当前音量",
			Call:        func(map[string]any) (string, error) { return fmt.Sprintf("当前音量为%d%%", d.Volume()), nil },
		},
	}
}

func (d *Set) setVolume(args map[string]any) (string, error) {
	v, ok := args["volume"].(float64)
	if !ok || v < 0 || v > 100 || v != float64(int(v)) {
		return "", fmt.Errorf("volume 必须是 0-100 的整数")
	}
	d.volume.Store(int32(v))
	d.publish(voice.Event{Type: "volume", Text: strconv.Itoa(int(v))})
	return fmt.Sprintf("音量已设置为%d%%", int(v)), nil
}
