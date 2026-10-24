package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

func TestAdversarialHandlerSuccessLifecycle(t *testing.T) {
	payload := standardArtifact(t, validLaunch(5000))
	files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
	s := newFakeService(t, fakeServiceOptions{})
	h := NewHandler(s)
	port := availableTestPort(t)
	body, err := json.Marshal(api.CreateRequest{AppID: "http-success", ArtifactURL: files.URL + "/a.tar.gz", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/apps", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body.String())
	}
	var created api.Instance
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.State != "ready" || w.Header().Get("Location") != "/v1/apps/"+created.InstanceID {
		t.Fatalf("unexpected create response: %+v location=%q", created, w.Header().Get("Location"))
	}

	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/apps/"+created.InstanceID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status %d", get.Code)
	}

	one := httptest.NewRecorder()
	h.ServeHTTP(one, httptest.NewRequest(http.MethodGet, "/v1/ports/"+strconv.Itoa(port), nil))
	if one.Code != http.StatusOK {
		t.Fatalf("port status %d: %s", one.Code, one.Body.String())
	}

	list := httptest.NewRecorder()
	h.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/ports", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("ports status %d: %s", list.Code, list.Body.String())
	}

	del := httptest.NewRecorder()
	h.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/v1/apps/"+created.InstanceID, nil))
	if del.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", del.Code, del.Body.String())
	}
	var stopped api.Instance
	if err := json.Unmarshal(del.Body.Bytes(), &stopped); err != nil || stopped.State != "stopped" {
		t.Fatalf("bad delete response: %s %v", del.Body.String(), err)
	}

	stream := httptest.NewRecorder()
	h.ServeHTTP(stream, httptest.NewRequest(http.MethodGet, "/streams/"+created.InstanceID+"/live.ts", nil))
	if stream.Code != http.StatusNotFound {
		t.Fatalf("stopped stream status %d", stream.Code)
	}
}

type adversarialWriter struct {
	header   http.Header
	writeErr error
	flushErr error
	notify   chan struct{}
}

func (w *adversarialWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *adversarialWriter) Write(p []byte) (int, error) {
	if w.notify != nil {
		select {
		case w.notify <- struct{}{}:
		default:
		}
	}
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(p), nil
}

func (w *adversarialWriter) WriteHeader(int) {}

func (w *adversarialWriter) FlushError() error { return w.flushErr }

func waitForHubViewer(t *testing.T, h *hub) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.mu.Lock()
		n := len(h.viewers)
		h.mu.Unlock()
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("stream handler never subscribed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAdversarialStreamHandlerExitPaths(t *testing.T) {
	cases := []struct {
		name       string
		writeErr   error
		flushErr   error
		closeAfter bool
	}{
		{name: "closed hub", closeAfter: true},
		{name: "write error", writeErr: errors.New("write failed")},
		{name: "flush error", flushErr: errors.New("flush failed")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testService(t)
			h := NewHandler(s)
			r := &run{info: api.Instance{InstanceID: "stream-id", State: "ready"}, hub: newHub(), cancel: func() {}}
			s.instances[r.info.InstanceID] = r
			writer := &adversarialWriter{writeErr: tc.writeErr, flushErr: tc.flushErr}
			req := httptest.NewRequest(http.MethodGet, "/streams/stream-id/live.ts", nil)
			done := make(chan struct{})
			go func() {
				h.ServeHTTP(writer, req)
				close(done)
			}()
			waitForHubViewer(t, r.hub)
			r.hub.publish([]byte("packet"))
			if tc.closeAfter {
				r.hub.close()
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("stream handler did not exit")
			}
			if got := writer.Header().Get("Content-Type"); got != "video/mp2t" {
				t.Fatalf("content type %q", got)
			}
			delete(s.instances, r.info.InstanceID)
		})
	}
}

func TestAdversarialStreamHandlerUnavailableRequiresReady(t *testing.T) {
	s := testService(t)
	h := NewHandler(s)
	r := &run{info: api.Instance{InstanceID: "starting-id", State: "starting"}, hub: newHub(), cancel: func() {}}
	s.instances[r.info.InstanceID] = r
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/streams/starting-id/live.ts", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("starting stream status %d", w.Code)
	}
	delete(s.instances, r.info.InstanceID)
}

func TestAdversarialStreamHandlerRequestCancellation(t *testing.T) {
	s := testService(t)
	h := NewHandler(s)
	r := &run{info: api.Instance{InstanceID: "cancel-id", State: "ready"}, hub: newHub(), cancel: func() {}}
	s.instances[r.info.InstanceID] = r
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/streams/cancel-id/live.ts", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(&adversarialWriter{}, req)
		close(done)
	}()
	waitForHubViewer(t, r.hub)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler ignored cancellation")
	}
	delete(s.instances, r.info.InstanceID)
}
