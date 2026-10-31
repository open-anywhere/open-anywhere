package service

import (
	"context"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

func TestGetScreensReportsAnExternalSession(t *testing.T) {
	env := newTestEnv(t, nil)
	target := testTarget()
	target.ActiveSessionID = "foreign"
	env.casting.targets = []casting.Target{target}
	views, err := env.svc.GetScreens(context.Background(), ownerFresh)
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	if views[0].Status != ScreenExternal || views[0].CurrentApp != nil {
		t.Fatalf("view = %+v", views[0])
	}
}

func TestGetScreensReportsARunningAppAsShowingApp(t *testing.T) {
	env := newTestEnv(t, nil)
	target := testTarget()
	target.ActiveSessionID = "ps_1"
	env.casting.targets = []casting.Target{target}
	env.registry.Add(runningApp(ownerFresh))
	views, err := env.svc.GetScreens(context.Background(), ownerFresh)
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	if views[0].Status != ScreenShowingApp || views[0].CurrentApp == nil || views[0].CurrentApp.AppID != "app_1" {
		t.Fatalf("view = %+v", views[0])
	}
}

func TestGetScreensReportsAnOfflineScreenAsUnavailable(t *testing.T) {
	env := newTestEnv(t, nil)
	target := testTarget()
	target.State = casting.TargetOffline
	target.ActiveSessionID = "foreign"
	env.casting.targets = []casting.Target{target}
	views, err := env.svc.GetScreens(context.Background(), ownerFresh)
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	if views[0].Status != ScreenUnavailable || views[0].CurrentApp != nil {
		t.Fatalf("view = %+v", views[0])
	}
}

func TestStartAppRefusesAScreenWithAnUnknownBusySession(t *testing.T) {
	env := newTestEnv(t, nil)
	env.casting.target.ActiveSessionID = "foreign"
	entry := env.registry.Add(idleApp(t, ownerFresh))
	if _, err := env.svc.startAppOnScreen(context.Background(), ownerFresh, "app_1", "tv_1", false); toolCode(t, err) != "TARGET_BUSY" {
		t.Fatalf("err = %v", err)
	}
	if entry.Snapshot().State != app.StateIdle {
		t.Fatal("app state changed on TARGET_BUSY")
	}
	if created := env.render.requests(); len(created) != 0 {
		t.Fatalf("a Render instance was started for a screen that cannot be taken over: %+v", created)
	}
}

func TestGetScreensForShowsTheCallersApp(t *testing.T) {
	env := newTestEnv(t, nil)
	target := testTarget()
	target.ActiveSessionID = "ps_1"
	env.casting.targets = []casting.Target{target}
	env.registry.Add(runningApp(ownerFresh))
	views, err := env.svc.GetScreensFor(context.Background(), []app.Identity{ownerFresh})
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	if views[0].Status != ScreenShowingApp || views[0].CurrentApp == nil || views[0].CurrentApp.AppID != "app_1" {
		t.Fatalf("view = %+v", views[0])
	}
	if !views[0].Online {
		t.Fatalf("online = false, view = %+v", views[0])
	}
}

func TestGetScreensForDoesNotIdentifyAnotherOwnersApp(t *testing.T) {
	env := newTestEnv(t, nil)
	target := testTarget()
	target.ActiveSessionID = "ps_other"
	env.casting.targets = []casting.Target{target}
	other := app.Identity{DeviceID: "other-device", AgentID: "other-agent"}
	env.registry.Add(runningApp(other))
	views, err := env.svc.GetScreensFor(context.Background(), []app.Identity{ownerFresh})
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	if views[0].Status != ScreenExternal || views[0].CurrentApp != nil {
		t.Fatalf("view = %+v", views[0])
	}
}

func TestGetScreensForKeepsAnOwnedAppOnAnOfflineScreen(t *testing.T) {
	env := newTestEnv(t, nil)
	target := testTarget()
	target.State = casting.TargetOffline
	env.casting.targets = []casting.Target{target}
	env.registry.Add(runningApp(ownerFresh))
	views, err := env.svc.GetScreensFor(context.Background(), []app.Identity{ownerFresh})
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	if views[0].Status != ScreenUnavailable || views[0].CurrentApp == nil || views[0].CurrentApp.AppID != "app_1" {
		t.Fatalf("view = %+v", views[0])
	}
	if views[0].Online {
		t.Fatalf("online = true, view = %+v", views[0])
	}
}

func TestStopAppOnScreenStopsTheCallersApp(t *testing.T) {
	env := newTestEnv(t, nil)
	env.registry.Add(runningApp(ownerFresh))
	view, err := env.svc.StopAppOnScreen(context.Background(), []app.Identity{ownerFresh}, "tv_1")
	if err != nil {
		t.Fatalf("StopAppOnScreen: %v", err)
	}
	if view.State != app.StateIdle || view.ScreenID != "" {
		t.Fatalf("view = %+v", view)
	}
	if stopped := env.casting.stoppedIDs(); len(stopped) != 1 || stopped[0] != "ps_1" {
		t.Fatalf("stopped = %v", stopped)
	}
	if deleted := env.render.deletedIDs(); len(deleted) != 1 || deleted[0] != "inst_1" {
		t.Fatalf("deleted = %v", deleted)
	}
}

func TestStopAppOnScreenRefusesAnotherOwnersApp(t *testing.T) {
	env := newTestEnv(t, nil)
	other := app.Identity{DeviceID: "other-device", AgentID: "other-agent"}
	entry := env.registry.Add(runningApp(other))
	_, err := env.svc.StopAppOnScreen(context.Background(), []app.Identity{ownerFresh}, "tv_1")
	if code := toolCode(t, err); code != "SCREEN_APP_NOT_FOUND" {
		t.Fatalf("code = %s", code)
	}
	if entry.Snapshot().State != app.StateRunning {
		t.Fatal("another owner's app was stopped")
	}
	if stopped := env.casting.stoppedIDs(); len(stopped) != 0 {
		t.Fatalf("stopped = %v", stopped)
	}
}
