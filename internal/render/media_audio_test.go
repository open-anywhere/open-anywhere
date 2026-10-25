package render

import "testing"

func hasArgs(args []string, want ...string) bool {
	if len(want) == 0 || len(want) > len(args) {
		return false
	}
	for i := 0; i <= len(args)-len(want); i++ {
		match := true
		for j := range want {
			if args[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestEncoderArgsVideoOnly(t *testing.T) {
	args := encoderArgs(Config{FPS: 10}, "10")
	if !hasArgs(args, "-an") || hasArgs(args, "-c:a") {
		t.Fatalf("unexpected video-only args: %v", args)
	}
	if !hasArgs(args, "-f", "image2pipe", "-vcodec", "mjpeg", "-framerate", "10", "-i", "pipe:0") {
		t.Fatalf("unexpected video input: %v", args)
	}
}

func TestEncoderArgsWithAudio(t *testing.T) {
	args := encoderArgs(Config{FPS: 10, AudioFormat: "pulse", AudioInput: "render.monitor"}, "10")
	for _, want := range [][]string{
		{"-thread_queue_size", "512", "-f", "pulse"},
		{"-f", "pulse", "-i", "render.monitor"},
		{"-map", "0:v:0", "-map", "1:a:0"},
		{"-c:a", "aac", "-profile:a", "aac_low", "-b:a", "128k"},
		{"-ar", "48000", "-ac", "2"},
		{"-f", "mpegts", "pipe:1"},
	} {
		if !hasArgs(args, want...) {
			t.Fatalf("args miss %v: %v", want, args)
		}
	}
	if hasArgs(args, "-an") {
		t.Fatalf("audio args disable audio: %v", args)
	}
}

func TestAudioConfig(t *testing.T) {
	if (Config{}).audioEnabled() || (Config{}).audioCodec() != "" {
		t.Fatal("empty audio configuration must stay video-only")
	}
	cfg := Config{AudioFormat: "pulse", AudioInput: "render.monitor"}
	if !cfg.audioEnabled() || cfg.audioCodec() != "aac" {
		t.Fatalf("audio configuration was not enabled: %+v", cfg)
	}
}
