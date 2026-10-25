package portal

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSecurityHeadersAddHSTSOnlyForTLSOrSecureConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><div id=root></div>"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := NewAPI(nil, nil, nil, nil, nil, dir)
	web := api.WebHandler()

	assertHSTS := func(name string, handler http.Handler, path string, tlsRequest, want bool) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if tlsRequest {
			request.TLS = &tls.ConnectionState{}
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		got := recorder.Header().Get("Strict-Transport-Security")
		if want && got == "" {
			t.Fatalf("%s: missing Strict-Transport-Security", name)
		}
		if !want && got != "" {
			t.Fatalf("%s: unexpected Strict-Transport-Security %q on plain HTTP", name, got)
		}
	}

	assertHSTS("plain http", web, "/", false, false)
	assertHSTS("tls", web, "/", true, true)

	api.SetCookieSecure(true)
	assertHSTS("secure config", web, "/", false, true)
	assertHSTS("api", api.Handler(), "/api/portal/unknown", false, true)
}
