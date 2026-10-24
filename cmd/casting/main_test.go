package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/open-anywhere/open-anywhere/internal/casting"
	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

func TestStartupRejectsInvalidConfiguration(t *testing.T) {
	if err := run("127.0.0.1:0", "", 0, 0); err == nil || !strings.Contains(err.Error(), "scan-interval") {
		t.Fatalf("invalid scan interval accepted: %v", err)
	}
	if err := run("127.0.0.1:0", "casting-interface-does-not-exist", time.Second, 0); err == nil {
		t.Fatal("invalid interface accepted")
	}
}

func TestStartupUsesLocalInterfaceAndReportsListenFailure(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			t.Fatal(err)
		}
		for _, address := range addresses {
			ip, ok := address.(*net.IPNet)
			if !ok || ip.IP.To4() == nil {
				continue
			}
			// An invalid listen address fails before discovery starts. Only inspect
			// loopback interface metadata; no multicast or LAN traffic is sent.
			err := run("invalid listen address", iface.Name, time.Second, 0)
			var opErr *net.OpError
			if !errors.As(err, &opErr) {
				t.Fatalf("want listen failure after interface selection, got %v", err)
			}
			return
		}
	}
	t.Fatal("no local IPv4 loopback interface")
}
func TestStartupDefaultInterfaceReportsListenFailure(t *testing.T) {
	var opErr *net.OpError
	if err := run("invalid listen address", "", time.Second, 0); !errors.As(err, &opErr) {
		t.Fatalf("want listen error, got %v", err)
	}
}

type scannerFunc func(context.Context) ([]core.Device, error)

func (f scannerFunc) Scan(ctx context.Context) ([]core.Device, error) { return f(ctx) }

func TestCommandFlagsAndExitCodes(t *testing.T) {
	for _, tt := range []struct {
		name                string
		args                []string
		wantCode            int
		wantAddr, wantIface string
		wantInterval        time.Duration
		wantGrace           time.Duration
		startErr            error
		called              bool
	}{
		{name: "defaults", wantAddr: ":8080", wantInterval: 30 * time.Second, wantGrace: 30 * time.Second, called: true},
		{name: "custom", args: []string{"-addr", "127.0.0.1:0", "-interface", "test-nic", "-scan-interval", "2s", "-unreachable-grace", "5s"}, wantAddr: "127.0.0.1:0", wantIface: "test-nic", wantInterval: 2 * time.Second, wantGrace: 5 * time.Second, called: true},
		{name: "help", args: []string{"-h"}},
		{name: "invalid flag", args: []string{"-unknown"}, wantCode: 2},
		{name: "invalid duration", args: []string{"-scan-interval", "bad"}, wantCode: 2},
		{name: "invalid grace", args: []string{"-unreachable-grace", "bad"}, wantCode: 2},
		{name: "extra argument", args: []string{"extra"}, wantCode: 2},
		{name: "startup failure", wantAddr: ":8080", wantInterval: 30 * time.Second, wantGrace: 30 * time.Second, wantCode: 1, startErr: errors.New("listen failed"), called: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			called := false
			code := command(tt.args, &out, func(addr, iface string, interval, grace time.Duration) error {
				called = true
				if addr != tt.wantAddr || iface != tt.wantIface || interval != tt.wantInterval || grace != tt.wantGrace {
					t.Fatalf("unexpected settings: %q %q %v %v", addr, iface, interval, grace)
				}
				return tt.startErr
			})
			if code != tt.wantCode || called != tt.called {
				t.Fatalf("code=%d called=%v output=%s", code, called, &out)
			}
			if (code != 0 || tt.name == "help") && out.Len() == 0 {
				t.Fatal("missing CLI diagnostic")
			}
			if tt.startErr != nil && !strings.Contains(out.String(), tt.startErr.Error()) {
				t.Fatal(out.String())
			}
		})
	}
}

