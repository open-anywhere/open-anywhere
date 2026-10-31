package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/build"
	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
)

// startOnlyBuilder implements BuildStarter but not BuildContinuer.
type startOnlyBuilder struct{}

func (startOnlyBuilder) Start(context.Context, app.App) error { return nil }

func TestSetHTTPClientKeepsNilAndReplacesClient(t *testing.T) {
	env := newTestEnv(t)
	original := env.svc.http
	env.svc.SetHTTPClient(nil)
	if env.svc.http != original {
		t.Fatal("SetHTTPClient(nil) replaced the client")
	}
	custom := &http.Client{Timeout: time.Millisecond}
	env.svc.SetHTTPClient(custom)
	if env.svc.http != custom {
		t.Fatal("SetHTTPClient did not install the client")
	}
}

func TestChoosePortSkipsExcludedPorts(t *testing.T) {
	env := newTestEnv(t)
	port, err := env.svc.choosePort(context.Background(), nil)
	if err != nil || port != 39000 {
		t.Fatalf("choosePort = %d, %v", port, err)
	}
	port, err = env.svc.choosePort(context.Background(), map[int]bool{39000: true})
	if err != nil || port != 39001 {
		t.Fatalf("choosePort with exclusion = %d, %v", port, err)
	}
}

func TestCreateAppReportsUnconfiguredPlatform(t *testing.T) {
	env := newTestEnv(t)
	env.builder.setError(app.ErrPlatformNotConfigured)
	_, err := env.svc.CreateApp(context.Background(), ownerFresh, "做一个小游戏", nil)
	if toolCode(t, err) != "PLATFORM_NOT_CONFIGURED" {
		t.Fatalf("err = %v", err)
	}
	if values := env.registry.List(); len(values) != 0 {
		t.Fatalf("registry = %+v", values)
	}
}

func TestUpdateAppStateAndBuilderFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("unsupported state", func(t *testing.T) {
		env := newTestEnv(t)
		env.registry.Add(app.App{ID: "app_1", Owner: ownerFresh, State: app.State("paused"), SessionID: "ses_1"})
		_, err := env.svc.UpdateApp(ctx, ownerFresh, "app_1", "改一下")
		var tool *ToolError
		if !errors.As(err, &tool) || tool.Code != "APP_IN_ERROR" || tool.Message != "应用当前状态无法修订" {
			t.Fatalf("err = %#v", err)
		}
	})
	t.Run("builder cannot continue", func(t *testing.T) {
		env := newTestEnv(t)
		env.svc.builder = startOnlyBuilder{}
		entry := env.registry.Add(app.App{ID: "app_1", Owner: ownerFresh, State: app.StateIdle, SessionID: "ses_1", Prompt: "旧需求"})
		_, err := env.svc.UpdateApp(ctx, ownerFresh, "app_1", "改一下")
		if toolCode(t, err) != "UPDATE_START_FAILED" {
			t.Fatalf("err = %v", err)
		}
		if snapshot := entry.Snapshot(); snapshot.State != app.StateIdle || snapshot.Prompt != "旧需求" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	})
	t.Run("session unavailable", func(t *testing.T) {
		env := newTestEnv(t)
		env.builder.setContinueError(build.ErrSessionUnavailable)
		entry := env.registry.Add(app.App{ID: "app_1", Owner: ownerFresh, State: app.StateIdle, SessionID: "ses_1", Prompt: "旧需求"})
		_, err := env.svc.UpdateApp(ctx, ownerFresh, "app_1", "改一下")
		if toolCode(t, err) != "SESSION_UNAVAILABLE" {
			t.Fatalf("err = %v", err)
		}
		if snapshot := entry.Snapshot(); snapshot.State != app.StateIdle || snapshot.Prompt != "旧需求" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	})
	t.Run("other continue failure", func(t *testing.T) {
		env := newTestEnv(t)
		env.builder.setContinueError(errors.New("continue broke"))
		entry := env.registry.Add(app.App{ID: "app_1", Owner: ownerFresh, State: app.StateIdle, SessionID: "ses_1", Prompt: "旧需求"})
		_, err := env.svc.UpdateApp(ctx, ownerFresh, "app_1", "改一下")
		var tool *ToolError
		if !errors.As(err, &tool) || tool.Code != "UPDATE_START_FAILED" || tool.Message != "无法启动修订" {
			t.Fatalf("err = %#v", err)
		}
		if snapshot := entry.Snapshot(); snapshot.State != app.StateIdle || snapshot.Prompt != "旧需求" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	})
}

