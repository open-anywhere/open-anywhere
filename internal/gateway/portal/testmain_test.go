package portal

import (
	"os"
	"testing"
)

// TestMain lowers the PBKDF2 work factor for the test process. Hashing still
// goes through the production code path and each hash records its own
// iteration count, so password verification behaves exactly as in production;
// only the deliberately expensive key derivation is shortened.
func TestMain(m *testing.M) {
	passwordIterations = 1_000
	os.Exit(m.Run())
}