func TestRuntimeInterfaceErrors(t *testing.T) {
	cause := errors.New("interface access failed")
	for _, tt := range []struct {
		name                string
		addresses           []net.Addr
		lookupErr, addrsErr error
		want                error
	}{
		{name: "lookup", lookupErr: cause, want: cause},
		{name: "address lookup", addrsErr: cause, want: cause},
		{name: "IPv6 only", addresses: []net.Addr{&net.IPNet{IP: net.IPv6loopback}}},
		{name: "no addresses"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			deps := systemRuntime()
			deps.interfaceByName = func(string) (*net.Interface, error) { return &net.Interface{}, tt.lookupErr }
			deps.interfaceAddrs = func(*net.Interface) ([]net.Addr, error) { return tt.addresses, tt.addrsErr }
			err := runWith(context.Background(), "127.0.0.1:0", "test-nic", time.Second, 0, deps)
			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "no IPv4") {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimePublishesPartialDiscoveryResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := systemRuntime()
	deps.interfaceByName = func(name string) (*net.Interface, error) { return &net.Interface{Name: name}, nil }
	deps.interfaceAddrs = func(*net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.IPv6loopback}, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, &net.IPNet{IP: net.IPv4(127, 0, 0, 1)}}, nil
	}
	deps.newDiscoverer = func(ip net.IP, client *http.Client) scanner {
		if !ip.Equal(net.IPv4(127, 0, 0, 1)) || client.Timeout != 5*time.Second {
			t.Error("wrong discovery interface or client timeout")
		}
		return scannerFunc(func(context.Context) ([]core.Device, error) {
			return []core.Device{{Target: api.Target{ID: "virtual-tv", Name: "Virtual television", State: api.TargetOnline}}}, errors.New("another device failed")
		})
	}
	var logsMu sync.Mutex
	var logs []string
	deps.logf = func(format string, args ...any) {
		logsMu.Lock()
		defer logsMu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	listening := make(chan net.Addr, 1)
	deps.listen = func(network, addr string) (net.Listener, error) {
		listener, err := net.Listen(network, addr)
		if err == nil {
			listening <- listener.Addr()
		}
		return listener, err
	}
	done := make(chan error, 1)
	go func() { done <- runWith(ctx, "127.0.0.1:0", "test-nic", time.Hour, 0, deps) }()
	var addr net.Addr
	select {
	case addr = <-listening:
	case <-time.After(5 * time.Second):
		t.Fatal("server not listening")
	}
	client := &http.Client{Timeout: time.Second}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(5 * time.Second)
	for {
		resp, err := client.Get("http://" + addr.String() + "/v1/targets")
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Targets []api.Target `json:"targets"`
		}
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Targets) == 1 {
			if result.Targets[0].ID != "virtual-tv" {
				t.Fatal(result)
			}
			break
		}
		select {
		case <-ticker.C:
		case <-timeout:
			t.Fatal("partial discovery result not published")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not shut down")
	}
	logsMu.Lock()
	defer logsMu.Unlock()
	combined := strings.Join(logs, "\n")
	for _, part := range []string{"API listening", "another device failed", "target discovered: Virtual television"} {
		if !strings.Contains(combined, part) {
			t.Fatalf("missing log %q: %s", part, combined)
		}
	}
}

func TestRuntimeDiscardsDiscoveryResultsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := systemRuntime()
	entered := make(chan struct{})
	deps.newDiscoverer = func(net.IP, *http.Client) scanner {
		return scannerFunc(func(ctx context.Context) ([]core.Device, error) {
			close(entered)
			<-ctx.Done()
			return []core.Device{{Target: api.Target{ID: "stale-result"}}}, ctx.Err()
		})
	}
	var discovered atomic.Bool
	deps.logf = func(format string, args ...any) {
		if strings.HasPrefix(format, "target discovered") {
			discovered.Store(true)
		}
	}
	done := make(chan error, 1)
	go func() { done <- runWith(ctx, "127.0.0.1:0", "", time.Hour, 0, deps) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("scan did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown hung")
	}
	if discovered.Load() {
		t.Fatal("canceled scan published a stale device")
	}
}