func TestRuntimeOperationsSurfaceBindingErrors(t *testing.T) {
	ctx := context.Background()
	env := newTenantEnv(t)
	owner := app.Identity{DeviceID: "device", AgentID: "agent", SessionID: "session"}
	value := runningApp(owner)
	env.registry.Add(value)
	env.bindings.errs[ownerKey(owner)] = errors.New("binding down")

	if _, err := env.svc.StopApp(ctx, owner, "app_1"); err == nil || !strings.Contains(err.Error(), "binding down") {
		t.Fatalf("StopApp err = %v", err)
	}
	if _, err := env.svc.SendRequest(ctx, owner, "app_1", "updateItem", nil, nil); err == nil || !strings.Contains(err.Error(), "binding down") {
		t.Fatalf("SendRequest err = %v", err)
	}
	if err := env.svc.verifyRunning(ctx, value); err == nil || !strings.Contains(err.Error(), "binding down") {
		t.Fatalf("verifyRunning err = %v", err)
	}
}

func TestCheckRunningSkipsAppsWithoutBindings(t *testing.T) {
	env := newTenantEnv(t)
	owner := app.Identity{DeviceID: "device", AgentID: "agent", SessionID: "session"}
	pending := app.App{ID: "pending", Owner: owner, State: app.StateError, PendingRuntime: true, InstanceID: "inst_1"}
	running := runningApp(owner)
	running.ID = "running"
	env.registry.Add(pending)
	env.registry.Add(running)
	env.bindings.errs[ownerKey(owner)] = errors.New("binding down")

	env.svc.checkRunning(context.Background())

	if got := mustGetTenant(t, env, "pending"); got.State != app.StateError || !got.PendingRuntime {
		t.Fatalf("pending = %+v", got)
	}
	if got := mustGetTenant(t, env, "running"); got.State != app.StateRunning {
		t.Fatalf("running = %+v", got)
	}
}

func mustGetTenant(t *testing.T, env *tenantEnv, id string) app.App {
	t.Helper()
	entry, ok := env.registry.Get(id)
	if !ok {
		t.Fatalf("missing app %s", id)
	}
	return entry.Snapshot()
}

