package openapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/httpapi"
)

func TestExecuteRoundsSubMillisecondTimeoutsUpToOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	spec, err := Parse([]byte(validSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	_, err = spec.Execute(context.Background(), server.URL, "rollDice", nil, nil, &http.Client{Timeout: 500 * time.Microsecond})
	var apiErr *httpapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "TIMEOUT" {
		t.Fatalf("err = %#v", err)
	}
}
