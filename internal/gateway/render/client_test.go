package render

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

const instanceJSON = `{"app_id":"app_1","instance_id":"inst_1","state":"ready","port":39001,"app_base_url":"http://127.0.0.1:39001","source":{"type":"stream","url":"http://127.0.0.1:39001/stream","format":"mpegts","video_codec":"h264","audio_codec":"aac"},"started_at":"2026-01-02T03:04:05Z"}`

func TestClientLifecycle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/apps", func(w http.ResponseWriter, r *http.Request) {
		var request CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode create: %v", err)
			return
		}
		if request.AppID != "app_1" || request.ArtifactURL != "http://gateway/artifact.tar.gz" || request.Port != 39000 {
			t.Errorf("request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, instanceJSON)
	})
	mux.HandleFunc("GET /v1/apps/inst_1", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, instanceJSON)
	})
	mux.HandleFunc("DELETE /v1/apps/inst_1", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"app_id":"app_1","instance_id":"inst_1","state":"stopped","port":39001}`)
	})
	mux.HandleFunc("GET /v1/ports", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ports":[{"app_id":"app_1","port":39000,"occupied":true,"owner":"app_1"},{"port":39001,"occupied":false}]}`)
	})
	mux.HandleFunc("GET /v1/ports/{port}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("port") != "39002" {
			t.Errorf("port path = %q", r.PathValue("port"))
		}
		io.WriteString(w, `{"port":39002,"occupied":false,"owner":""}`)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	client := New(server.URL, 2*time.Second)

	app, err := client.Create(ctx, CreateRequest{AppID: "app_1", ArtifactURL: "http://gateway/artifact.tar.gz", Port: 39000})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if app.InstanceID != "inst_1" || app.State != StateReady || app.Source.Format != "mpegts" || app.Source.VideoCodec != "h264" {
		t.Fatalf("app = %+v", app)
	}
	if app.StartedAt.IsZero() {
		t.Fatal("started_at was not parsed")
	}
	if _, err := client.Get(ctx, "inst_1"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	stopped, err := client.Delete(ctx, "inst_1")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if stopped.State != StateStopped {
		t.Fatalf("stopped = %+v", stopped)
	}
	ports, err := client.ListPorts(ctx)
	if err != nil {
		t.Fatalf("ListPorts: %v", err)
	}
	if len(ports) != 2 || !ports[0].Occupied || ports[1].Occupied {
		t.Fatalf("ports = %+v", ports)
	}
	info, err := client.GetPort(ctx, 39002)
	if err != nil {
		t.Fatalf("GetPort: %v", err)
	}
	if info.Port != 39002 || info.Occupied {
		t.Fatalf("port = %+v", info)
	}
	if err := client.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestClientErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/apps", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `{"error":{"stage":"starting","code":"PORT_IN_USE","message":"port busy"}}`)
	})
	mux.HandleFunc("GET /v1/apps/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "not found")
	})
	mux.HandleFunc("GET /v1/ports", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"code":"INTERNAL","message":"boom"}}`)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(server.URL, time.Second)
	_, err := client.Create(context.Background(), CreateRequest{})
	if !IsStatus(err, http.StatusConflict) || !IsCode(err, "PORT_IN_USE") {
		t.Fatalf("create error = %#v", err)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Message != "port busy" {
		t.Fatalf("http error = %+v", httpErr)
	}
	if _, err := client.Get(context.Background(), "missing"); !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("get error = %#v", err)
	}
	if _, err := client.ListPorts(context.Background()); !IsCode(err, "INTERNAL") {
		t.Fatalf("ports error = %#v", err)
	}
	if err := client.Health(context.Background()); !IsStatus(err, http.StatusServiceUnavailable) {
		t.Fatalf("health error = %#v", err)
	}
}
