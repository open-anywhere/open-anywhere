package endpoints

import (
	"net/http"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

func TestNewPoolAppliesTimeoutDefaults(t *testing.T) {
	pool := NewPool(nil, 0, 0)
	if pool.httpTimeout != 30*time.Second || pool.renderTimeout != 30*time.Second {
		t.Fatalf("defaults = %v %v", pool.httpTimeout, pool.renderTimeout)
	}
	pool = NewPool(nil, 5*time.Second, 0)
	if pool.httpTimeout != 5*time.Second || pool.renderTimeout != 5*time.Second {
		t.Fatalf("render default = %v %v", pool.httpTimeout, pool.renderTimeout)
	}
	if client := pool.httpClient("render"); client.Timeout != 5*time.Second {
		t.Fatalf("render client timeout = %v", client.Timeout)
	}
}

func TestPoolBuildsGuardedClients(t *testing.T) {
	guard := netguard.New(true)
	pool := NewPool(guard, 3*time.Second, 7*time.Second)

	renderHTTP := pool.httpClient("render")
	if renderHTTP.Timeout != 7*time.Second {
		t.Fatalf("render HTTP timeout = %v", renderHTTP.Timeout)
	}
	castingHTTP := pool.httpClient("casting")
	if castingHTTP.Timeout != 3*time.Second {
		t.Fatalf("casting HTTP timeout = %v", castingHTTP.Timeout)
	}
	transport, ok := renderHTTP.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.DialContext == nil {
		t.Fatalf("render transport = %#v", renderHTTP.Transport)
	}

	if pool.Render(service.Binding{RenderURL: "https://render.example", RenderToken: "token"}) == nil {
		t.Fatal("guarded render client is nil")
	}
	if pool.Casting(service.Binding{CastingURL: "https://casting.example", CastingToken: "token"}) == nil {
		t.Fatal("guarded casting client is nil")
	}
}
