package endpoints

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

var _ service.Clients = (*Pool)(nil)

func TestPoolCachesClientsByAddressAndToken(t *testing.T) {
	pool := NewPool(nil, time.Second, 2*time.Second)
	first := pool.Render(service.Binding{RenderURL: "https://render.example", RenderToken: "one"})
	again := pool.Render(service.Binding{RenderURL: "https://render.example", RenderToken: "one"})
	if first == nil || first != again {
		t.Fatal("the same address and token should reuse a client")
	}
	other := pool.Render(service.Binding{RenderURL: "https://render.example", RenderToken: "two"})
	if other == first {
		t.Fatal("a different token must not reuse the client")
	}
	otherURL := pool.Render(service.Binding{RenderURL: "https://render-2.example", RenderToken: "one"})
	if otherURL == first {
		t.Fatal("a different address must not reuse the client")
	}

}

func TestPoolKeysDoNotContainSecrets(t *testing.T) {
	pool := NewPool(nil, time.Second, time.Second)
	pool.Render(service.Binding{RenderURL: "https://render.example", RenderToken: "very-secret-token"})
	for key := range pool.clients {
		if strings.Contains(key, "very-secret-token") || strings.Contains(key, "render.example") {
			t.Fatalf("cache key exposes connection details: %q", key)
		}
	}
}

func TestPoolRebuildsWhenItReachesTheLimit(t *testing.T) {
	pool := NewPool(nil, time.Second, time.Second)
	for index := 0; index < maxCachedClients+2; index++ {
		pool.Render(service.Binding{RenderURL: fmt.Sprintf("https://render-%d.example", index)})
	}
	if len(pool.clients) > maxCachedClients {
		t.Fatalf("cache size = %d", len(pool.clients))
	}
	if len(pool.clients) == 0 {
		t.Fatal("the cache was not rebuilt")
	}
}
