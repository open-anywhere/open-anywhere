//go:build !opus

package audio

import "errors"

var ErrCodecUnavailable = errors.New("Opus codec is disabled; rebuild with -tags opus and libopus")

type Codec struct{}

func NewCodec() (*Codec, error)                { return nil, ErrCodecUnavailable }
func (c *Codec) Encode([]byte) ([]byte, error) { return nil, ErrCodecUnavailable }
func (c *Codec) Decode([]byte) ([]byte, error) { return nil, ErrCodecUnavailable }
