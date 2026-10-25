package portal

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootSPAServesClientRoutesAndRejectsReservedPrefixes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><div id=root>shell</div>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('app')"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := NewRootSPA(dir)

	for _, path := range []string{"/", "/login", "/portal", "/portal/", "/portal/apps/app_1"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "shell") {
			t.Errorf("%s status=%d body=%q", path, recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s is missing nosniff", path)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "console.log") {
		t.Fatalf("asset status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache-control=%q", recorder.Header().Get("Cache-Control"))
	}

	request = httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing asset status=%d body=%q", recorder.Code, recorder.Body.String())
	}

	for _, path := range []string{"/mcp", "/sse", "/api/portal/screens", "/voice/dev/status", "/artifacts/app_1/file", "/healthz"} {
		request = httptest.NewRequest(http.MethodGet, path, nil)
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s status=%d body=%q", path, recorder.Code, recorder.Body.String())
		}
	}

	request = httptest.NewRequest(http.MethodGet, "/mcpfoo", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "shell") {
		t.Fatalf("mcpfoo status=%d body=%q", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/portal", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /portal status=%d body=%q", recorder.Code, recorder.Body.String())
	}

	missing := NewRootSPA(filepath.Join(t.TempDir(), "nope"))
	request = httptest.NewRequest(http.MethodGet, "/portal", nil)
	recorder = httptest.NewRecorder()
	missing.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "pnpm build") {
		t.Fatalf("missing build status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}
