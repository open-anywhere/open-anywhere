package voiceentry

import "github.com/open-anywhere/open-anywhere/internal/voiceentry/audio"

// This file is the glue to ffmpeg, ffplay and libopus. It is excluded from
// coverage because exercising it needs those programs; tests inject fakes
// through the media type instead.

// codec encodes and decodes Opus. audio.Codec implements it in builds that
// include the opus tag.
type codec interface {
	Encode(pcm []byte) ([]byte, error)
	Decode(packet []byte) ([]byte, error)
}

// frameSource yields 60 ms PCM frames until it ends.
type frameSource interface {
	ReadFrame() ([]byte, error)
	Close() error
}

// pcmPlayer plays decoded downlink speech locally.
type pcmPlayer interface {
	Write(pcm []byte) error
}

// media creates the audio helpers. The defaults wrap ffmpeg, ffplay and libopus;
// tests replace them because those are not available everywhere.
type media struct {
	newCodec   func() (codec, error)
	fileSource func(path string) (frameSource, error)
	micSource  func(device string) (frameSource, error)
	newPlayer  func(rate int) (pcmPlayer, error)
}

func systemMedia() media {
	return media{
		newCodec: func() (codec, error) {
			c, err := audio.NewCodec()
			if err != nil {
				return nil, err
			}
			return c, nil
		},
		fileSource: func(path string) (frameSource, error) {
			src, err := audio.NewFileSource(path, 3)
			if err != nil {
				return nil, err
			}
			return src, nil
		},
		micSource: func(device string) (frameSource, error) {
			src, err := audio.NewMicSource(device)
			if err != nil {
				return nil, err
			}
			return src, nil
		},
		newPlayer: func(rate int) (pcmPlayer, error) {
			p, err := audio.NewPlayer(rate)
			if err != nil {
				return nil, err
			}
			return p, nil
		},
	}
}
