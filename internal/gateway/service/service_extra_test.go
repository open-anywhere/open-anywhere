package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
)

func TestGetAppOperations(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env := newTestEnv(t)
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
	env := newTestEnv(t)
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

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
