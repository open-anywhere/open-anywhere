//go:build portalembed

package portal

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedWebPresentWithTag(t *testing.T) {
	fsys := embeddedWebFS()
	if fsys == nil {
		t.Fatal("embedded frontend is missing; run `make portal-build` before a tagged build")
	}
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		t.Fatalf("embedded index.html: %v", err)
	}
}

func TestEmbeddedWebServesPortal(t *testing.T) {
	handler := NewRootSPA("")

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("/login status=%d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `id="root"`) {
		t.Fatalf("/login did not return the embedded shell: %q", recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/assets/does-not-exist.js", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing embedded asset status=%d", recorder.Code)
	}
}
