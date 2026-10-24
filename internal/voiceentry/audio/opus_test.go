package audio

import "testing"

func TestFrameConstants(t *testing.T) {
	if FrameSamples != 960 || PCMFrameBytes != 1920 {
		t.Fatalf("unexpected 60ms frame: samples=%d bytes=%d", FrameSamples, PCMFrameBytes)
	}
}

func TestCodecRoundTrip(t *testing.T) {
	c, err := NewCodec()
	if err != nil {
		t.Skipf("codec unavailable: %v", err)
	}
	packet, err := c.Encode(make([]byte, PCMFrameBytes))
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := c.Decode(packet)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) == 0 || len(pcm) == 0 {
		t.Fatalf("empty codec result packet=%d pcm=%d", len(packet), len(pcm))
	}
}
