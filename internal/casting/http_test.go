package casting

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

func httpCall(t *testing.T, h http.Handler, method, path, body, ct string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestHTTPWorkflow(t *testing.T) {
	s, _ := setup(&fakeAdapter{state: api.SessionPlaying})
	h := NewHandler(s)
	for _, path := range []string{"/healthz", "/v1/targets", "/v1/targets/tv"} {
		w := httpCall(t, h, "GET", path, "", "")
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	req, _ := json.Marshal(testRequest())
	w := httpCall(t, h, "POST", "/v1/projection-sessions", string(req), "application/json; charset=utf-8")
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var session api.Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	path := "/v1/projection-sessions/" + session.ID
	if w.Header().Get("Location") != path {
		t.Fatal("missing Location")
	}
	for _, method := range []string{"GET", "DELETE", "DELETE"} {
		w = httpCall(t, h, method, path, "", "")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body)
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil || session.State != api.SessionStopped {
		t.Fatalf("%+v %v", session, err)
	}
	empty := httpCall(t, NewHandler(sEmpty()), "GET", "/v1/targets", "", "")
	if strings.TrimSpace(empty.Body.String()) != `{"targets":[]}` {
		t.Fatal(empty.Body.String())
	}
}
func TestHTTPRejectsInvalidInput(t *testing.T) {
	s, _ := setup(&fakeAdapter{})
	h := NewHandler(s)
	tests := []struct {
		name, method, path, body, ct string
		status                       int
	}{
		{"missing content type", "POST", "/v1/projection-sessions", "{}", "", 415},
		{"wrong content type", "POST", "/v1/projection-sessions", "{}", "text/plain", 415},
		{"malformed JSON", "POST", "/v1/projection-sessions", "{", "application/json", 400},
		{"empty body", "POST", "/v1/projection-sessions", "", "application/json", 400},
		{"unknown field", "POST", "/v1/projection-sessions", `{"extra":1}`, "application/json", 400},
		{"multiple documents", "POST", "/v1/projection-sessions", "{} {}", "application/json", 400},
		{"trailing invalid JSON", "POST", "/v1/projection-sessions", "{} garbage", "application/json", 400},
		{"oversize trailing whitespace", "POST", "/v1/projection-sessions", "{}" + strings.Repeat(" ", 70<<10), "application/json", 413},
		{"null request", "POST", "/v1/projection-sessions", "null", "application/json", 400},
		{"oversize body", "POST", "/v1/projection-sessions", `{"target_id":"` + strings.Repeat("x", 70<<10) + `"}`, "application/json", 413},
		{"missing target", "GET", "/v1/targets/missing", "", "", 404},
		{"missing session", "GET", "/v1/projection-sessions/missing", "", "", 404},
		{"missing stop", "DELETE", "/v1/projection-sessions/missing", "", "", 404},
		{"method", "PUT", "/v1/targets", "", "", 405},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httpCall(t, h, tt.method, tt.path, tt.body, tt.ct)
			if w.Code != tt.status {
				t.Fatalf("want %d, got %d %s", tt.status, w.Code, w.Body)
			}
		})
	}
}
func TestHTTPServiceErrors(t *testing.T) {
	tests := []struct {
		name   string
		want   int
		change func(*api.CreateSessionRequest, *fakeAdapter, *Registry)
	}{
		{"not found", 404, func(r *api.CreateSessionRequest, _ *fakeAdapter, _ *Registry) { r.TargetID = "missing" }},
		{"unsupported", 422, func(r *api.CreateSessionRequest, _ *fakeAdapter, _ *Registry) { r.Source.VideoCodec = "vp9" }},
		{"offline", 409, func(_ *api.CreateSessionRequest, _ *fakeAdapter, r *Registry) {
			d := testDevice()
			d.Target.State = api.TargetOffline
			r.Upsert(d)
		}},
		{"upstream", 502, func(_ *api.CreateSessionRequest, a *fakeAdapter, _ *Registry) {
			a.startErr = errors.New("device timeout")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &fakeAdapter{}
			s, r := setup(a)
			req := testRequest()
			tt.change(&req, a, r)
			data, _ := json.Marshal(req)
			w := httpCall(t, NewHandler(s), "POST", "/v1/projection-sessions", string(data), "application/json")
			if w.Code != tt.want {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["error"] == nil {
				t.Fatal("missing structured error")
			}
		})
	}
	t.Run("busy", func(t *testing.T) {
		s, _ := setup(&fakeAdapter{state: api.SessionPlaying})
		h := NewHandler(s)
		b, _ := json.Marshal(testRequest())
		for i := range 2 {
			w := httpCall(t, h, "POST", "/v1/projection-sessions", string(b), "application/json")
			if i == 1 && w.Code != 409 {
				t.Fatal(w.Code)
			}
		}
	})
}

// Exercise handlers over a real local HTTP connection as well as recorder tests.
func TestHTTPServer(t *testing.T) {
	server := httptest.NewServer(NewHandler(sEmpty()))
	defer server.Close()
	resp, err := http.Get(server.URL + "/v1/targets")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || !bytes.Contains(body, []byte(`"targets":[]`)) {
		t.Fatalf("%s %v", body, err)
	}
}
