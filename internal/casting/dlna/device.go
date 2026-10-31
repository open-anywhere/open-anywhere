package dlna

import (
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// Device is one discovered renderer: the target it presents to callers and the
// AVTransport control URL used to drive it.
type Device struct {
	Target     api.Target
	ControlURL string
	ExpiresAt  time.Time
}
