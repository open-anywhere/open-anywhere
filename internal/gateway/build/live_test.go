package build

import (
	"context"
	"encoding/json"
	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	"os"
	"testing"
)

// TestLiveRegisteredArtifact is read-only and opt-in; it verifies persisted real build output.
func TestLiveRegisteredArtifact(t *testing.T) {
	path := os.Getenv("GATEWAY_LIVE_DB")
	if path == "" {
		t.Skip("set GATEWAY_LIVE_DB and GATEWAY_LIVE_APP to validate real registered output")
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.ListApps()
	if err != nil {
		t.Fatal(err)
	}
	id := os.Getenv("GATEWAY_LIVE_APP")
	for _, a := range rows {
		if a.ID != id {
			continue
		}
		if a.State != app.StateIdle {
			t.Fatalf("app state=%s", a.State)
		}
		result, err := artifact.Validate(a.Artifact.Path)
		if err != nil {
			t.Fatal(err)
		}
		if result.SHA256 != a.Artifact.SHA256 || result.Size != a.Artifact.Size {
			t.Fatal("artifact differs from persisted metadata")
		}
		doc, err := a.OpenAPI.Document()
		if err != nil || doc["components"] == nil {
			t.Fatal("missing persisted full OpenAPI", err)
		}
		b, _ := json.Marshal(doc)
		t.Logf("app=%s size=%d operations=%d spec_bytes=%d", id, result.Size, len(a.OpenAPI.Operations()), len(b))
		if err := db.Ping(context.Background()); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("app not found")
}
