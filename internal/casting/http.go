package casting

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

func NewHandler(s *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /v1/targets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"targets": s.ListTargets()})
	})
	mux.HandleFunc("GET /v1/targets/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.GetTarget(r.PathValue("id"))
		respond(w, 200, v, err)
	})
	mux.HandleFunc("POST /v1/projection-sessions", func(w http.ResponseWriter, r *http.Request) {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			writeError(w, 415, "unsupported_content_type", "Content-Type must be application/json", nil)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var req api.CreateSessionRequest
		err = dec.Decode(&req)
		if err == nil {
			var extra any
			if nextErr := dec.Decode(&extra); nextErr != io.EOF {
				if nextErr != nil {
					err = nextErr
				} else {
					err = ErrInvalid
				}
			}
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, 413, "body_too_large", "request body exceeds 64 KiB", nil)
			} else {
				writeError(w, 400, "invalid_request", "expected one JSON object with known fields", nil)
			}
			return
		}
		v, err := s.CreateSession(r.Context(), req)
		if err != nil {
			writeServiceError(w, err, v.ID)
			return
		}
		w.Header().Set("Location", "/v1/projection-sessions/"+v.ID)
		writeJSON(w, 201, v)
	})
	mux.HandleFunc("GET /v1/projection-sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.GetSession(r.Context(), r.PathValue("id"))
		respond(w, 200, v, err)
	})
	mux.HandleFunc("DELETE /v1/projection-sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.StopSession(r.Context(), r.PathValue("id"))
		respond(w, 200, v, err)
	})
	return mux
}
func respond(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		writeServiceError(w, err, "")
		return
	}
	writeJSON(w, status, v)
}
func writeServiceError(w http.ResponseWriter, err error, id string) {
	status, code := 500, "internal_error"
	switch {
	case errors.Is(err, ErrInvalid):
		status, code = 400, "invalid_request"
	case errors.Is(err, ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, ErrUnsupported):
		status, code = 422, "unsupported_media"
	case errors.Is(err, ErrConflict):
		status, code = 409, "target_busy"
	case errors.Is(err, ErrOffline):
		status, code = 409, "target_offline"
	case errors.Is(err, ErrUpstream):
		status, code = 502, "device_control_failed"
	}
	var sessionID any
	if id != "" {
		sessionID = id
	}
	writeError(w, status, code, err.Error(), sessionID)
}
func writeError(w http.ResponseWriter, status int, code, message string, id any) {
	body := map[string]any{"code": code, "message": message}
	if id != nil {
		body["session_id"] = id
	}
	writeJSON(w, status, map[string]any{"error": body})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
