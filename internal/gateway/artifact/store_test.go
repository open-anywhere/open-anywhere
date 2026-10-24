package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type fakeRepository struct {
	mu      sync.Mutex
	items   []Item
	saveErr error
	listErr error
}

func (r *fakeRepository) SaveArtifact(item Item) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	for index := range r.items {
		if r.items[index].AppID == item.AppID {
			r.items[index] = item
			return nil
		}
	}
	r.items = append(r.items, item)
	return nil
}

func (r *fakeRepository) ListArtifacts() ([]Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]Item(nil), r.items...), nil
}

func TestStoreRestoresPersistedArtifacts(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(t.TempDir(), "artifact.tar.gz")
	data := []byte("artifact-bytes")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	repo := &fakeRepository{}
	store, err := NewStore(dir, "http://gateway:8090", repo)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.SaveFile("app_1", source, int64(len(data)), hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if item.Token == "" || item.DownloadURL == "" {
		t.Fatalf("item = %+v", item)
	}

	restored, err := NewStore(dir, "http://gateway:8090", repo)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := restored.Get("app_1")
	if !ok {
		t.Fatal("artifact was not restored")
	}
	if got.Token != item.Token || got.DownloadURL != item.DownloadURL || got.Path != item.Path {
		t.Fatalf("item = %+v, want %+v", got, item)
	}
}

func TestStoreSaveFileRemovesFileWhenPersistFails(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(t.TempDir(), "artifact.tar.gz")
	data := []byte("artifact-bytes")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	repo := &fakeRepository{saveErr: errors.New("database is down")}
	store, err := NewStore(dir, "http://gateway:8090", repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFile("app_1", source, int64(len(data)), hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("expected a persist error")
	}
	if _, err := os.Stat(filepath.Join(dir, "app_1.tar.gz")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact file = %v", err)
	}
	if _, ok := store.Get("app_1"); ok {
		t.Fatal("artifact should not be recorded")
	}
}

func TestStoreLoadFailure(t *testing.T) {
	if _, err := NewStore(t.TempDir(), "http://gateway:8090", &fakeRepository{listErr: errors.New("boom")}); err == nil {
		t.Fatal("expected a load error")
	}
}
