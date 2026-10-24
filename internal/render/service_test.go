package render

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

func testService(t *testing.T) *Service {
	t.Helper()
	s, err := New(Config{WorkDir: t.TempDir(), PublicURL: "http://127.0.0.1:8090", Node: "/no-such-node", Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}
func availableTestPort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
func TestReservationsDuringDownload(t *testing.T) {
	s := testService(t)
	entered := make(chan struct{})
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer files.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := api.CreateRequest{AppID: "same-app", ArtifactURL: files.URL, Port: availableTestPort(t)}
	finished := make(chan *Failure, 1)
	go func() { _, err := s.Create(ctx, req); finished <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("download did not start")
	}
	otherPort := req
	otherPort.Port = availableTestPort(t)
	v, err := s.Create(context.Background(), otherPort)
	if err == nil || err.Detail.Code != "APP_ALREADY_RUNNING" || v.AppID != req.AppID || err.InstanceID == "" {
		t.Fatalf("duplicate app: %+v %v", v, err)
	}
	otherApp := req
	otherApp.AppID = "other-app"
	if _, err = s.Create(context.Background(), otherApp); err == nil || err.Detail.Code != "PORT_IN_USE" {
		t.Fatalf("duplicate port: %v", err)
	}
	p, e := s.Port(req.Port)
	if e != nil || !p.Occupied || p.AppID != req.AppID {
		t.Fatalf("missing reservation: %+v %v", p, e)
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not return")
	}
	stopCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	if _, err = s.Stop(stopCtx, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	p, e = s.Port(req.Port)
	if e != nil || p.Occupied {
		t.Fatalf("reservation leaked: %+v %v", p, e)
	}
}
func TestStartupFailureReleasesReservations(t *testing.T) {
	s := testService(t)
	artifact, err := os.ReadFile(fixtureTar(t))
	if err != nil {
		t.Fatal(err)
	}
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(artifact) }))
	defer files.Close()
	req := api.CreateRequest{AppID: "start-fails", ArtifactURL: files.URL, Port: availableTestPort(t)}
	for range 2 {
		v, e := s.Create(context.Background(), req)
		if e == nil || e.Detail.Code != "APP_START_FAILED" || v.State != "error" {
			t.Fatalf("got %+v %v", v, e)
		}
		p, err := s.Port(req.Port)
		if err != nil || p.Occupied {
			t.Fatalf("port leaked %+v %v", p, err)
		}
	}
}
func TestPortExternalOccupancy(t *testing.T) {
	s := testService(t)
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	p, e := s.Port(port)
	if e != nil || !p.Occupied || p.Owner == nil || *p.Owner != "external" {
		t.Fatalf("got %+v %v", p, e)
	}
}
func TestHTTPValidation(t *testing.T) {
	h := NewHandler(testService(t))
	for _, tc := range []struct {
		method, path, body, ct string
		status                 int
	}{
		{"GET", "/healthz", "", "", 200},
		{"GET", "/v1/apps/missing", "", "", 404},
		{"DELETE", "/v1/apps/missing", "", "", 404},
		{"GET", "/v1/ports/0", "", "", 400},
		{"GET", "/v1/ports/nope", "", "", 400},
		{"POST", "/v1/apps", `{}`, "text/plain", 415},
		{"POST", "/v1/apps", `{}`, "application/json", 400},
		{"POST", "/v1/apps", `{} {}`, "application/json", 400},
		{"POST", "/v1/apps", `{"extra":true}`, "application/json", 400},
	} {
		t.Run(tc.method+tc.path+strconv.Itoa(tc.status), func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			r.Header.Set("Content-Type", tc.ct)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
			var obj map[string]any
			if json.Unmarshal(w.Body.Bytes(), &obj) != nil {
				t.Fatal("response not JSON")
			}
		})
	}
}
func TestSlowStreamViewerDoesNotBlockOthers(t *testing.T) {
	h := newHub()
	slow, removeSlow := h.subscribe()
	defer removeSlow()
	fast, removeFast := h.subscribe()
	defer removeFast()
	for i := 0; i < 150; i++ {
		h.publish([]byte{byte(i)})
		select {
		case b := <-fast:
			if b[0] != byte(i) {
				t.Fatal("lost active viewer frame")
			}
		case <-time.After(time.Second):
			t.Fatal("publisher blocked")
		}
	}
	for range slow {
	} // A full queue disconnects the slow viewer.
	h.close()
	if _, ok := <-fast; ok {
		t.Fatal("hub close did not close active viewer")
	}
}
func TestOldCleanupDoesNotReleaseReplacement(t *testing.T) {
	s := testService(t)
	r := &run{info: api.Instance{AppID: "app", InstanceID: "old", Port: 4000}}
	s.apps["app"] = "new"
	s.ports[4000] = "new"
	s.releaseLocked(r)
	if s.apps["app"] != "new" || s.ports[4000] != "new" {
		t.Fatal("replacement reservation released")
	}
	delete(s.apps, "app")
	delete(s.ports, 4000)
}

