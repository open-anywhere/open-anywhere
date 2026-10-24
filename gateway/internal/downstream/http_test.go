package downstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

func TestClientsAndRejectedOperations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing downstream auth")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/ports/39100":
			fmt.Fprint(w, `{"occupied":false}`)
		case "/v1/apps":
			w.WriteHeader(409)
			fmt.Fprint(w, `{"app_id":"x","instance_id":"existing","error":{"code":"APP_ALREADY_RUNNING","message":"occupied"}}`)
		case "/v1/apps/x":
			fmt.Fprint(w, `{"instance_id":"x","state":"stopped"}`)
		case "/v1/targets":
			fmt.Fprint(w, `{"targets":[{"id":"tv","state":"online"}]}`)
		case "/v1/projection-sessions":
			fmt.Fprint(w, `{"session_id":"cast","state":"playing"}`)
		case "/v1/projection-sessions/cast":
			fmt.Fprint(w, `{"session_id":"cast","state":"stopped"}`)
		case "/redirect":
			w.Header().Set("Location", "/secret")
			w.WriteHeader(302)
		case "/large":
			fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "test")
	if err != nil {
		t.Fatal(err)
	}
	render := Render{Client: client}
	display := Casting{Client: client}
	ctx := context.Background()
	if free, err := render.PortFree(ctx, 39100); err != nil || !free {
		t.Fatal(free, err)
	}
	_, err = render.Start(ctx, "x", "http://artifact/x", 39100)
	var rejected *application.Rejected
	if !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	for _, fn := range []func(context.Context, string) (application.Instance, error){render.Get, render.Stop} {
		if out, err := fn(ctx, "x"); err != nil || out.ID != "x" {
			t.Fatal(out, err)
		}
	}
	if out, err := display.Targets(ctx); err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	if out, err := display.Start(ctx, casting.CreateSessionRequest{}); err != nil || out.ID != "cast" {
		t.Fatal(out, err)
	}
	for _, fn := range []func(context.Context, string) (casting.Session, error){display.Get, display.Stop} {
		if out, err := fn(ctx, "cast"); err != nil || out.State != casting.SessionStopped {
			t.Fatal(out, err)
		}
	}
	for _, path := range []string{"/redirect", "/large"} {
		if err := client.Request(ctx, "GET", path, nil, nil); err == nil {
			t.Fatal("accepted", path)
		}
	}
}
