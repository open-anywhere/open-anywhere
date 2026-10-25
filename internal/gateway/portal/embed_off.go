//go:build !portalembed

package portal

import "io/fs"

// embeddedWebFS is nil in the default build: the frontend is served from
// portal.web_dir instead of from the binary.
func embeddedWebFS() fs.FS { return nil }
