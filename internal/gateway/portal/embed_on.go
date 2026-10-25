//go:build portalembed

package portal

import (
	"embed"
	"io/fs"
)

// webdist is written by `pnpm build` (portal/scripts/sync-embed.mjs), so a
// `-tags portalembed` build carries the frontend inside the gateway binary.
//
//go:embed all:webdist
var embeddedWeb embed.FS

func embeddedWebFS() fs.FS {
	sub, err := fs.Sub(embeddedWeb, "webdist")
	if err != nil {
		return nil
	}
	return sub
}
