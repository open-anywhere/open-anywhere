//go:build !opus

package audio

import (
	"errors"
	"testing"
)

func TestCodecIsUnavailableWithoutTheOpusTag(t *testing.T) {
	if _, err := NewCodec(); !errors.Is(err, ErrCodecUnavailable) {
		t.Fatalf("NewCodec err=%v", err)
	}
	var c *Codec
	if _, err := c.Encode(nil); !errors.Is(err, ErrCodecUnavailable) {
		t.Fatalf("Encode err=%v", err)
	}
	if _, err := c.Decode(nil); !errors.Is(err, ErrCodecUnavailable) {
		t.Fatalf("Decode err=%v", err)
	}
}
