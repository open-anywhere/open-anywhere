package casting

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sessionJSON = `{"session_id":"ps_1","state":"playing","target_id":"tv_1","endpoint_id":"ep_1","source":{"type":"stream","url":"http://127.0.0.1:39001/stream","format":"mpegts","video_codec":"h264"},"created_at":"2026-01-02T03:04:05Z"}`

func TestClientLifecycle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/targets", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"targets":[{"id":"tv_1","name":"客厅","state":"online","endpoints":[{"id":"ep_1","protocol":"dlna","capabilities":{"formats":["mpegts"],"video_codecs":["h264"],"streaming":true}}]}]}`)
	})
	mux.HandleFunc("GET /v1/targets/tv_1", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"tv_1","name":"客厅","state":"online","endpoints":[]}`)
	})
	mux.HandleFunc("POST /v1/projection-sessions", func(w http.ResponseWriter, r *http.Request) {
		var request CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode create session: %v", err)
			return
		}
		if request.TargetID != "tv_1" || request.EndpointID != "ep_1" || request.Source.Format != "mpegts" {
			t.Errorf("request = %+v", request)
		}
		io.WriteString(w, sessionJSON)
	})
	mux.HandleFunc("GET /v1/projection-sessions/ps_1", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, sessionJSON)
	})
	mux.HandleFunc("DELETE /v1/projection-sessions/ps_1", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"session_id":"ps_1","state":"stopped","target_id":"tv_1","endpoint_id":"ep_1"}`)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	client := NewClient(server.URL, 2*time.Second)

	targets, err := client.ListTargets(ctx)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != "tv_1" || targets[0].State != TargetOnline {
		t.Fatalf("targets = %+v", targets)
	}
	if target, err := client.GetTarget(ctx, "tv_1"); err != nil || target.Name != "客厅" {
		t.Fatalf("target = %+v, %v", target, err)
	}
	session, err := client.CreateSession(ctx, CreateSessionRequest{
		TargetID:   "tv_1",
		EndpointID: "ep_1",
		Source:     StreamSource{Type: "stream", URL: "http://127.0.0.1:39001/stream", Format: "mpegts", VideoCodec: "h264"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID != "ps_1" || session.State != SessionPlaying || session.Source.VideoCodec != "h264" {
		t.Fatalf("session = %+v", session)
	}
	if current, err := client.GetSession(ctx, "ps_1"); err != nil || current.State != SessionPlaying {
		t.Fatalf("session = %+v, %v", current, err)
	}
	stopped, err := client.StopSession(ctx, "ps_1")
	if err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	if stopped.State != SessionStopped {
		t.Fatalf("stopped = %+v", stopped)
	}
	if err := client.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestClientErrorCarriesTheLeakedSessionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"error":{"code":"device_control_failed","message":"start failed","session_id":"ps_leaked"}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	_, err := client.CreateSession(context.Background(), CreateSessionRequest{TargetID: "tv_1", EndpointID: "ep_1"})
	var clientErr *ClientError
	if !errors.As(err, &clientErr) || clientErr.Code != "device_control_failed" || clientErr.SessionID != "ps_leaked" {
		t.Fatalf("error = %#v", err)
	}
}

func TestClientErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/targets", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"code":"INTERNAL","message":"boom"}}`)
	})
	mux.HandleFunc("GET /v1/targets/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "not found")
	})
	mux.HandleFunc("POST /v1/projection-sessions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `{"error":{"code":"TARGET_BUSY","message":"busy"}}`)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	if _, err := client.ListTargets(context.Background()); !IsStatus(err, http.StatusInternalServerError) {
		t.Fatalf("targets error = %#v", err)
	}
	_, err := client.GetTarget(context.Background(), "missing")
	var clientErr *ClientError
	if !errors.As(err, &clientErr) || clientErr.Status != http.StatusNotFound || clientErr.Message != "not found" {
		t.Fatalf("target error = %#v", err)
	}
	_, err = client.CreateSession(context.Background(), CreateSessionRequest{})
	var sessionErr *ClientError
	if !errors.As(err, &sessionErr) || sessionErr.Status != http.StatusConflict || sessionErr.Code != "TARGET_BUSY" {
		t.Fatalf("session error = %#v", err)
	}
	if err := client.Health(context.Background()); !IsStatus(err, http.StatusServiceUnavailable) {
		t.Fatalf("health error = %#v", err)
	}
}
