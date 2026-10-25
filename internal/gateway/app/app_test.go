package app

import (
	"errors"
	"testing"
)

func TestRegistryOwnershipAndQueries(t *testing.T) {
	registry := NewRegistry()
	owner := Identity{DeviceID: "device", SessionID: "session"}
	record := registry.Add(App{ID: "app_1", Owner: owner, State: StateBuilding})
	other := registry.Add(App{ID: "app_2", Owner: Identity{DeviceID: "device", SessionID: "other"}, State: StateRunning, ScreenID: "tv"})

	if got, ok := registry.Get("app_1"); !ok || !got.Snapshot().Owner.SameOwner(owner) {
		t.Fatalf("app_1 owner = %+v", got)
	}
	if !record.Snapshot().Owner.SameOwner(Identity{DeviceID: "device", SessionID: "reconnected"}) {
		t.Fatal("same device with a new Linx session must still own the app")
	}
	if running, ok := registry.RunningOnScreen("tv"); !ok || running.ID != "app_2" {
		t.Fatalf("RunningOnScreen = %+v, %v", running, ok)
	}

	record.Update(func(a *App) {
		a.State = StateRunning
		a.ScreenID = "tv"
	})
	if got := record.Snapshot(); got.State != StateRunning || got.ScreenID != "tv" {
		t.Fatalf("snapshot = %+v", got)
	}

	registry.Remove("app_2")
	if _, ok := registry.Get("app_2"); ok {
		t.Fatal("app_2 should be removed")
	}
	_ = other
}

func TestRegistryAddIsIdempotent(t *testing.T) {
	registry := NewRegistry()
	first := registry.Add(App{ID: "app"})
	second := registry.Add(App{ID: "app"})
	if first != second {
		t.Fatal("Add should return the existing record")
	}
	if len(registry.List()) != 1 {
		t.Fatalf("list = %d", len(registry.List()))
	}
}

func TestRecordDoReturnsError(t *testing.T) {
	record := &Record{app: App{ID: "app"}}
	want := errors.New("test")
	if err := record.Do(func(a *App) error { a.Name = "name"; return want }); !errors.Is(err, want) {
		t.Fatalf("Do error = %v", err)
	}
	if record.Snapshot().Name != "" {
		t.Fatal("failed transaction published mutation")
	}
}

func TestBuildingMessage(t *testing.T) {
	value := App{}
	if value.BuildingMessage() != "正在构建" {
		t.Fatalf("message = %q", value.BuildingMessage())
	}
	value.LastStatusMessage = "status"
	if value.BuildingMessage() != "status" {
		t.Fatalf("message = %q", value.BuildingMessage())
	}
	value.LastAgentMessage = "agent"
	if value.BuildingMessage() != "agent" {
		t.Fatalf("message = %q", value.BuildingMessage())
	}
	value.LastProgress = "progress"
	if value.BuildingMessage() != "progress" {
		t.Fatalf("message = %q", value.BuildingMessage())
	}
}

func TestIdentity(t *testing.T) {
	a := Identity{DeviceID: "d", AgentID: "agent", SessionID: "s"}
	sameOwnerNewSession := Identity{DeviceID: "d", AgentID: "agent", SessionID: "x"}
	otherAgent := Identity{DeviceID: "d", AgentID: "other", SessionID: "s"}
	otherDevice := Identity{DeviceID: "e", AgentID: "agent", SessionID: "s"}
	if !a.SameOwner(sameOwnerNewSession) {
		t.Fatal("a new session of the same device and agent must keep ownership")
	}
	if a.SameOwner(otherAgent) || a.SameOwner(otherDevice) || (Identity{}).SameOwner(Identity{}) {
		t.Fatal("a different agent or device, or no device at all, must not match")
	}
	first, second := NewID(), NewID()
	if first == second {
		t.Fatal("IDs should be unique")
	}
}
