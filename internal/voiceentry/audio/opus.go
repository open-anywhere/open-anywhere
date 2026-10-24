// Package audio holds the audio constants and codec and subprocess helpers
// shared by the client: 16 kHz mono Opus, ffmpeg capture and ffplay output.
package audio

import "errors"

const (
	SampleRate      = 16000
	Channels        = 1
	FrameDurationMS = 60
	FrameSamples    = SampleRate * FrameDurationMS / 1000
	PCMFrameBytes   = FrameSamples * 2
)

var ErrFrameSize = errors.New("PCM must contain exactly one 60ms frame")

// Encoder and Decoder isolate the Opus implementation from the session state machine.
// The default build keeps codec selection injectable. The working local implementation
// is available with -tags opus nolibopusfile and system libopus.
type Encoder interface {
	Encode(pcm []byte) ([]byte, error)
}
type Decoder interface {
	Decode(packet []byte) ([]byte, error)
}
