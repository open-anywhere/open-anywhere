package endpoints

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

// maxCachedClients bounds the cache. When it is exceeded the map is rebuilt
// rather than evicted entry by entry; clients are cheap to recreate.
const maxCachedClients = 128

// Pool builds and caches Render clients per address and token.
// Tokens only participate in the cache key hash and never appear in logs.
type Pool struct {
	mu            sync.Mutex
	guard         *netguard.Guard
	httpTimeout   time.Duration
	renderTimeout time.Duration
	clients       map[string]any
}

// NewPool returns a client pool. renderTimeout is the longer budget a start
// request needs while Render downloads and starts the application.
func NewPool(guard *netguard.Guard, httpTimeout, renderTimeout time.Duration) *Pool {
	if httpTimeout <= 0 {
		httpTimeout = 30 * time.Second
	}
	if renderTimeout <= 0 {
		renderTimeout = httpTimeout
	}
	return &Pool{
		guard:         guard,
		httpTimeout:   httpTimeout,
		renderTimeout: renderTimeout,
		clients:       make(map[string]any),
	}
}

// Render implements service.Clients.
func (p *Pool) Render(binding service.Binding) service.RenderClient {
	value := p.cached("render", binding.RenderURL, binding.RenderToken, func(client *http.Client) any {
		return render.New(binding.RenderURL, p.renderTimeout, render.WithToken(binding.RenderToken), render.WithHTTPClient(client))
	})
	client, _ := value.(service.RenderClient)
	return client
}

func (p *Pool) cached(kind, baseURL, token string, build func(*http.Client) any) any {
	digest := sha256.Sum256([]byte(kind + "\x00" + baseURL + "\x00" + token))
	key := kind + ":" + hex.EncodeToString(digest[:])
	p.mu.Lock()
	defer p.mu.Unlock()
	if value, ok := p.clients[key]; ok {
		return value
	}
	if len(p.clients) >= maxCachedClients {
		p.clients = make(map[string]any)
	}
	value := build(p.httpClient(kind))
	p.clients[key] = value
	return value
}

func (p *Pool) httpClient(kind string) *http.Client {
	timeout := p.httpTimeout
	if kind == "render" {
		timeout = p.renderTimeout
	}
	if p.guard != nil {
		return p.guard.Client(timeout)
	}
	return &http.Client{Timeout: timeout}
}