func TestSendRequestMappingOfExecutorErrors(t *testing.T) {
	ctx := context.Background()
	owner := app.Identity{DeviceID: "device", AgentID: "agent", SessionID: "session"}
	baseURL := "http://127.0.0.1:1"

	newRunningEnv := func(t *testing.T, appBaseURL string, spec *openapi.Spec) *testEnv {
		t.Helper()
		env := newTestEnv(t)
		env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateRunning, AppBaseURL: appBaseURL, OpenAPI: spec})
		return env
	}

	t.Run("timeout", func(t *testing.T) {
		slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(20 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer slow.Close()
		env := newRunningEnv(t, slow.URL, mustSpec(t))
		env.svc.SetHTTPClient(&http.Client{Timeout: time.Millisecond})
		_, err := env.svc.SendRequest(ctx, owner, "app_1", "updateItem", map[string]any{"id": "a"}, nil)
		var tool *ToolError
		if !errors.As(err, &tool) || tool.Code != "TARGET_UNREACHABLE" || tool.Message != "应用请求失败" {
			t.Fatalf("err = %#v", err)
		}
	})

	t.Run("redirect", func(t *testing.T) {
		redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}))
		defer redirect.Close()
		env := newRunningEnv(t, redirect.URL, mustSpec(t))
		_, err := env.svc.SendRequest(ctx, owner, "app_1", "updateItem", map[string]any{"id": "a"}, nil)
		var tool *ToolError
		if !errors.As(err, &tool) || tool.Code != "INVALID_TARGET_URL" || tool.Message != "应用请求失败" {
			t.Fatalf("err = %#v", err)
		}
	})

	t.Run("unclassified api error", func(t *testing.T) {
		env := newRunningEnv(t, baseURL, mustSpec(t))
		_, err := env.svc.SendRequest(ctx, owner, "app_1", "updateItem", map[string]any{"bogus": "x"}, nil)
		var tool *ToolError
		if !errors.As(err, &tool) || tool.Code != "INVALID_ARGUMENT" || tool.Message != "应用请求参数无效" {
			t.Fatalf("err = %#v", err)
		}
	})

	t.Run("unclassified execution error", func(t *testing.T) {
		var spec openapi.Spec
		if err := json.Unmarshal([]byte(`{"operations":[{"operationId":"updateItem","method":"POST","path":"/api/items/{id}"}]}`), &spec); err != nil {
			t.Fatal(err)
		}
		env := newRunningEnv(t, baseURL, &spec)
		_, err := env.svc.SendRequest(ctx, owner, "app_1", "updateItem", nil, nil)
		var tool *ToolError
		if !errors.As(err, &tool) || tool.Code != "TARGET_UNREACHABLE" || tool.Message != "应用请求失败" {
			t.Fatalf("err = %#v", err)
		}
	})
}

func TestReleaseFailedStartRequiresBindings(t *testing.T) {
	env := newTenantEnv(t)
	owner := app.Identity{DeviceID: "device", AgentID: "agent"}
	entry := env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateError})
	if err := env.svc.releaseFailedStart(context.Background(), entry); toolCode(t, err) != "SERVICE_NOT_CONFIGURED" {
		t.Fatalf("err = %v", err)
	}
}

func TestListAllAppsIncludesOwnersNewestFirst(t *testing.T) {
	env := newTestEnv(t)
	if got := env.svc.ListAllApps(context.Background()); got == nil || len(got) != 0 {
		t.Fatalf("empty list = %+v", got)
	}
	older := builtApp(t, "app_old", 0, app.StateIdle)
	newer := builtApp(t, "app_new", 10, app.StateIdle)
	env.registry.Add(older)
	env.registry.Add(newer)
	out := env.svc.ListAllApps(context.Background())
	if len(out) != 2 || out[0].AppID != "app_new" || out[1].AppID != "app_old" {
		t.Fatalf("out = %+v", out)
	}
	for _, item := range out {
		if !item.Owner.SameOwner(ownerOld) {
			t.Fatalf("owner = %+v", item.Owner)
		}
	}
}

func TestDeleteAppReportsArtifactRemovalFailure(t *testing.T) {
	env := newTestEnv(t)
	env.svc.SetArtifactStore(failingArtifactStore{err: errors.New("artifact store down")})
	env.registry.Add(builtApp(t, "app_1", 0, app.StateIdle))
	if err := env.svc.DeleteApp(context.Background(), ownerFresh, "app_1"); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	if _, ok := env.registry.Get("app_1"); ok {
		t.Fatal("application was not removed")
	}
}

func TestDeleteAppOwnershipChangedWhileWaitingForTheLock(t *testing.T) {
	env := newTestEnv(t)
	entry := env.registry.Add(app.App{ID: "app_1", Owner: ownerOld, State: app.StateIdle})
	entry.Begin()
	done := make(chan error, 1)
	go func() { done <- env.svc.DeleteApp(context.Background(), ownerFresh, "app_1") }()
	time.Sleep(20 * time.Millisecond)
	entry.Update(func(a *app.App) { a.Owner.DeviceID = "other-device" })
	entry.End()

	err := <-done
	if toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
	if _, ok := env.registry.Get("app_1"); !ok {
		t.Fatal("application was removed")
	}
}
