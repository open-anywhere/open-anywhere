// Package canvas defines the fixed render canvas shared by the build contract
// and the Render service. Apps are built against this canvas and Render
// captures the page at exactly this size.
package canvas

import "fmt"

const (
	// Width and Height are the logical canvas size captured by Render.
	Width  = 1280
	Height = 720
	// Aspect is the human-readable aspect ratio of Width×Height.
	Aspect = "16:9"
)

// Description is injected into build prompts and shown in documentation.
func Description() string {
	return fmt.Sprintf("%d×%d（%s）", Width, Height, Aspect)
}

// Is16x9 reports whether w×h has the same aspect ratio as the canvas.
func Is16x9(w, h int) bool {
	return w > 0 && h > 0 && w*9 == h*16
}
