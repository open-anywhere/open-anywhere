package app

import (
	"sync"
	"testing"
)

type fakePersister struct {
	mu      sync.Mutex
	saved   []App
	deleted []string
}

func (p *fakePersister) SaveApp(value App) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.saved = append(p.saved, value)
	return nil
}

func (p *fakePersister) DeleteApp(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deleted = append(p.deleted, id)
	return nil
}

func (p *fakePersister) snapshots() []App {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]App(nil), p.saved...)
}

func (p *fakePersister) deletions() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.deleted...)
}

func TestRegistryPersistsMutations(t *testing.T) {
	registry := NewRegistry()
	registry.logf = func(string, ...any) {}
	persister := &fakePersister{}
	registry.SetPersister(persister)

	record := registry.Add(App{ID: "app_1", Owner: Identity{DeviceID: "d", SessionID: "s"}, State: StateBuilding})
	saved := persister.snapshots()
	if len(saved) != 1 || saved[0].ID != "app_1" || saved[0].Revision != 1 {
		t.Fatalf("saved = %+v", saved)
	}
	record.Update(func(a *App) { a.State = StateIdle })
	saved = persister.snapshots()
	if len(saved) != 2 || saved[1].State != StateIdle || saved[1].Revision != 2 {
		t.Fatalf("saved = %+v", saved)
	}
	if err := record.Do(func(a *App) error { a.Name = "quiz"; return nil }); err != nil {
		t.Fatal(err)
	}
	saved = persister.snapshots()
	if len(saved) != 3 || saved[2].Name != "quiz" || saved[2].Revision != 3 {
		t.Fatalf("saved = %+v", saved)
	}

	registry.Remove("app_1")
	if deletions := persister.deletions(); len(deletions) != 1 || deletions[0] != "app_1" {
		t.Fatalf("deletions = %+v", deletions)
	}
	registry.Remove("app_1")
	if deletions := persister.deletions(); len(deletions) != 1 {
		t.Fatalf("deletions = %+v", deletions)
	}
}

func TestRegistryLoadDoesNotRewrite(t *testing.T) {
	registry := NewRegistry()
	registry.logf = func(string, ...any) {}
	persister := &fakePersister{}
	registry.SetPersister(persister)
	registry.Load([]App{
		{ID: "app_1", Name: "loaded", Revision: 7},
		{ID: "app_1", Name: "duplicate"},
		{ID: ""},
	})
	if saved := persister.snapshots(); len(saved) != 0 {
		t.Fatalf("saved = %+v", saved)
	}
	record, ok := registry.Get("app_1")
	if !ok {
		t.Fatal("app_1 was not loaded")
	}
	if got := record.Snapshot(); got.Name != "loaded" || got.Revision != 7 {
		t.Fatalf("snapshot = %+v", got)
	}
	if existing := registry.Add(App{ID: "app_1", Name: "new"}); existing != record {
		t.Fatal("Add should return the loaded record")
	}
	if len(registry.List()) != 1 {
		t.Fatalf("list = %+v", registry.List())
	}
}
