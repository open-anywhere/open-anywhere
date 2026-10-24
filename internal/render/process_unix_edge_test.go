//go:build darwin || linux

package render

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func fakeProcessCmd() *exec.Cmd {
	return &exec.Cmd{Process: &os.Process{Pid: 4242}}
}

func TestAdversarialStopProcessKillErrors(t *testing.T) {
	boom := errors.New("kill failed")
	t.Run("term fails", func(t *testing.T) {
		kill := func(int, syscall.Signal) error { return boom }
		if err := stopProcessWith(fakeProcessCmd(), nil, kill, time.Millisecond, time.Millisecond); !errors.Is(err, boom) {
			t.Fatalf("expected term error, got %v", err)
		}
	})

	t.Run("kill fails", func(t *testing.T) {
		calls := 0
		kill := func(int, syscall.Signal) error {
			calls++
			if calls > 1 {
				return boom
			}
			return nil
		}
		if err := stopProcessWith(fakeProcessCmd(), nil, kill, time.Millisecond, time.Millisecond); !errors.Is(err, boom) {
			t.Fatalf("expected kill error, got %v", err)
		}
	})
}

func TestAdversarialStopProcessIgnoresRecycledGroupAfterReap(t *testing.T) {
	kill := func(int, syscall.Signal) error { return syscall.EPERM }
	done := make(chan struct{})
	close(done)
	if err := stopProcessWith(fakeProcessCmd(), done, kill, time.Millisecond, time.Millisecond); err != nil {
		t.Fatalf("EPERM after the process was reaped should be ignored: %v", err)
	}

	// If the process never reports done, the final grace period still fails.
	err := stopProcessWith(fakeProcessCmd(), make(chan struct{}), kill, time.Millisecond, time.Millisecond)
	if err == nil || err.Error() != "process did not exit after SIGKILL" {
		t.Fatalf("expected exit grace failure, got %v", err)
	}
}

func TestAdversarialStopProcessGraceExpiry(t *testing.T) {
	kill := func(int, syscall.Signal) error { return nil }
	done := make(chan struct{})
	err := stopProcessWith(fakeProcessCmd(), done, kill, time.Millisecond, time.Millisecond)
	if err == nil || err.Error() != "process did not exit after SIGKILL" {
		t.Fatalf("expected SIGKILL timeout, got %v", err)
	}
}

func TestAdversarialStopProcessIgnoresMissingProcess(t *testing.T) {
	calls := 0
	kill := func(int, syscall.Signal) error {
		calls++
		return syscall.ESRCH
	}
	done := make(chan struct{})
	close(done)
	if err := stopProcessWith(fakeProcessCmd(), done, kill, time.Millisecond, time.Millisecond); err != nil {
		t.Fatalf("ESRCH should be ignored: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected both signals, got %d", calls)
	}
}

func TestAdversarialStopProcessNilCommand(t *testing.T) {
	if err := stopProcessWith(nil, nil, syscall.Kill, time.Millisecond, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := stopProcessWith(&exec.Cmd{}, nil, syscall.Kill, time.Millisecond, time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
