package render

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPErrorFormatting(t *testing.T) {
	err := &HTTPError{Status: 409, Code: "PORT_IN_USE", Message: "port busy"}
	if got, want := err.Error(), "render: 409 PORT_IN_USE: port busy"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !IsStatus(err, 409) || IsStatus(errors.New("other"), 409) {
		t.Fatal("IsStatus")
	}
	if !IsCode(err, "PORT_IN_USE") || IsCode(errors.New("other"), "PORT_IN_USE") {
		t.Fatal("IsCode")
	}
	if IsInvalidArtifact(err) {
		t.Fatal("a port error is not an artifact error")
	}
	if !IsInvalidArtifact(&HTTPError{Status: http.StatusUnprocessableEntity, Code: CodeInvalidArtifact}) {
		t.Fatal("422 INVALID_ARTIFACT was not recognized")
	}
	if !IsInvalidArtifact(&HTTPError{Status: http.StatusUnprocessableEntity}) {
		t.Fatal("a 422 without a code was not recognized")
	}
	if IsInvalidArtifact(errors.New("other")) {
		t.Fatal("a plain error is not an artifact error")
	}
}

func TestNewDefaultsTimeout(t *testing.T) {
	client := New("http://render.invalid/", 0)
	if client.http.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v", client.http.Timeout)
	}
	if client.baseURL != "http://render.invalid" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
	if New("http://render.invalid", -time.Second).http.Timeout != 30*time.Second {
		t.Fatal("negative timeout should take the default")
	}
}

func TestDoRejectsUnmarshalableBodyAndBadURL(t *testing.T) {
	client := New("http://render.invalid", time.Second)
	if err := client.do(context.Background(), http.MethodPost, "/v1/apps", make(chan int), nil); err == nil {
		t.Fatal("unmarshalable body should fail before sending")
	}
	// A control character makes net/http refuse to build the request.
	bad := New("http://render.invalid\n", time.Second)
	if err := bad.do(context.Background(), http.MethodGet, "/healthz", nil, nil); err == nil {
		t.Fatal("control character in URL should fail")
	}
}

func TestDoTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	client := New(url, 50*time.Millisecond)
	if _, err := client.Get(context.Background(), "inst"); err == nil {
		t.Fatal("closed server should fail")
	}
	if err := client.Health(context.Background()); err == nil {
		t.Fatal("closed server should fail health")
	}
}

func TestPathEscapingDoesNotTraverse(t *testing.T) {
	var escaped string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escaped = r.URL.EscapedPath()
		_, _ = io.WriteString(w, `{"app_id":"app","instance_id":"inst","state":"ready"}`)
	}))
	defer server.Close()

	client := New(server.URL, time.Second)
	if _, err := client.Get(context.Background(), "../../etc/passwd"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(escaped, "/v1/apps/") || strings.Contains(escaped, "/etc/passwd") {
		t.Fatalf("escaped path = %q", escaped)
	}
	if !strings.Contains(escaped, "%2F") {
		t.Fatalf("slashes should be escaped: %q", escaped)
	}
}

func TestDoRejectsMalformedJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"app_id":`)
	}))
	defer server.Close()
	if _, err := New(server.URL, time.Second).ListPorts(context.Background()); err == nil {
		t.Fatal("truncated JSON should fail")
	}
}
