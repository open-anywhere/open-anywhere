//go:build unix

package audio

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestFlushReapsPlayerProcess(t *testing.T) {
	p, err := NewPlayer(16000)
	if err != nil {
		t.Fatal(err)
	}
	p.build = func(int) *exec.Cmd { return exec.Command("sleep", "30") }
	if err := p.Write(make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	pid := p.cmd.Process.Pid
	p.mu.Unlock()
	p.Flush()
	// A killed but unreaped child stays visible as a zombie, so this only
	// passes once Flush has waited for it.
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("flush left player process %d behind: %v", pid, err)
	}
}
