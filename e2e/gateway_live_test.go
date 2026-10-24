//go:build e2e && (darwin || linux)

package e2e

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	core "github.com/open-anywhere/open-anywhere/internal/render"
	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

// TestLiveBuiltCounter runs the counter created by the real AgentAPI smoke
// build. It reads the registry without changing its lifecycle or calling AI.
// The counter-specific operations are intentionally not a generic acceptance
// test for arbitrary generated applications.
func TestLiveBuiltCounter(t *testing.T) {
	dbPath, appID := os.Getenv("GATEWAY_LIVE_DB"), os.Getenv("GATEWAY_LIVE_APP")
	if dbPath == "" || appID == "" {
		t.Skip("set GATEWAY_LIVE_DB and GATEWAY_LIVE_APP for the real counter artifact")
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.ListApps()
	if err != nil {
		t.Fatal(err)
	}
	var built app.App
	for _, row := range rows {
		if row.ID == appID {
			built = row
		}
	}
	if built.State != app.StateIdle || built.OpenAPI == nil {
		t.Fatal("expected registered idle application with persisted OpenAPI")
	}
	checked, err := artifact.Validate(built.Artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if checked.SHA256 != built.Artifact.SHA256 || checked.Size != built.Artifact.Size {
		t.Fatal("artifact differs from build registry")
	}
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, built.Artifact.Path)
	}))
	defer files.Close()
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	runtime, err := core.New(core.Config{WorkDir: t.TempDir(), PublicURL: base, Chrome: os.Getenv("RENDER_CHROME"), ServicePorts: []int{server.Listener.Addr().(*net.TCPAddr).Port}})
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = core.NewHandler(runtime)
	server.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Error(err)
		}
		server.Close()
	})
	port := freePort(t)
	instance := decode[api.Instance](t, request(t, "POST", base+"/v1/apps", api.CreateRequest{AppID: appID, ArtifactURL: files.URL + "/artifact.tar.gz", Port: port}, 201))
	if instance.State != "ready" || instance.Source == nil {
		t.Fatalf("runtime not ready: %+v", instance)
	}
	request(t, "GET", instance.AppBaseURL+"/health", nil, 200)
	request(t, "GET", instance.AppBaseURL+"/", nil, 200)
	call := func(operation string, body any, wantStatus int) map[string]any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := built.OpenAPI.Execute(ctx, instance.AppBaseURL, operation, nil, body, &http.Client{Timeout: 5 * time.Second})
		if err != nil || result.Status != wantStatus {
			t.Fatalf("%s: result=%+v err=%v", operation, result, err)
		}
		data, ok := result.Data.(map[string]any)
		if !ok {
			t.Fatalf("%s: expected JSON object", operation)
		}
		return data
	}
	if call("getCounterState", nil, 200)["total"] != float64(0) {
		t.Fatal("counter did not start at zero")
	}
	player := map[string]any{"user_name": "整合验收玩家"}
	call("joinPlayer", player, 201)
	call("incrementCounter", player, 200)
	if call("getCounterState", nil, 200)["total"] != float64(1) {
		t.Fatal("OpenAPI mutation did not change state")
	}
	// Invalid schema input must be rejected before sending an HTTP request.
	if _, err := built.OpenAPI.Execute(context.Background(), instance.AppBaseURL, "incrementCounter", nil, map[string]any{"user_name": 123}, http.DefaultClient); err == nil {
		t.Fatal("invalid player name passed schema validation")
	}
	stream := decodeStream(t, instance.Source.URL)
	select {
	case frame := <-stream.output:
		if len(frame) != 1280*720*3 {
			t.Fatal("no decoded video frame")
		}
		saveFrame(t, frame, "live-counter")
	case <-time.After(20 * time.Second):
		t.Fatal("real generated application did not produce video")
	}
	stream.cancel()
	stopped := decode[api.Instance](t, request(t, "DELETE", base+"/v1/apps/"+instance.InstanceID, nil, 200))
	if stopped.State != "stopped" {
		t.Fatalf("stop: %+v", stopped)
	}
	listener, err := net.Listen("tcp4", instance.AppBaseURL[len("http://"):])
	if err != nil {
		t.Fatalf("application port not released: %v", err)
	}
	listener.Close()
	t.Logf("real build %s: schema HTTP mutation, video decode and stop passed", appID)
}
