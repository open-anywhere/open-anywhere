package portal

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// APIError is a portal error with a stable code and a user-facing message.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

func apiError(status int, code, message string) *APIError {
	return &APIError{Status: status, Code: code, Message: message}
}

var (
	errUnauthorized = apiError(http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
	errForbidden    = apiError(http.StatusForbidden, "FORBIDDEN", "没有权限执行该操作")
	errNotFound     = apiError(http.StatusNotFound, "NOT_FOUND", "资源不存在")
)

func invalidArgument(message string) *APIError {
	return apiError(http.StatusBadRequest, "INVALID_ARGUMENT", message)
}

// asAPIError turns any error into an APIError, defaulting to 500 without
// leaking internal details.
func asAPIError(err error) *APIError {
	var target *APIError
	if errors.As(err, &target) {
		return target
	}
	return apiError(http.StatusInternalServerError, "INTERNAL", "服务暂时不可用")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	target := asAPIError(err)
	writeJSON(w, target.Status, map[string]any{
		"error": map[string]string{"code": target.Code, "message": target.Message},
	})
}

// formatHTTPError is used at call sites that only have a status and message.
func formatHTTPError(status int, code, format string, args ...any) *APIError {
	return apiError(status, code, fmt.Sprintf(format, args...))
}
