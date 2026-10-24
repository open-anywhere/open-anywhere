package audio

import "testing"

func TestMicFFmpegArgs(t *testing.T) {
	tests := []struct{ goos, want string }{{"windows", "dshow"}, {"darwin", "avfoundation"}, {"linux", "alsa"}}
	for _, tt := range tests {
		args, err := MicFFmpegArgs("Mic One", tt.goos)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, a := range args {
			if a == tt.want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s args=%v", tt.goos, args)
		}
	}
	if _, err := MicFFmpegArgs("mic", "plan9"); err == nil {
		t.Fatal("expected unsupported platform error")
	}
}
