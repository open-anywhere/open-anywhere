package render

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func errorResponse(w http.ResponseWriter, e *Failure) {
	jsonResponse(w, e.HTTP, struct {
		AppID      string    `json:"app_id,omitempty"`
		InstanceID string    `json:"instance_id,omitempty"`
		Error      api.Error `json:"error"`
	}{e.AppID, e.InstanceID, e.Detail})
}
func NewHandler(s *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /v1/apps", func(w http.ResponseWriter, r *http.Request) {
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
			errorResponse(w, failure(415, "INVALID_REQUEST", "Content-Type must be application/json"))
			return
		}
		var req api.CreateRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || dec.Decode(new(any)) != io.EOF {
			errorResponse(w, failure(400, "INVALID_REQUEST", "invalid request JSON"))
			return
		}
		v, err := s.Create(r.Context(), req)
		if err != nil {
			errorResponse(w, err)
			return
		}
		w.Header().Set("Location", "/v1/apps/"+v.InstanceID)
		jsonResponse(w, 201, v)
	})
	mux.HandleFunc("GET /v1/apps/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, ok := s.Get(r.PathValue("id"))
		if !ok {
			errorResponse(w, failure(404, "INSTANCE_NOT_FOUND", "unknown instance"))
			return
		}
		jsonResponse(w, 200, v)
	})
	mux.HandleFunc("DELETE /v1/apps/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		v, err := s.Stop(ctx, r.PathValue("id"))
		if err != nil {
			errorResponse(w, err)
			return
		}
		jsonResponse(w, 200, v)
	})
	mux.HandleFunc("GET /v1/ports/{port}", func(w http.ResponseWriter, r *http.Request) {
		port, err := strconv.Atoi(r.PathValue("port"))
		if err != nil || port < 1 || port > 65535 {
			errorResponse(w, &Failure{HTTP: 400, Detail: api.Error{Code: "INVALID_REQUEST", Message: "port must be an integer from 1 to 65535"}})
			return
		}
		v, err := s.Port(port)
		if err != nil {
			errorResponse(w, &Failure{HTTP: 503, Detail: api.Error{Code: "PORT_INSPECTION_FAILED", Message: err.Error()}})
			return
		}
		jsonResponse(w, 200, v)
	})
	mux.HandleFunc("GET /v1/ports", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		v, err := s.Ports(ctx)
		if err != nil {
			errorResponse(w, &Failure{HTTP: 503, Detail: api.Error{Code: "PORT_INSPECTION_FAILED", Message: err.Error()}})
			return
		}
		jsonResponse(w, 200, map[string]any{"ports": v})
	})
	mux.HandleFunc("GET /streams/{id}/live.ts", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		instance, ok := s.instances[r.PathValue("id")]
		if !ok || instance.info.State != "ready" {
			s.mu.Unlock()
			http.Error(w, "stream unavailable", http.StatusNotFound)
			return
		}
		ch, unsubscribe := instance.hub.subscribe()
		s.mu.Unlock()
		defer unsubscribe()
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		rc := http.NewResponseController(w)
		for {
			select {
			case <-r.Context().Done():
				return
			case b, ok := <-ch:
				if !ok {
					return
				}
				_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := w.Write(b); err != nil {
					return
				}
				if rc.Flush() != nil {
					return
				}
			}
		}
	})
	return mux
}
