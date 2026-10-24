package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/mocktv"
)

type fakeAddr struct{ value string }

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return a.value }

type fakeCloser struct {
	mu     sync.Mutex
	closed int
}

func (c *fakeCloser) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	return nil
}

func (c *fakeCloser) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// freeAddr reserves and releases a loopback port so the command can bind it.
func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

// noKeepAliveClient avoids lingering idle connections that make the command's
// five-second Shutdown wait for them.
func noKeepAliveClient() *http.Client {
	return &http.Client{
		Timeout:   time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

func waitForHealth(t *testing.T, address string) {
	t.Helper()
	client := noKeepAliveClient()
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + address + "/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
			last = err
		} else {
			last = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("mock-tv did not become ready: %v", last)
}

func TestRunArgumentValidation(t *testing.T) {
	ctx := context.Background()
	for _, args := range [][]string{
		{"-unknown"},
		{"positional"},
		{"-addr", "not-an-address"},
		{"-addr", "127.0.0.1:0", "-public-url", "ftp://example"},
		{"-addr", "127.0.0.1:0", "-public-url", "http://example/path"},
		{"-addr", "127.0.0.1:0", "-iface", "no-such-interface-xyz"},
	} {
		if err := runContext(ctx, args); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}

func TestRunHelp(t *testing.T) {
	if err := runContext(context.Background(), []string{"-h"}); err != nil {
		t.Fatalf("-h returned error: %v", err)
	}
}

func TestRunListenFailureWhenPortTaken(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := runContext(context.Background(), []string{"-addr", listener.Addr().String()}); err == nil {
		t.Fatal("expected listen failure on an occupied port")
	}
}

func TestRunFailsWhenListenerDies(t *testing.T) {
	original := listenTCP
	t.Cleanup(func() { listenTCP = original })

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close() // Serve will observe a closed listener immediately.
	listenTCP = func(network, addr string) (net.Listener, error) {
		dead, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		_ = dead.Close()
		return dead, nil
	}
	err = runContext(context.Background(), []string{"-addr", address, "-no-ssdp"})
	if err == nil {
		t.Fatal("expected serve failure for a closed listener")
	}
}

func TestDefaultListenSSDPValidatesConfig(t *testing.T) {
	// Invalid config returns before any multicast socket is opened.
	if _, err := defaultListenSSDP(mocktv.SSDPConfig{}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRunSSDPListenFailure(t *testing.T) {
	original := listenSSDP
	t.Cleanup(func() { listenSSDP = original })
	listenSSDP = func(mocktv.SSDPConfig) (io.Closer, error) {
		return nil, net.ErrClosed
	}
	err := runContext(context.Background(), []string{"-addr", "127.0.0.1:0"})
	if err == nil || !strings.Contains(err.Error(), "try -iface en0 or -no-ssdp") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunSSDPConfigAndShutdown(t *testing.T) {
	original := listenSSDP
	t.Cleanup(func() { listenSSDP = original })

	responder := &fakeCloser{}
	var received mocktv.SSDPConfig
	listenSSDP = func(cfg mocktv.SSDPConfig) (io.Closer, error) {
		received = cfg
		return responder, nil
	}
	address := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runContext(ctx, []string{
			"-addr", address,
			"-public-url", "http://" + address,
			"-name", "Edge TV",
			"-udn", "uuid:edge-tv",
			"-max-age", "90s",
		})
	}()
	waitForHealth(t, address)
	if received.Location != "http://"+address+"/device.xml" {
		t.Fatalf("SSDP location = %q", received.Location)
	}
	if received.UDN != "uuid:edge-tv" {
		t.Fatalf("SSDP UDN = %q", received.UDN)
	}
	if received.MaxAge != 90*time.Second {
		t.Fatalf("SSDP max-age = %v", received.MaxAge)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("mock-tv did not shut down")
	}
	if responder.count() != 1 {
		t.Fatalf("responder Close calls = %d", responder.count())
	}
}

func TestRunNoSSDPAndCanceledContext(t *testing.T) {
	address := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runContext(ctx, []string{"-addr", address, "-no-ssdp"})
	}()
	waitForHealth(t, address)

	response, err := noKeepAliveClient().Post("http://"+address+"/api/stop", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stop status = %d", response.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("mock-tv did not shut down")
	}

	// The port must be released for reuse.
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("listener retained: %v", err)
	}
	_ = listener.Close()
}

func TestRunSignalContextCanceled(t *testing.T) {
	original := notifyContext
	t.Cleanup(func() { notifyContext = original })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	notifyContext = func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) {
		return ctx, func() {}
	}
	if err := run([]string{"-addr", "127.0.0.1:0", "-no-ssdp"}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunCanceledBeforeServing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Port 0 lets the kernel choose so the cancelled process never races
	// another test for a fixed port.
	if err := runContext(ctx, []string{"-addr", "127.0.0.1:0", "-no-ssdp"}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestPublicBaseNonTCPAddress(t *testing.T) {
	if _, err := publicBase("", fakeAddr{"127.0.0.1:8095"}); err == nil {
		t.Fatal("expected error for non-TCP address")
	}
	if _, err := publicBase("", fakeAddr{"without-port"}); err == nil {
		t.Fatal("expected error for address without port")
	}
	if got, err := publicBase("http://192.168.1.4:8095/", &net.TCPAddr{IP: net.IPv4zero, Port: 8095}); err != nil || got != "http://192.168.1.4:8095" {
		t.Fatalf("publicBase with trailing slash = %q, %v", got, err)
	}
}
