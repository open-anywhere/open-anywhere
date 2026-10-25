package render

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientOptionsInstallTokenAndHTTPClient(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ports":[{"port":39001,"occupied":true}]}`)
	}))
	defer server.Close()

	custom := &http.Client{Timeout: time.Second}
	client := New(server.URL+"/", 0,
		WithToken("render-token"),
		WithHTTPClient(nil),
		WithHTTPClient(custom),
	)
	if client.token != "render-token" {
		t.Fatalf("token = %q", client.token)
	}
	if client.http != custom {
		t.Fatal("WithHTTPClient did not install the custom client")
	}
	ports, err := client.ListPorts(context.Background())
	if err != nil {
		t.Fatalf("ListPorts: %v", err)
	}
	if len(ports) != 1 || ports[0].Port != 39001 || !ports[0].Occupied {
		t.Fatalf("ports = %+v", ports)
	}
	if authorization != "Bearer render-token" {
		t.Fatalf("Authorization = %q", authorization)
	}
}
