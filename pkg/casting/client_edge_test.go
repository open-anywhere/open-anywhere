package casting

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

func TestNewClientDefaultsAndErrorFormat(t *testing.T) {
	client := NewClient("http://casting.invalid/", 0)
	if client.http.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v", client.http.Timeout)
	}
	if client.baseURL != "http://casting.invalid" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
	if NewClient("http://casting.invalid", -time.Second).http.Timeout != 30*time.Second {
		t.Fatal("negative timeout should take the default")
	}
	err := &ClientError{Status: 409, Code: "TARGET_BUSY", Message: "busy"}
	if got, want := err.Error(), "casting: 409 TARGET_BUSY: busy"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !IsStatus(err, 409) || IsStatus(errors.New("other"), 409) {
		t.Fatal("IsStatus")
	}
}

func TestDoRejectsUnmarshalableBodyAndBadURL(t *testing.T) {
	client := NewClient("http://casting.invalid", time.Second)
	if err := client.do(context.Background(), http.MethodPost, "/v1/projection-sessions", make(chan int), nil); err == nil {
		t.Fatal("unmarshalable body should fail before sending")
	}
	bad := NewClient("http://casting.invalid\n", time.Second)
	if err := bad.do(context.Background(), http.MethodGet, "/healthz", nil, nil); err == nil {
		t.Fatal("control character in URL should fail")
	}
}

func TestDoTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	client := NewClient(url, 50*time.Millisecond)
	if _, err := client.ListTargets(context.Background()); err == nil {
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
		_, _ = io.WriteString(w, `{"id":"tv","state":"online"}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	if _, err := client.GetTarget(context.Background(), "../../admin"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(escaped, "/v1/targets/") || strings.Contains(escaped, "/admin") {
		t.Fatalf("escaped path = %q", escaped)
	}
	if !strings.Contains(escaped, "%2F") {
		t.Fatalf("slashes should be escaped: %q", escaped)
	}
}

func TestNon2xxWithoutBodyUsesStatusText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := NewClient(server.URL, time.Second).GetSession(context.Background(), "ps_1")
	var clientErr *ClientError
	if !errors.As(err, &clientErr) || clientErr.Status != http.StatusBadGateway || clientErr.Message != "502 Bad Gateway" {
		t.Fatalf("err = %#v", err)
	}
}
