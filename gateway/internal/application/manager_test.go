package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/open-anywhere/open-anywhere/gateway/internal/openapi"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

var ownerA = Owner{"device-a", "agent", "session"}
var ownerB = Owner{"device-b", "agent", "session"}

const spec = `{"openapi":"3.1.0","paths":{"/api/state":{"get":{"operationId":"read","responses":{"200":{"description":"ok"}}}}}}`

type fakeBuilder struct {
	calls atomic.Int32
	gate  chan struct{}
	fail  bool
}

func (b *fakeBuilder) Create(context.Context, BuildRequest) (string, error) {
	b.calls.Add(1)
	return "build-session", nil
}
func (b *fakeBuilder) Run(ctx context.Context, id string, r BuildRequest, submitted bool, cursor BuildCursor, save func(bool, *BuildEvent) error) (BuildProduct, error) {
	if err := save(true, &BuildEvent{ID: "e1", Type: "agent.message", Message: "building"}); err != nil {
		return BuildProduct{}, err
	}
	if b.gate != nil {
		select {
		case <-ctx.Done():
			return BuildProduct{}, ctx.Err()
		case <-b.gate:
		}
	}
	if b.fail {
		return BuildProduct{}, fmt.Errorf("failed")
	}
	return BuildProduct{Name: "demo", URL: "https://artifact.invalid/demo.tar.gz", Document: json.RawMessage(spec)}, nil
}

type fakeRuntime struct {
	starts   atomic.Int32
	stops    atomic.Int32
	state    string
	stopFail bool
}

func (r *fakeRuntime) PortFree(context.Context, int) (bool, error) { return true, nil }
func (r *fakeRuntime) Start(ctx context.Context, id, url string, p int) (Instance, error) {
	r.starts.Add(1)
	r.state = "ready"
	return Instance{AppID: id, ID: "instance-" + id, State: "ready", Port: p, BaseURL: "http://127.0.0.1:1234", Source: casting.StreamSource{Type: "stream", URL: "http://media/live.ts", Format: "mpegts", VideoCodec: "h264"}}, nil
}
func (r *fakeRuntime) Get(context.Context, string) (Instance, error) {
	return Instance{State: r.state}, nil
}
func (r *fakeRuntime) Stop(context.Context, string) (Instance, error) {
	r.stops.Add(1)
	if r.stopFail {
		return Instance{}, fmt.Errorf("stop failed")
	}
	return Instance{State: "stopped"}, nil
}

type fakeDisplay struct {
	starts atomic.Int32
	stops  atomic.Int32
	fail   bool
	state  casting.SessionState
}

