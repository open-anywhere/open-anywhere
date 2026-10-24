//go:build darwin || linux

package render

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// Each application/encoder owns a process group. Shutdown includes children.
func ownProcessGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func stopProcess(cmd *exec.Cmd, done <-chan struct{}) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid := -cmd.Process.Pid
	if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	// The parent can exit before its children; always kill remaining group members.
	if err := syscall.Kill(pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	select {
	case <-done:
		return nil
	case <-time.After(3 * time.Second):
		return errors.New("process did not exit after SIGKILL")
	}
}
