package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

type fakeArtifacts struct {
	mu      sync.Mutex
	missing map[string]bool
	removed []string
}

func (f *fakeArtifacts) Has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.missing[id]
}

func (f *fakeArtifacts) Remove(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeArtifacts) removedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

var (
	ownerOld   = app.Identity{DeviceID: "device", AgentID: "agent", SessionID: "old-session"}
	ownerFresh = app.Identity{DeviceID: "device", AgentID: "agent", SessionID: "fresh-session"}
	baseTime   = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
)

// builtApp is an application that built successfully, built minutes after baseTime.
func builtApp(t *testing.T, id string, minutes int, state app.State) app.App {
	t.Helper()
	started := baseTime.Add(time.Duration(minutes) * time.Minute)
	return app.App{
		ID:              id,
		Owner:           ownerOld,
		Name:            "name-" + id,
		Prompt:          "做一个游戏 " + id,
		State:           state,
		OpenAPI:         mustSpec(t),
		BuildStartedAt:  started,
		BuildFinishedAt: started.Add(2 * time.Minute),
		BuildDuration:   2 * time.Minute,
		Artifact:        app.Artifact{Path: "artifacts/" + id + ".tar.gz", DownloadURL: "http://gateway/artifacts/" + id + "/artifact.tar.gz?token=x"},
	}
}

func TestAppsBelongToTheDeviceAndAgentNotTheSession(t *testing.T) {
	env := newTestEnv(t, nil)
	env.registry.Add(builtApp(t, "app_a", 0, app.StateIdle))
	ctx := context.Background()

	if _, err := env.svc.GetAppStatus(ctx, ownerFresh, "app_a"); err != nil {
		t.Fatalf("a new session of the same device and agent lost the app: %v", err)
	}
	if view, err := env.svc.GetAppOperations(ctx, ownerFresh, "app_a"); err != nil || len(view.Operations) != 1 {
		t.Fatalf("operations=%+v err=%v", view, err)
	}
	if doc, err := env.svc.GetOpenAPI(ctx, ownerFresh, "app_a"); err != nil || doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi=%v err=%v", doc, err)
	}
	list, err := env.svc.ListApps(ctx, ownerFresh)
	if err != nil || len(list) != 1 || list[0].AppID != "app_a" {
		t.Fatalf("list=%+v err=%v", list, err)
	}

	for name, stranger := range map[string]app.Identity{
		"other agent":  {DeviceID: "device", AgentID: "other", SessionID: "s"},
		"other device": {DeviceID: "other", AgentID: "agent", SessionID: "s"},
		"no agent":     {DeviceID: "device", SessionID: "s"},
	} {
		if _, err := env.svc.GetAppStatus(ctx, stranger, "app_a"); toolCode(t, err) != "APP_NOT_FOUND" {
			t.Errorf("%s can see the app", name)
		}
		if list, _ := env.svc.ListApps(ctx, stranger); len(list) != 0 {
			t.Errorf("%s lists %d apps", name, len(list))
		}
	}
	if _, err := env.svc.ListApps(ctx, app.Identity{AgentID: "agent", SessionID: "s"}); toolCode(t, err) != "MISSING_CONTEXT" {
		t.Fatal("a request without a device must be refused")
	}
}