func (d *fakeDisplay) Targets(context.Context) ([]casting.Target, error) {
	return []casting.Target{{ID: "tv", State: casting.TargetOnline, Endpoints: []casting.Endpoint{{ID: "dlna", Capabilities: casting.Capabilities{Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, Streaming: true}}}}}, nil
}
func (d *fakeDisplay) Start(ctx context.Context, r casting.CreateSessionRequest) (casting.Session, error) {
	d.starts.Add(1)
	if d.fail {
		return casting.Session{ID: "projection"}, fmt.Errorf("screen failed")
	}
	d.state = casting.SessionPlaying
	return casting.Session{ID: "projection", State: casting.SessionPlaying}, nil
}
func (d *fakeDisplay) Get(context.Context, string) (casting.Session, error) {
	return casting.Session{State: d.state}, nil
}
func (d *fakeDisplay) Stop(context.Context, string) (casting.Session, error) {
	d.stops.Add(1)
	return casting.Session{State: casting.SessionStopped}, nil
}
func manager(t *testing.T, b *fakeBuilder) (*Manager, *fakeRuntime, *fakeDisplay) {
	t.Helper()
	r := &fakeRuntime{}
	d := &fakeDisplay{}
	m, err := New(context.Background(), Options{StateFile: filepath.Join(t.TempDir(), "state.json"), Builder: b, Runtime: r, Display: d, Screens: []Screen{{ID: "screen", TargetID: "tv", EndpointID: "dlna"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, r, d
}
func idle(t *testing.T, m *Manager, owner Owner) string {
	t.Helper()
	out, err := m.Create(owner, "quiz", []string{" 韩鲁鹏 ", "张之阳"})
	if err != nil {
		t.Fatal(err)
	}
	id := out["app_id"].(string)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a, _ := m.find(owner, id)
		if a.State == "idle" {
			if a.Players[0] != "韩鲁鹏" {
				t.Fatal(a.Players)
			}
			return id
		}
		if a.State == "error" {
			t.Fatal(a.Error)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("build did not complete")
	return ""
}
func TestCreateOwnershipIdempotenceAndPersistence(t *testing.T) {
	b := &fakeBuilder{gate: make(chan struct{})}
	m, _, _ := manager(t, b)
	out, err := m.Create(ownerA, "quiz", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	id := out["app_id"].(string)
	same, err := m.Create(ownerA, "different", []string{"b"})
	if err != nil || same["app_id"] != id {
		t.Fatal(same, err)
	}
	if _, err = m.Status(context.Background(), ownerB, id); err == nil {
		t.Fatal("cross-owner access")
	}
	if _, err = m.Create(ownerB, "quiz", []string{"a", " a "}); err == nil {
		t.Fatal("duplicate names accepted")
	}
	if _, err = m.Create(Owner{}, "quiz", nil); err == nil {
		t.Fatal("missing owner accepted")
	}
	close(b.gate)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		a, _ := m.find(ownerA, id)
		if a.State == "idle" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	restored, err := New(context.Background(), m.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	a, err := restored.find(ownerA, id)
	if err != nil || a.State != "idle" || a.Prompt != "quiz" {
		t.Fatal(a, err)
	}
	if b.calls.Load() != 1 {
		t.Fatal("duplicate build")
	}
}
func TestConcurrentStartAndScreenExclusion(t *testing.T) {
	m, r, d := manager(t, &fakeBuilder{})
	id := idle(t, m, ownerA)
	other := idle(t, m, ownerB)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := m.Start(context.Background(), ownerA, id, "screen"); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if r.starts.Load() != 1 || d.starts.Load() != 1 {
		t.Fatal("duplicated resources")
	}
	if _, err := m.Start(context.Background(), ownerB, other, "screen"); err == nil {
		t.Fatal("screen double booking")
	}
	out, err := m.Stop(context.Background(), ownerA, id)
	if err != nil || out["state"] != "idle" {
		t.Fatal(out, err)
	}
	if _, err = m.Start(context.Background(), ownerB, other, "screen"); err != nil {
		t.Fatal(err)
	}
}
func TestFailureCleanupAndLostProjection(t *testing.T) {
	m, r, d := manager(t, &fakeBuilder{})
	id := idle(t, m, ownerA)
	d.fail = true
	if _, err := m.Start(context.Background(), ownerA, id, "screen"); err == nil {
		t.Fatal("start falsely succeeded")
	}
	if r.stops.Load() != 1 || d.stops.Load() != 1 {
		t.Fatal("cleanup missing")
	}
	a, _ := m.find(ownerA, id)
	if a.State != "error" || a.InstanceID != "" || a.ProjectionID != "" {
		t.Fatal(a)
	}
	id2 := idle(t, m, ownerA)
	d.fail = false
	if _, err := m.Start(context.Background(), ownerA, id2, "screen"); err != nil {
		t.Fatal(err)
	}
	d.state = casting.SessionFailed
	status, err := m.Status(context.Background(), ownerA, id2)
	if err != nil || status["state"] != "error" {
		t.Fatal(status, err)
	}
}
func TestFailedCleanupRetainsReservation(t *testing.T) {
	m, r, d := manager(t, &fakeBuilder{})
	id := idle(t, m, ownerA)
	d.fail = true
	r.stopFail = true
	_, _ = m.Start(context.Background(), ownerA, id, "screen")
	a, _ := m.find(ownerA, id)
	if a.InstanceID == "" {
		t.Fatal("lost occupied instance")
	}
	other := idle(t, m, ownerB)
	if _, err := m.Start(context.Background(), ownerB, other, "screen"); err == nil {
		t.Fatal("released occupied screen")
	}
}
func TestRecoverBuild(t *testing.T) {
	b := &fakeBuilder{gate: make(chan struct{})}
	m, _, _ := manager(t, b)
	out, err := m.Create(ownerA, "quiz", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := out["app_id"].(string)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		a, _ := m.find(ownerA, id)
		if a.Submitted {
			break
		}
		time.Sleep(time.Millisecond)
	}
	m.Close()
	b.gate = nil
	restored, err := New(context.Background(), m.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.Recover(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		a, _ := restored.find(ownerA, id)
		if a.State == "idle" {
			if b.calls.Load() != 1 {
				t.Fatal("recreated build session")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("recovery failed")
}

func TestSaveFailureDoesNotPublishApp(t *testing.T) {
	b := &fakeBuilder{}
	m, _, _ := manager(t, b)
	// A directory at the state path makes atomic file replacement fail.
	if err := os.Mkdir(m.opts.StateFile, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(ownerA, "quiz", nil); err == nil {
		t.Fatal("persistence failure accepted")
	}
	if len(m.apps) != 0 || b.calls.Load() != 0 {
		t.Fatal("uncommitted state published")
	}
}
func TestUnknownResultBlocksDuplicateResources(t *testing.T) {
	m, _, _ := manager(t, &fakeBuilder{})
	id := idle(t, m, ownerA)
	a, _ := m.find(ownerA, id)
	a.State = "error"
	a.ScreenID = "screen"
	a.UnknownRuntime = true
	if err := m.save(a); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Stop(context.Background(), ownerA, id); err == nil {
		t.Fatal("uncertain resource released")
	}
	other := idle(t, m, ownerB)
	if _, err := m.Start(context.Background(), ownerB, other, "screen"); err == nil {
		t.Fatal("uncertain screen reused")
	}
}

func TestSendRefreshesRuntimeBeforeBusinessRequest(t *testing.T) {
	m, _, d := manager(t, &fakeBuilder{})
	id := idle(t, m, ownerA)
	if _, err := m.Start(context.Background(), ownerA, id, "screen"); err != nil {
		t.Fatal(err)
	}
	d.state = casting.SessionFailed
	_, err := m.Send(context.Background(), ownerA, id, openapi.Arguments{OperationID: "read"})
	var f *Failure
	if !errors.As(err, &f) || f.Code != "APP_NOT_RUNNING" {
		t.Fatalf("want APP_NOT_RUNNING before HTTP dispatch, got %v", err)
	}
	a, _ := m.find(ownerA, id)
	if a.State != "error" {
		t.Fatal("stale running state", a.State)
	}
}
func TestRepeatedStartChecksProjection(t *testing.T) {
	m, _, d := manager(t, &fakeBuilder{})
	id := idle(t, m, ownerA)
	if _, err := m.Start(context.Background(), ownerA, id, "screen"); err != nil {
		t.Fatal(err)
	}
	d.state = casting.SessionFailed
	if out, err := m.Start(context.Background(), ownerA, id, "screen"); err == nil {
		t.Fatalf("falsely returned running: %v", out)
	}
}

type wrongIdentityRuntime struct{ *fakeRuntime }

func (r wrongIdentityRuntime) Start(context.Context, string, string, int) (Instance, error) {
	return Instance{AppID: "someone-else", ID: "foreign-instance", State: "ready", BaseURL: "http://example.test"}, nil
}
func TestForeignInstanceNeverCleanedUp(t *testing.T) {
	m, r, _ := manager(t, &fakeBuilder{})
	m.opts.Runtime = wrongIdentityRuntime{r}
	id := idle(t, m, ownerA)
	if _, err := m.Start(context.Background(), ownerA, id, "screen"); err == nil {
		t.Fatal("foreign instance accepted")
	}
	if r.stops.Load() != 0 {
		t.Fatal("deleted another application's instance")
	}
	a, _ := m.find(ownerA, id)
	if !a.UnknownRuntime {
		t.Fatal("unknown outcome not retained")
	}
}
