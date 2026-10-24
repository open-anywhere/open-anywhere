package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

func TestGetAppOperations(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env := newTestEnv(t, nil)
	ctx := context.Background()
	env.registry.Add(app.App{ID: "idle", Owner: owner, State: app.StateIdle, OpenAPI: mustSpec(t)})
	env.registry.Add(app.App{ID: "empty", Owner: owner, State: app.StateIdle})
	env.registry.Add(app.App{ID: "building", Owner: owner, State: app.StateBuilding})
	env.registry.Add(app.App{ID: "error", Owner: owner, State: app.StateError})

	view, err := env.svc.GetAppOperations(ctx, owner, "idle")
	if err != nil {
		t.Fatalf("GetAppOperations: %v", err)
	}
	if view.AppID != "idle" || view.State != app.StateIdle || len(view.Operations) != 1 {
		t.Fatalf("view = %+v", view)
	}
	if operation := view.Operations[0]; operation.OperationID != "updateItem" || operation.Method != http.MethodPost || operation.Path != "/api/items/{id}" {
		t.Fatalf("operation = %+v", operation)
	}
	empty, err := env.svc.GetAppOperations(ctx, owner, "empty")
	if err != nil || empty.Operations == nil || len(empty.Operations) != 0 {
		t.Fatalf("empty = %+v, %v", empty, err)
	}
	if _, err := env.svc.GetAppOperations(ctx, owner, "building"); toolCode(t, err) != "APP_BUILDING" {
		t.Fatalf("building error = %v", err)
	}
	if _, err := env.svc.GetAppOperations(ctx, owner, "error"); toolCode(t, err) != "APP_IN_ERROR" {
		t.Fatalf("error error = %v", err)
	}
	if _, err := env.svc.GetAppOperations(ctx, owner, "missing"); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatalf("missing error = %v", err)
	}
}

func TestRunMonitorUntilCanceled(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env := newTestEnv(t, nil)
	env.svc.cfg.MonitorInterval = time.Millisecond
	env.registry.Add(runningApp(owner))
	env.render.getInstance = render.App{InstanceID: "inst_1", State: render.StateError}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		env.svc.RunMonitor(ctx)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(env.notifier.notifications()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunMonitor did not stop")
	}
	if len(env.notifier.notifications()) == 0 {
		t.Fatal("monitor did not detect the failure")
	}
}

func TestNewDefaults(t *testing.T) {
	svc := New(app.NewRegistry(), &fakeBuilder{}, &fakeRender{}, &fakeCasting{}, nil, &fakeNotifier{}, Config{})
	cfg := svc.cfg
	if cfg.StartTimeout != 60*time.Second || cfg.ProjectionTimeout != 30*time.Second || cfg.OperationsTimeout != 15*time.Second || cfg.MonitorInterval != 5*time.Second {
		t.Fatalf("timeouts = %+v", cfg)
	}
	if cfg.PortMin != 39000 || cfg.PortMax != 39999 {
		t.Fatalf("ports = %+v", cfg)
	}
	if svc.http.Timeout != cfg.OperationsTimeout {
		t.Fatalf("http timeout = %v", svc.http.Timeout)
	}
}

func TestScalarParameter(t *testing.T) {
	tests := []struct {
		value any
		want  string
		ok    bool
	}{
		{value: "text", want: "text", ok: true},
		{value: true, want: "true", ok: true},
		{value: float64(1.5), want: "1.5", ok: true},
		{value: float32(2.5), want: "2.5", ok: true},
		{value: 3, want: "3", ok: true},
		{value: int64(4), want: "4", ok: true},
		{value: int32(5), want: "5", ok: true},
		{value: json.Number("6"), want: "6", ok: true},
		{value: []any{"x"}, ok: false},
		{value: map[string]any{"x": 1}, ok: false},
		{value: nil, ok: false},
	}
	for _, test := range tests {
		got, ok := scalarParameter(test.value)
		if got != test.want || ok != test.ok {
			t.Fatalf("scalarParameter(%v) = %q, %v", test.value, got, ok)
		}
	}
}

func TestWaitPlaying(t *testing.T) {
	ctx := context.Background()
	tests := map[string]struct {
		setup func(*fakeCasting, *Service)
		ctx   context.Context
		ok    bool
	}{
		"playing": {setup: func(f *fakeCasting, _ *Service) { f.states = []casting.SessionState{casting.SessionPlaying} }, ctx: ctx, ok: true},
		"stopped": {setup: func(f *fakeCasting, _ *Service) { f.states = []casting.SessionState{casting.SessionStopped} }, ctx: ctx},
		"failed":  {setup: func(f *fakeCasting, _ *Service) { f.states = []casting.SessionState{casting.SessionFailed} }, ctx: ctx},
		"error":   {setup: func(f *fakeCasting, _ *Service) { f.getErr = errors.New("down") }, ctx: ctx},
		"timeout": {
			setup: func(f *fakeCasting, s *Service) {
				f.states = []casting.SessionState{casting.SessionStarting}
				s.cfg.ProjectionTimeout = time.Nanosecond
			},
			ctx: ctx,
		},
		"canceled": {
			setup: func(f *fakeCasting, s *Service) {
				f.states = []casting.SessionState{casting.SessionStarting}
				s.cfg.ProjectionTimeout = time.Hour
			},
			ctx: canceledContext(),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			test.setup(env.casting, env.svc)
			err := env.svc.waitPlaying(test.ctx, "ps_1")
			if (err == nil) != test.ok {
				t.Fatalf("waitPlaying error = %v", err)
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestStartRuntimeFailures(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	tests := map[string]struct {
		setup func(*testEnv)
		code  string
	}{
		"missing artifact": {
			setup: func(env *testEnv) {
				entry, _ := env.registry.Get("app_1")
				entry.Update(func(value *app.App) { value.Artifact.DownloadURL = "" })
			},
			code: "START_FAILED",
		},
		"render not ready": {
			setup: func(env *testEnv) {
				env.render.instance = render.App{InstanceID: "inst_1", State: render.StateStarting}
			},
			code: "START_FAILED",
		},
		"stream missing": {
			setup: func(env *testEnv) {
				env.render.instance = readyApp()
				env.render.instance.Source = casting.StreamSource{}
			},
			code: "STREAM_START_FAILED",
		},
		"incompatible screen": {
			setup: func(env *testEnv) {
				env.casting.target.Endpoints[0].Capabilities.Formats = []string{"mp4"}
			},
			code: "TARGET_UNREACHABLE",
		},
		"projection create failure": {
			setup: func(env *testEnv) {
				env.casting.createErr = errors.New("casting down")
			},
			code: "PROJECTION_TIMEOUT",
		},
		"projection stopped": {
			setup: func(env *testEnv) {
				env.casting.session = casting.Session{ID: "ps_1", State: casting.SessionStopped}
			},
			code: "PROJECTION_TIMEOUT",
		},
		"port listing failure": {
			setup: func(env *testEnv) {
				env.render.listErr = errors.New("render down")
			},
			code: "START_FAILED",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			entry := env.registry.Add(idleApp(t, owner))
			test.setup(env)
			if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != test.code {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
			snapshot := entry.Snapshot()
			if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Stage != app.StageStarting {
				t.Fatalf("snapshot = %+v", snapshot)
			}
			if len(env.notifier.notifications()) != 1 {
				t.Fatalf("notifications = %+v", env.notifier.notifications())
			}
		})
	}
}
