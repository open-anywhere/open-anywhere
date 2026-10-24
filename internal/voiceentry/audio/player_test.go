package audio

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// writeWithin fails the test instead of blocking forever, which is what a
// player that kept a half-started process would do on the second write.
func writeWithin(t *testing.T, p *Player, pcm []byte) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.Write(pcm) }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked")
		return nil
	}
}

func TestPlayerDiscardsFailedStart(t *testing.T) {
	p, err := NewPlayer(16000)
	if err != nil {
		t.Fatal(err)
	}
	p.build = func(int) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "missing-ffplay"))
	}
	if err := writeWithin(t, p, make([]byte, 640)); err == nil {
		t.Fatal("write with a missing player binary succeeded")
	}
	p.mu.Lock()
	cmd, stdin := p.cmd, p.stdin
	p.mu.Unlock()
	if cmd != nil || stdin != nil {
		t.Fatal("player kept a process that never started")
	}
	if err := writeWithin(t, p, make([]byte, 640)); err == nil {
		t.Fatal("second write with a missing player binary succeeded")
	}
}

func TestPlayerRestartsAfterProcessExit(t *testing.T) {
	p, err := NewPlayer(16000)
	if err != nil {
		t.Fatal(err)
	}
	p.build = func(int) *exec.Cmd { return exec.Command("sleep", "30") }
	defer p.Flush()
	if err := writeWithin(t, p, make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	first := p.cmd
	p.mu.Unlock()
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := writeWithin(t, p, make([]byte, 640))
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writing to a dead player never failed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.mu.Lock()
	retained := p.cmd != nil || p.stdin != nil
	p.mu.Unlock()
	if retained {
		t.Fatal("player kept a dead process")
	}
	// The next chunk starts a fresh process instead of failing forever.
	if err := writeWithin(t, p, make([]byte, 640)); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
	p.mu.Lock()
	restarted := p.cmd != nil && p.cmd != first
	p.mu.Unlock()
	if !restarted {
		t.Fatal("write after recovery did not start a new process")
	}
}
