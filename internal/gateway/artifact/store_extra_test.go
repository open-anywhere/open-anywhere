package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStoreRejectsBadDirectories(t *testing.T) {
	if _, err := NewStore("", "http://gateway", nil); err == nil {
		t.Fatal("empty directory should fail")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(file, "http://gateway", nil); err == nil {
		t.Fatal("file directory should fail")
	}
}

func TestNewStoreSkipsUnsafePersistedIDs(t *testing.T) {
	repo := &fakeRepository{items: []Item{
		{AppID: "../escape", Path: "/tmp/escape"},
		{AppID: "app_1", Path: "/old/path.tar.gz"},
	}}
	store, err := NewStore(t.TempDir(), "http://gateway", repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("../escape"); ok {
		t.Fatal("unsafe id was restored")
	}
	item, ok := store.Get("app_1")
	if !ok || item.Path != filepath.Join(store.dir, "app_1.tar.gz") {
		t.Fatalf("item = %+v", item)
	}
}

func TestSaveFileRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir, "http://gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFile("../bad", "missing", 0, ""); err == nil {
		t.Fatal("invalid app id should fail")
	}
	if _, err := store.SaveFile("app_1", filepath.Join(dir, "missing"), 0, ""); err == nil {
		t.Fatal("missing source should fail")
	}
	data := []byte("artifact")
	source := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if _, err := store.SaveFile("app_1", source, 1, hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("size mismatch should fail")
	}
	if _, err := store.SaveFile("app_1", source, int64(len(data)), "deadbeef"); err == nil {
		t.Fatal("digest mismatch should fail")
	}
	if _, ok := store.Get("app_1"); ok {
		t.Fatal("failed saves should not be recorded")
	}
	item, err := store.SaveFile("app_1", source, int64(len(data)), strings.ToUpper(hex.EncodeToString(digest[:])))
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if item.Size != int64(len(data)) || item.DownloadURL == "" {
		t.Fatalf("item = %+v", item)
	}
}

func TestSafeID(t *testing.T) {
	for _, value := range []string{"app_1", "ABC-123", strings.Repeat("a", 128)} {
		if !safeID(value) {
			t.Fatalf("safeID(%q) = false", value)
		}
	}
	for _, value := range []string{"", "../x", "a/b", "a b", "应用", strings.Repeat("a", 129)} {
		if safeID(value) {
			t.Fatalf("safeID(%q) = true", value)
		}
	}
}

func TestCopyAndDigestErrors(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "target")); err == nil {
		t.Fatal("missing source should fail")
	}
	if err := copyFile(source, dir); err == nil {
		t.Fatal("directory target should fail")
	}
	if _, _, err := fileDigest(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing file should fail")
	}
	if err := copyFile(source, filepath.Join(dir, "target")); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
}

func TestServeDownload(t *testing.T) {
	data := []byte("artifact-bytes")
	source := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	store, err := NewStore(t.TempDir(), "http://gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.SaveFile("app_1", source, int64(len(data)), hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /artifacts/{app_id}/{name}", store.ServeDownload)
	server := httptest.NewServer(mux)
	defer server.Close()

	request := func(path string) *http.Response {
		t.Helper()
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	token := url.QueryEscape(item.Token)
	response := request("/artifacts/app_1/artifact.tar.gz?token=" + token)
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != string(data) {
		t.Fatalf("download = %d %q", response.StatusCode, body)
	}
	if disposition := response.Header.Get("Content-Disposition"); !strings.Contains(disposition, "artifact.tar.gz") {
		t.Fatalf("disposition = %q", disposition)
	}

	for name, path := range map[string]string{
		"wrong token": "/artifacts/app_1/artifact.tar.gz?token=nope",
		"wrong name":  "/artifacts/app_1/other.tar.gz?token=" + token,
		"unknown app": "/artifacts/app_2/artifact.tar.gz?token=" + token,
		"bad request": "/artifacts/app_1/artifact.tar.gz",
	} {
		t.Run(name, func(t *testing.T) {
			response := request(path)
			response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d", response.StatusCode)
			}
		})
	}

	if err := os.Remove(item.Path); err != nil {
		t.Fatal(err)
	}
	response = request("/artifacts/app_1/artifact.tar.gz?token=" + token)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file status = %d", response.StatusCode)
	}
}

func TestStoreGetMissing(t *testing.T) {
	store, err := NewStore(t.TempDir(), "http://gateway", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("missing"); ok {
		t.Fatal("missing item was found")
	}
}
