package render

import (
	"context"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func TestAdversarialSystemPortsParsing(t *testing.T) {
	fakeLsof(t, "printf 'p1\\nn*:443\\nn127.0.0.1:8080\\nnno-colon\\nn*:70000\\nn*:0\\nn*:65535\\np2\\n'; exit 0")
	ports, err := systemPorts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []int{443, 8080, 65535}
	if !reflect.DeepEqual(ports, want) {
		t.Fatalf("got %v want %v", ports, want)
	}
}

func TestAdversarialSystemPortsFailure(t *testing.T) {
	fakeLsof(t, "exit 2")
	if _, err := systemPorts(context.Background()); err == nil {
		t.Fatal("expected lsof failure")
	}
}

func TestAdversarialPortsMergesSources(t *testing.T) {
	fakeLsof(t, "printf 'n*:443\\nn*:4000\\n'; exit 0")
	s, err := New(Config{
		WorkDir:      t.TempDir(),
		PublicURL:    "http://127.0.0.1:8090",
		ServicePorts: []int{9001, 9001},
		Logger:       discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	s.mu.Lock()
	s.instances["reserved-id"] = &run{info: api.Instance{AppID: "reserved", InstanceID: "reserved-id", Port: 4000}}
	s.ports[4000] = "reserved-id"
	s.mu.Unlock()
	t.Cleanup(func() {
		s.mu.Lock()
		delete(s.instances, "reserved-id")
		delete(s.ports, 4000)
		s.mu.Unlock()
	})

	ports, err := s.Ports(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byPort := map[int]api.Port{}
	for _, p := range ports {
		byPort[p.Port] = p
	}
	if got := byPort[443]; !got.Occupied || got.Owner == nil || *got.Owner != "external" {
		t.Fatalf("external port: %+v", got)
	}
	if got := byPort[4000]; !got.Occupied || got.Owner == nil || *got.Owner != "app" || got.AppID != "reserved" {
		t.Fatalf("app port: %+v", got)
	}
	if got := byPort[9001]; !got.Occupied || got.Owner == nil || *got.Owner != "render" {
		t.Fatalf("service port: %+v", got)
	}
	if !sortedPorts(ports) {
		t.Fatalf("ports not sorted: %+v", ports)
	}
}

func sortedPorts(ports []api.Port) bool {
	for i := 1; i < len(ports); i++ {
		if ports[i-1].Port > ports[i].Port {
			return false
		}
	}
	return true
}

func TestAdversarialPortsInspectionFailure(t *testing.T) {
	s := newFakeService(t, fakeServiceOptions{})
	fakeLsof(t, "exit 2")
	if _, err := s.Ports(context.Background()); err == nil {
		t.Fatal("expected Ports failure")
	}
	if _, err := s.Port(availableTestPort(t)); err == nil {
		t.Fatal("expected Port inspection failure")
	}
}

func TestAdversarialPortAvailableBranches(t *testing.T) {
	t.Run("lsof error", func(t *testing.T) {
		fakeLsof(t, "exit 2")
		if _, err := portAvailable(availableTestPort(t)); err == nil {
			t.Fatal("expected lsof error")
		}
	})

	t.Run("address in use", func(t *testing.T) {
		fakeLsof(t, "exit 1") // exit 1 with no output means "no listeners"
		// macOS lets a wildcard bind coexist with a specific-address listener,
		// so the conflicting listener must itself be on the wildcard address to
		// reach the EADDRINUSE branch.
		ln, err := net.Listen("tcp4", "0.0.0.0:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		port := ln.Addr().(*net.TCPAddr).Port
		free, err := portAvailable(port)
		if err != nil || free {
			t.Fatalf("got free=%v err=%v", free, err)
		}
	})

	t.Run("invalid bind", func(t *testing.T) {
		fakeLsof(t, "exit 1")
		free, err := portAvailable(70000)
		if err == nil || free {
			t.Fatalf("got free=%v err=%v", free, err)
		}
	})

	t.Run("lsof reports listener", func(t *testing.T) {
		fakeLsof(t, "printf 'n*:1234\\n'; exit 0")
		free, err := portAvailable(1234)
		if err != nil || free {
			t.Fatalf("got free=%v err=%v", free, err)
		}
	})
}

func TestAdversarialLsofErrorModes(t *testing.T) {
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := lsof(ctx, "-iTCP", "-sTCP:LISTEN", "-Fn"); err == nil {
			t.Fatal("expected context error")
		}
	})

	t.Run("stderr", func(t *testing.T) {
		fakeLsof(t, "echo boom >&2; exit 0")
		_, err := lsof(context.Background(), "-iTCP", "-sTCP:LISTEN", "-Fn")
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("expected stderr error, got %v", err)
		}
	})

	t.Run("exit one with output", func(t *testing.T) {
		fakeLsof(t, "printf 'n*:1\\n'; exit 1")
		if _, err := lsof(context.Background(), "-iTCP", "-sTCP:LISTEN", "-Fn"); err == nil {
			t.Fatal("expected exit error")
		}
	})
}

func TestAdversarialOwnsListener(t *testing.T) {
	fakeLsof(t, "printf 'n127.0.0.1:80\\n'; exit 0")
	if !ownsListener(context.Background(), 1, 80) {
		t.Fatal("expected owned listener")
	}
	fakeLsof(t, "exit 0")
	if ownsListener(context.Background(), 1, 80) {
		t.Fatal("empty output must not own a listener")
	}
	fakeLsof(t, "printf 'n127.0.0.1:80\\n'; exit 2")
	if ownsListener(context.Background(), 1, 80) {
		t.Fatal("failed lsof must not own a listener")
	}
}

func TestAdversarialPortLockedServiceAndExternal(t *testing.T) {
	s, err := New(Config{WorkDir: t.TempDir(), PublicURL: "http://127.0.0.1:8090", ServicePorts: []int{9002}, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.cancel)
	s.mu.Lock()
	reserved := s.portLocked(9002)
	s.mu.Unlock()
	if !reserved.Occupied || reserved.Owner == nil || *reserved.Owner != "render" {
		t.Fatalf("service port: %+v", reserved)
	}

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	external := s.portLocked(ln.Addr().(*net.TCPAddr).Port)
	if external.Occupied {
		t.Fatalf("portLocked must not probe external listeners: %+v", external)
	}
	p, err := s.Port(ln.Addr().(*net.TCPAddr).Port)
	if err != nil || !p.Occupied || p.Owner == nil || *p.Owner != "external" {
		t.Fatalf("external port: %+v %v", p, err)
	}
}
