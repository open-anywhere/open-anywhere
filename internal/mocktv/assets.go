package mocktv

import _ "embed"

//go:embed assets/index.html
var indexHTML []byte

//go:embed assets/mpegts.min.js
var playerJS []byte
