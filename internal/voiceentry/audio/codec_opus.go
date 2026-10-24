//go:build opus

package audio

import (
	"encoding/binary"

	"github.com/hraban/opus"
)

type Codec struct {
	encoder *opus.Encoder
	decoder *opus.Decoder
}

func NewCodec() (*Codec, error) {
	enc, err := opus.NewEncoder(SampleRate, Channels, opus.AppVoIP)
	if err != nil {
		return nil, err
	}
	dec, err := opus.NewDecoder(SampleRate, Channels)
	if err != nil {
		return nil, err
	}
	return &Codec{encoder: enc, decoder: dec}, nil
}
func (c *Codec) Encode(pcm []byte) ([]byte, error) {
	if len(pcm) != PCMFrameBytes {
		return nil, ErrFrameSize
	}
	samples := make([]int16, FrameSamples)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	out := make([]byte, 4000)
	n, err := c.encoder.Encode(samples, out)
	return out[:n], err
}
func (c *Codec) Decode(packet []byte) ([]byte, error) {
	samples := make([]int16, FrameSamples)
	n, err := c.decoder.Decode(packet, samples)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(samples[i]))
	}
	return out, nil
}
