package casting

import (
	"context"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// Adapter controls playback; it neither generates nor proxies media.
type Adapter interface {
	Start(context.Context, Device, api.StreamSource) error
	Status(context.Context, Device) (api.SessionState, error)
	Stop(context.Context, Device) error
}
