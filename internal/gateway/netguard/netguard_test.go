package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateURLAcceptsHTTPAddresses(t *testing.T) {
	guard := New(false)
	for _, raw := range []string{
		"https://example.com",
		"http://example.com:8080/base?x=1",
		"http://[2001:4860:4860::8888]:443/mcp",
	} {
		if err := guard.ValidateURL(raw); err != nil {
			t.Errorf("ValidateURL(%q) = %v", raw, err)
		}
	}
}

func TestValidateURLRejectsUnsafeShapes(t *testing.T) {
	guard := New(false)
	for _, raw := range []string{
		"",
		"   ",
		"ftp://example.com",
		"file:///etc/passwd",
		"https://user:pass@example.com",
		"https://example.com/#fragment",
		"https:///missing-host",
		"http://example.com:0",
		"http://example.com:70000",
		"http://exa mple.com",
	} {
		err := guard.ValidateURL(raw)
		if CodeOf(err) != CodeInvalidURL {
			t.Errorf("ValidateURL(%q) = %v, want %s", raw, err, CodeInvalidURL)
		}
	}
}

func TestCheckURLRejectsNonPublicAddresses(t *testing.T) {
	guard := New(false)
	ctx := context.Background()
	for _, raw := range []string{
		"http://127.0.0.1",
		"https://127.1.2.3:8080",
		"http://10.0.0.5",
		"http://172.16.0.1",
		"http://192.168.1.1",
		"http://169.254.10.10",
		"http://100.64.0.1",
		"http://100.127.255.254",
		"http://192.0.0.8",
		"http://0.0.0.0",
		"http://224.0.0.1",
		"http://[::1]",
		"http://[::]",
		"http://[fc00::1]",
		"http://[fdff::1]",
		"http://[fe80::1]",
		"http://[ff02::1]",
	} {
		err := guard.CheckURL(ctx, raw)
		if CodeOf(err) != CodePrivateTarget {
			t.Errorf("CheckURL(%q) = %v, want %s", raw, err, CodePrivateTarget)
		}
	}
}

func TestCheckURLAcceptsPublicAddresses(t *testing.T) {
	guard := New(false)
	ctx := context.Background()
	for _, raw := range []string{
		"http://8.8.8.8",
		"https://1.1.1.1:8443",
		"http://[2001:4860:4860::8888]",
	} {
		if err := guard.CheckURL(ctx, raw); err != nil {
			t.Errorf("CheckURL(%q) = %v", raw, err)
		}
	}
}

func TestCheckURLHonoursAllowPrivate(t *testing.T) {
	guard := New(true)
	if !guard.AllowPrivate() {
		t.Fatal("AllowPrivate() = false")
	}
	if err := guard.CheckURL(context.Background(), "http://127.0.0.1:8080"); err != nil {
		t.Fatalf("CheckURL with allowPrivate: %v", err)
	}
}

func TestCheckURLUsesResolvedAddresses(t *testing.T) {
	ctx := context.Background()
	guard := New(false)
	guard.lookupIP = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("10.1.2.3")}, nil
	}
	if err := guard.CheckURL(ctx, "http://mixed.example"); CodeOf(err) != CodePrivateTarget {
		t.Fatalf("mixed answer = %v, want %s", err, CodePrivateTarget)
	}

	guard.lookupIP = func(context.Context, string) ([]net.IP, error) {
		return nil, errors.New("dns down")
	}
	if err := guard.CheckURL(ctx, "http://broken.example"); CodeOf(err) != CodeTargetUnreach {
		t.Fatalf("lookup error = %v, want %s", err, CodeTargetUnreach)
	}

	guard.lookupIP = func(context.Context, string) ([]net.IP, error) {
		return nil, nil
	}
	if err := guard.CheckURL(ctx, "http://empty.example"); CodeOf(err) != CodeTargetUnreach {
		t.Fatalf("empty answer = %v, want %s", err, CodeTargetUnreach)
	}
}

func TestDialContextRechecksTheAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().String()

	guard := New(false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := guard.DialContext(ctx, "tcp", address); CodeOf(err) != CodePrivateTarget {
		t.Fatalf("dial private address = %v, want %s", err, CodePrivateTarget)
	}

	allowed := New(true)
	conn, err := allowed.DialContext(ctx, "tcp", address)
	if err != nil {
		t.Fatalf("dial with allowPrivate: %v", err)
	}
	conn.Close()

	if _, err := allowed.DialContext(ctx, "tcp", "not-an-address"); CodeOf(err) != CodeInvalidURL {
		t.Fatalf("dial malformed address = %v, want %s", err, CodeInvalidURL)
	}
}

func TestDialContextRefusesAReboundHostname(t *testing.T) {
	calls := 0
	guard := New(false)
	guard.lookupIP = func(context.Context, string) ([]net.IP, error) {
		calls++
		if calls == 1 {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		return []net.IP{net.ParseIP("10.0.0.9")}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := guard.CheckURL(ctx, "http://rebind.example:8080"); err != nil {
		t.Fatalf("first check: %v", err)
	}
	if _, err := guard.DialContext(ctx, "tcp", "rebind.example:8080"); CodeOf(err) != CodePrivateTarget {
		t.Fatalf("rebound dial = %v, want %s", err, CodePrivateTarget)
	}
}

func TestClientBlocksLocalTargetsAndAllowsThemWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	if _, err := New(false).Client(2 * time.Second).Get(server.URL); CodeOf(err) != CodePrivateTarget {
		t.Fatalf("guarded GET = %v, want %s", err, CodePrivateTarget)
	}
	response, err := New(true).Client(2 * time.Second).Get(server.URL)
	if err != nil {
		t.Fatalf("allowed GET: %v", err)
	}
	response.Body.Close()
}

func TestClientRejectsPrivateRedirects(t *testing.T) {
	guard := New(false)
	client := guard.Client(time.Second)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/next", nil)
	err := client.CheckRedirect(request, nil)
	if CodeOf(err) != CodePrivateTarget {
		t.Fatalf("redirect = %v, want %s", err, CodePrivateTarget)
	}
	if err := New(true).Client(time.Second).CheckRedirect(request, nil); err != nil {
		t.Fatalf("redirect with allowPrivate: %v", err)
	}
}

func TestClientLimitsRedirects(t *testing.T) {
	client := New(true).Client(time.Second)
	request := httptest.NewRequest(http.MethodGet, "http://example.com/next", nil)
	err := client.CheckRedirect(request, make([]*http.Request, maxRedirects))
	if err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("redirect limit = %v", err)
	}
}

func TestTransportDisablesEnvironmentProxy(t *testing.T) {
	transport := New(false).Transport()
	if transport.Proxy != nil {
		t.Fatal("the guarded transport must not use environment proxies")
	}
	if transport.DialContext == nil {
		t.Fatal("the guarded transport has no dial policy")
	}
}

func TestCodeOfUnwrapsErrors(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", &Error{Code: CodeInvalidURL, Message: "bad"})
	if got := CodeOf(wrapped); got != CodeInvalidURL {
		t.Fatalf("CodeOf = %q", got)
	}
	if got := CodeOf(errors.New("plain")); got != "" {
		t.Fatalf("CodeOf(plain) = %q", got)
	}
}

func TestCheckURLTrimsWhitespace(t *testing.T) {
	ctx := context.Background()
	guard := New(false)
	if err := guard.CheckURL(ctx, "  http://8.8.8.8 \n"); err != nil {
		t.Fatalf("padded public address = %v", err)
	}
	if err := guard.CheckURL(ctx, "\thttp://10.1.2.3  "); CodeOf(err) != CodePrivateTarget {
		t.Fatalf("padded private address = %v, want %s", err, CodePrivateTarget)
	}
}

func TestDialContextRejectsIPv4MappedLoopback(t *testing.T) {
	guard := New(false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := guard.DialContext(ctx, "tcp", "[::ffff:127.0.0.1]:80"); CodeOf(err) != CodePrivateTarget {
		t.Fatalf("mapped loopback dial = %v, want %s", err, CodePrivateTarget)
	}
}

func TestDialContextReportsAnUnreachableAllowedAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// allowPrivate lets the dial through the policy, so a closed port must be
	// reported as unreachable rather than forbidden.
	if _, err := New(true).DialContext(ctx, "tcp", address); CodeOf(err) != CodeTargetUnreach {
		t.Fatalf("closed port dial = %v, want %s", err, CodeTargetUnreach)
	}
}
