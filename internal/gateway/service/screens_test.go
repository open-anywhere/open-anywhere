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
	if _, err := env.svc.StartApp(context.Background(), ownerFresh, "app_1", "tv_1", false); toolCode(t, err) != "TARGET_BUSY" {
		t.Fatalf("err = %v", err)
	}
	if entry.Snapshot().State != app.StateIdle {
		t.Fatal("app state changed on TARGET_BUSY")
	}
	if created := env.render.requests(); len(created) != 0 {
		t.Fatalf("a Render instance was started for a screen that cannot be taken over: %+v", created)
	}
}
