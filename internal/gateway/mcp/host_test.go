package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestHostsFromURL(t *testing.T) {
	tests := []struct {
		raw  string
		want []string
	}{
		{"http://203.0.113.10:6004/mcp", []string{"203.0.113.10:6004"}},
		{"https://gateway.example.com/mcp", []string{"gateway.example.com"}},
		{"https://gateway.example.com:443/mcp", []string{"gateway.example.com:443", "gateway.example.com"}},
		{"http://gateway.example.com:80/mcp", []string{"gateway.example.com:80", "gateway.example.com"}},
		{"  http://gw.example:8090/mcp  ", []string{"gw.example:8090"}},
		{"", nil},
		{"not a url", nil},
		{"http://[::1/mcp", nil},
	}
	for _, test := range tests {
		if got := HostsFromURL(test.raw); !reflect.DeepEqual(got, test.want) {
			t.Errorf("HostsFromURL(%q) = %v, want %v", test.raw, got, test.want)
		}
	}
}

// hostStatus sends a request without identity headers. A request that passes
// the Host check reaches the SDK and is answered 400; a refused one is 403.
func hostStatus(handler http.Handler, host string, localAddr net.Addr) int {
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	request.Host = host
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if localAddr != nil {
		request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, localAddr))
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func TestStreamableHTTPHostsFollowTheCurrentSetting(t *testing.T) {
	var (
		mu      sync.Mutex
		allowed []string
	)
	handler := New(&fakeApps{}, 0).StreamableHTTPHosts(func(context.Context) []string {
		mu.Lock()
		defer mu.Unlock()
		return allowed
	}, false)
	loopback := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8090}

	if got := hostStatus(handler, "gw.example:6004", loopback); got != http.StatusForbidden {
		t.Fatalf("before the address is set: status = %d", got)
	}
	mu.Lock()
	allowed = []string{"gw.example:6004"}
	mu.Unlock()
	if got := hostStatus(handler, "gw.example:6004", loopback); got == http.StatusForbidden {
		t.Fatalf("after the address is set: status = %d", got)
	}
	if got := hostStatus(handler, "GW.example:6004", loopback); got == http.StatusForbidden {
		t.Fatalf("host names are not case sensitive: status = %d", got)
	}
	// Once Hosts are configured, any other Host is refused even without a loopback connection.
	if got := hostStatus(handler, "attacker.example", nil); got != http.StatusForbidden {
		t.Fatalf("another host: status = %d", got)
	}
	for _, host := range []string{"127.0.0.1:8090", "localhost:8090", "[::1]:8090"} {
		if got := hostStatus(handler, host, loopback); got == http.StatusForbidden {
			t.Fatalf("loopback host %s: status = %d", host, got)
		}
	}
	mu.Lock()
	allowed = nil
	mu.Unlock()
	if got := hostStatus(handler, "gw.example:6004", loopback); got != http.StatusForbidden {
		t.Fatalf("after the address is cleared: status = %d", got)
	}
}

func TestStreamableHTTPWithoutConfiguredHostsGuardsLoopbackConnections(t *testing.T) {
	handler := New(&fakeApps{}, 0).StreamableHTTP("", false)
	loopback := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8090}
	lan := &net.TCPAddr{IP: net.ParseIP("192.168.1.5"), Port: 8090}

	// Through a local tunnel the connection is loopback but the Host is public.
	if got := hostStatus(handler, "gw.example:6004", loopback); got != http.StatusForbidden {
		t.Fatalf("public host over loopback: status = %d", got)
	}
	// Exposed directly on a network interface, the Host is not restricted.
	if got := hostStatus(handler, "gw.example:6004", lan); got == http.StatusForbidden {
		t.Fatalf("public host over a network interface: status = %d", got)
	}
	if got := hostStatus(handler, "gw.example:6004", nil); got == http.StatusForbidden {
		t.Fatalf("unknown connection address: status = %d", got)
	}
}

func TestRefusedHostsAreLoggedOncePerMinuteWithoutGrowingWithoutBound(t *testing.T) {
	var (
		mu    sync.Mutex
		lines []string
	)
	server := New(&fakeApps{}, 0)
	server.logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	handler := server.StreamableHTTP("", false)
	loopback := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8090}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(lines)
	}

	for i := 0; i < 5; i++ {
		hostStatus(handler, "gw.example:6004", loopback)
	}
	if count() != 1 {
		t.Fatalf("one host refused five times logged %d lines: %v", count(), lines)
	}
	if !strings.Contains(lines[0], `"gw.example:6004"`) || !strings.Contains(lines[0], "public_host") {
		t.Fatalf("log line does not name the host and the fix: %s", lines[0])
	}
	hostStatus(handler, "other.example", loopback)
	if count() != 2 {
		t.Fatalf("a second host should be logged, got %d lines", count())
	}
	// Allowed requests are not logged.
	hostStatus(handler, "127.0.0.1:8090", loopback)
	if count() != 2 {
		t.Fatalf("an allowed host was logged: %v", lines)
	}

	// The table stays small even when every request brings a new Host.
	var refusals hostRefusals
	for i := 0; i < 1000; i++ {
		refusals.note(func(string, ...any) {}, fmt.Sprintf("h%d.example", i))
	}
	if len(refusals.seen) > hostRefusalMaxHosts {
		t.Fatalf("table grew to %d entries", len(refusals.seen))
	}
	long := strings.Repeat("a", 1000)
	var logged string
	refusals.note(func(format string, args ...any) { logged = fmt.Sprintf(format, args...) }, long)
	if len(logged) > 400 {
		t.Fatalf("an oversized Host was logged in full: %d bytes", len(logged))
	}
}