func TestPageReadinessRejectsErrorAndNonHTML(t *testing.T) {
	for _, tc := range []struct {
		status      int
		contentType string
		wantError   bool
	}{{200, "text/html; charset=utf-8", false}, {404, "text/html", true}, {200, "application/json", true}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", tc.contentType)
			w.WriteHeader(tc.status)
		}))
		err := checkPage(context.Background(), server.URL)
		server.Close()
		if (err != nil) != tc.wantError {
			t.Fatalf("status=%d type=%s: %v", tc.status, tc.contentType, err)
		}
	}
}

func TestDownloadErrorsDoNotLeaveReservations(t *testing.T) {
	s := testService(t)
	for _, status := range []int{404, 500} {
		files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		req := api.CreateRequest{AppID: "download-fails", ArtifactURL: files.URL, Port: availableTestPort(t)}
		_, err := s.Create(context.Background(), req)
		files.Close()
		if err == nil || err.Detail.Code != "ARTIFACT_DOWNLOAD_FAILED" {
			t.Fatalf("unexpected download failure: %v", err)
		}
		p, e := s.Port(req.Port)
		if e != nil || p.Occupied {
			t.Fatalf("failed download retained port: %+v %v", p, e)
		}
	}
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not a gzip archive")) }))
	defer files.Close()
	_, err := s.Create(context.Background(), api.CreateRequest{AppID: "bad-gzip", ArtifactURL: files.URL, Port: availableTestPort(t)})
	if err == nil || err.Detail.Code != "INVALID_ARTIFACT" {
		t.Fatalf("invalid archive: %v", err)
	}
}

func TestPortInspectionFailureIsNotReportedAsFree(t *testing.T) {
	s := testService(t)
	t.Setenv("PATH", t.TempDir())
	h := NewHandler(s)
	for _, path := range []string{"/v1/ports", "/v1/ports/39999"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 503 || !bytes.Contains(w.Body.Bytes(), []byte("PORT_INSPECTION_FAILED")) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestConfigurationAndClosedService(t *testing.T) {
	for _, cfg := range []Config{
		{Width: 321}, {FPS: 31}, {StartupTimeout: time.Millisecond}, {PublicURL: "ftp://example.com"},
		{PublicURL: "http://localhost:8090", AppHost: "bad/host"},
		{PublicURL: "http://localhost:8090"},
	} {
		if s, err := New(cfg); err == nil {
			s.cancel()
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	s := testService(t)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := s.Create(context.Background(), api.CreateRequest{AppID: "after-close", ArtifactURL: "http://localhost/test", Port: 39998})
	if err == nil || err.Detail.Code != "RENDER_STOPPING" {
		t.Fatalf("closed service accepted start: %v", err)
	}
}
