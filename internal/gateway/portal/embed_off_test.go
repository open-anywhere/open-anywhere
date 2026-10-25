//go:build !portalembed

package portal

import "testing"

func TestEmbeddedWebAbsentWithoutTag(t *testing.T) {
	if EmbeddedWebAvailable() {
		t.Fatal("frontend unexpectedly embedded without -tags portalembed")
	}
}