func TestListAppsSummarizesAndOrdersTheHistory(t *testing.T) {
	env := newTestEnv(t, nil)
	env.svc.SetArtifactStore(&fakeArtifacts{missing: map[string]bool{"app_lost": true}})
	old := builtApp(t, "app_old", 0, app.StateIdle)
	old.Prompt = strings.Repeat("很长的需求", 30)
	running := builtApp(t, "app_run", 10, app.StateRunning)
	running.ScreenID = "tv_1"
	building := app.App{ID: "app_new", Owner: ownerOld, State: app.StateBuilding, BuildStartedAt: baseTime.Add(30 * time.Minute)}
	failedBuild := app.App{ID: "app_bad", Owner: ownerOld, State: app.StateError, BuildStartedAt: baseTime.Add(20 * time.Minute), Error: &app.Error{Stage: app.StageBuilding, Code: "BUILD_FAILED", Message: "构建被中断"}}
	failedStart := builtApp(t, "app_retry", 15, app.StateError)
	failedStart.Error = &app.Error{Stage: app.StageStarting, Code: "PROJECTION_TIMEOUT", Message: "无法建立投屏"}
	lost := builtApp(t, "app_lost", 5, app.StateIdle)
	for _, a := range []app.App{old, running, building, failedBuild, failedStart, lost} {
		env.registry.Add(a)
	}

	list, err := env.svc.ListApps(context.Background(), ownerFresh)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	byID := map[string]AppSummary{}
	for _, item := range list {
		order = append(order, item.AppID)
		byID[item.AppID] = item
	}
	if want := "app_new app_bad app_retry app_run app_lost app_old"; strings.Join(order, " ") != want {
		t.Fatalf("order=%v want %s", order, want)
	}

	check := func(id string, startable bool, reason string) {
		t.Helper()
		got := byID[id]
		if got.Startable != startable || !strings.Contains(got.NotStartableReason, reason) {
			t.Errorf("%s: startable=%v reason=%q, want %v %q", id, got.Startable, got.NotStartableReason, startable, reason)
		}
	}
	check("app_old", true, "")
	check("app_retry", true, "")
	check("app_new", false, "构建")
	check("app_bad", false, "重新创建")
	check("app_run", false, "运行")
	check("app_lost", false, "丢失")

	oldView := byID["app_old"]
	if oldView.OperationCount != 1 || len(oldView.Operations) != 1 || oldView.Operations[0] != "updateItem" {
		t.Errorf("operations=%+v", oldView)
	}
	if oldView.BuiltAt == nil || oldView.BuildDurationSeconds != 120 || oldView.Name != "name-app_old" {
		t.Errorf("build info=%+v", oldView)
	}
	if n := len([]rune(oldView.Summary)); n != 81 || !strings.HasSuffix(oldView.Summary, "…") {
		t.Errorf("summary has %d runes: %q", n, oldView.Summary)
	}
	if byID["app_run"].ScreenID != "tv_1" || byID["app_retry"].Error == nil || byID["app_retry"].Error.Code != "PROJECTION_TIMEOUT" {
		t.Errorf("run/retry=%+v / %+v", byID["app_run"], byID["app_retry"])
	}
}

