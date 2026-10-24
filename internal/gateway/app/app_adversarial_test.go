package app

import (
	"errors"
	"strings"
	"testing"
)

type deleteFailPersister struct{}

func (deleteFailPersister) SaveApp(App) error { return nil }
func (deleteFailPersister) DeleteApp(string) error {
	return errors.New("disk unavailable")
}

func TestRecordLifecycleLockRoundTrip(t *testing.T) {
	record := &Record{}
	record.Begin()
	record.End()
	// The lock must be usable again after being released.
	record.Begin()
	record.End()
}

func TestLoadAssignsRevisionOne(t *testing.T) {
	registry := NewRegistry()
	registry.Load([]App{{ID: "app_1", Revision: 0}, {ID: ""}})
	entry, ok := registry.Get("app_1")
	if !ok {
		t.Fatal("the app was not loaded")
	}
	if entry.Snapshot().Revision != 1 {
		t.Fatalf("revision = %d", entry.Snapshot().Revision)
	}
	if _, ok := registry.Get(""); ok {
		t.Fatal("an app without an id was loaded")
	}
}

func TestRemoveLogsAStoreFailure(t *testing.T) {
	registry := NewRegistry()
	var logs []string
	registry.logf = func(format string, args ...any) { logs = append(logs, format) }
	registry.Add(App{ID: "app_1"})
	registry.SetPersister(deleteFailPersister{})

	registry.Remove("app_1")

	if _, ok := registry.Get("app_1"); ok {
		t.Fatal("the app was not removed from memory")
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "delete app") {
		t.Fatalf("logs = %v", logs)
	}
	// Removing an unknown id is a no-op and must not touch the store.
	registry.Remove("app_1")
	if len(logs) != 1 {
		t.Fatalf("logs = %v", logs)
	}
}

func TestRunningOnScreenReturnsFalseWithoutAMatch(t *testing.T) {
	registry := NewRegistry()
	registry.Add(App{ID: "app_1", State: StateIdle})
	if value, ok := registry.RunningOnScreen("tv_1"); ok {
		t.Fatalf("unexpected running app: %+v", value)
	}
	registry.Add(App{ID: "app_2", State: StateRunning, ScreenID: "tv_2"})
	if _, ok := registry.RunningOnScreen("tv_1"); ok {
		t.Fatal("a different screen was reported as busy")
	}
}
