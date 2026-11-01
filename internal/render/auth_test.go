package render

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorizeRequiresBearerOnControlRoutesOnly(t *testing.T) {
	s := testService(t)
	s.cfg.Token = "render-secret"
	handler := NewHandler(s)

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status %d", health.Code)
	}

	stream := httptest.NewRecorder()
	handler.ServeHTTP(stream, httptest.NewRequest(http.MethodGet, "/streams/missing/live.ts", nil))
	if stream.Code != http.StatusNotFound {
		t.Fatalf("stream status %d, want 404", stream.Code)
	}

	for _, header := range []string{"", "Bearer wrong", "Basic render-secret", "Bearer render-secret-extra"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/ports", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("header %q status %d, want 401: %s", header, rec.Code, rec.Body.String())
		}
	}

	ok := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ports", nil)
	req.Header.Set("Authorization", "Bearer render-secret")
	handler.ServeHTTP(ok, req)
	if ok.Code == http.StatusUnauthorized {
		t.Fatalf("valid bearer rejected: %s", ok.Body.String())
	}

	escaped := httptest.NewRecorder()
	handler.ServeHTTP(escaped, httptest.NewRequest(http.MethodGet, "/v1/../healthz", nil))
	if escaped.Code != http.StatusOK {
		t.Fatalf("cleaned health path status %d", escaped.Code)
	}
}

func TestAuthorizeSkipsCheckWhenTokenEmpty(t *testing.T) {
	handler := NewHandler(testService(t))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}