func TestStartRetriesABuiltAppWhoseLastStartFailed(t *testing.T) {
	env := newTestEnv(t, nil)
	env.svc.SetArtifactStore(&fakeArtifacts{})
	failed := builtApp(t, "app_1", 0, app.StateError)
	failed.Error = &app.Error{Stage: app.StageStarting, Code: "PROJECTION_TIMEOUT", Message: "无法建立投屏"}
	failed.InstanceID, failed.Port, failed.ScreenID, failed.CleanupPending = "inst_stale", 39000, "tv_1", true
	entry := env.registry.Add(failed)

	view, err := env.svc.StartApp(context.Background(), ownerFresh, "app_1", "tv_1", false)
	if err != nil || view.State != app.StateRunning {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if got := env.render.deletedIDs(); len(got) != 1 || got[0] != "inst_stale" {
		t.Fatalf("the instance left by the failed start was not released: %v", got)
	}
	snapshot := entry.Snapshot()
	if snapshot.Error != nil || snapshot.InstanceID != "inst_1" || snapshot.CleanupPending {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestStartRefusesWhatCannotBeStarted(t *testing.T) {
	env := newTestEnv(t, nil)
	env.svc.SetArtifactStore(&fakeArtifacts{missing: map[string]bool{"app_lost": true}})
	failedBuild := builtApp(t, "app_bad", 0, app.StateError)
	failedBuild.Error = &app.Error{Stage: app.StageBuilding, Code: "BUILD_FAILED", Message: "构建被中断"}
	failedBuild.OpenAPI = nil
	env.registry.Add(failedBuild)
	env.registry.Add(builtApp(t, "app_lost", 1, app.StateIdle))
	ctx := context.Background()

	if _, err := env.svc.StartApp(ctx, ownerFresh, "app_bad", "tv_1", false); toolCode(t, err) != "APP_IN_ERROR" {
		t.Fatalf("a failed build must stay unstartable, err=%v", err)
	}
	if _, err := env.svc.StartApp(ctx, ownerFresh, "app_lost", "tv_1", false); toolCode(t, err) != "ARTIFACT_MISSING" {
		t.Fatalf("a lost package must be reported, err=%v", err)
	}
	if len(env.render.requests()) != 0 {
		t.Fatal("Render was asked to start something that cannot start")
	}
}

func TestOperationsStayVisibleAfterAFailedStartButNotAfterAFailedBuild(t *testing.T) {
	env := newTestEnv(t, nil)
	failedStart := builtApp(t, "app_start", 0, app.StateError)
	failedStart.Error = &app.Error{Stage: app.StageStarting, Code: "PROJECTION_TIMEOUT"}
	failedBuild := app.App{ID: "app_build", Owner: ownerOld, State: app.StateError, Error: &app.Error{Stage: app.StageBuilding, Code: "BUILD_FAILED"}}
	env.registry.Add(failedStart)
	env.registry.Add(failedBuild)
	ctx := context.Background()

	if view, err := env.svc.GetAppOperations(ctx, ownerFresh, "app_start"); err != nil || len(view.Operations) != 1 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if _, err := env.svc.GetOpenAPI(ctx, ownerFresh, "app_start"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.GetAppOperations(ctx, ownerFresh, "app_build"); toolCode(t, err) != "APP_IN_ERROR" {
		t.Fatalf("err=%v", err)
	}
	if _, err := env.svc.GetOpenAPI(ctx, ownerFresh, "app_build"); toolCode(t, err) != "APP_IN_ERROR" {
		t.Fatalf("err=%v", err)
	}
}

func TestPruneHistoryKeepsTheNewestTenAndNeverTouchesLiveApps(t *testing.T) {
	env := newTestEnv(t, nil)
	artifacts := &fakeArtifacts{}
	env.svc.SetArtifactStore(artifacts)
	// 12 idle apps built at minutes 10..21, plus older apps that must survive or go
	for i := 0; i < 12; i++ {
		env.registry.Add(builtApp(t, fmt.Sprintf("app_%02d", i), 10+i, app.StateIdle))
	}
	running := builtApp(t, "app_running_old", 0, app.StateRunning)
	building := app.App{ID: "app_building_old", Owner: ownerOld, State: app.StateBuilding, BuildStartedAt: baseTime.Add(time.Minute)}
	holding := builtApp(t, "app_holding_old", 2, app.StateError)
	holding.CleanupPending, holding.InstanceID = true, "inst_x"
	failedOld := app.App{ID: "app_failed_old", Owner: ownerOld, State: app.StateError, BuildStartedAt: baseTime.Add(3 * time.Minute), Error: &app.Error{Stage: app.StageBuilding}}
	for _, a := range []app.App{running, building, holding, failedOld} {
		env.registry.Add(a)
	}

	env.svc.PruneHistory()

	var left []string
	for _, a := range env.registry.List() {
		left = append(left, a.ID)
	}
	has := func(id string) bool {
		for _, got := range left {
			if got == id {
				return true
			}
		}
		return false
	}
	for _, id := range []string{"app_11", "app_10", "app_09", "app_08", "app_07", "app_06", "app_05", "app_04", "app_03", "app_02"} {
		if !has(id) {
			t.Errorf("%s is among the newest ten but was removed", id)
		}
	}
	for _, id := range []string{"app_01", "app_00", "app_failed_old"} {
		if has(id) {
			t.Errorf("%s should have been pruned", id)
		}
	}
	for _, id := range []string{"app_running_old", "app_building_old", "app_holding_old"} {
		if !has(id) {
			t.Errorf("%s is live or holds resources and must stay", id)
		}
	}
	if got := artifacts.removedIDs(); len(got) != 3 {
		t.Fatalf("removed packages=%v", got)
	}
}

func TestPruneHistorySkipsWhileAStartIsInProgress(t *testing.T) {
	env := newTestEnv(t, nil)
	for i := 0; i < 12; i++ {
		env.registry.Add(builtApp(t, fmt.Sprintf("app_%02d", i), i, app.StateIdle))
	}
	env.svc.lifecycle.Lock()
	env.svc.PruneHistory()
	env.svc.lifecycle.Unlock()
	if got := len(env.registry.List()); got != 12 {
		t.Fatalf("%d apps left, the prune should have waited", got)
	}
}

func TestCreateAppPrunesTheOldestBeyondTen(t *testing.T) {
	env := newTestEnv(t, nil)
	artifacts := &fakeArtifacts{}
	env.svc.SetArtifactStore(artifacts)
	for i := 0; i < 10; i++ {
		env.registry.Add(builtApp(t, fmt.Sprintf("app_%02d", i), i, app.StateIdle))
	}
	if _, err := env.svc.CreateApp(context.Background(), ownerFresh, "再做一个", nil); err != nil {
		t.Fatal(err)
	}
	if got := len(env.registry.List()); got != 10 {
		t.Fatalf("%d apps, want 10", got)
	}
	if _, ok := env.registry.Get("app_00"); ok {
		t.Fatal("the oldest application was kept")
	}
	if got := artifacts.removedIDs(); len(got) != 1 || got[0] != "app_00" {
		t.Fatalf("removed packages=%v", got)
	}
}
