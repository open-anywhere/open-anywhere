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
	return stopProcessWith(cmd, done, syscall.Kill, 2*time.Second, 3*time.Second)
}

func stopProcessWith(cmd *exec.Cmd, done <-chan struct{}, kill func(int, syscall.Signal) error, termGrace, killGrace time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid := -cmd.Process.Pid
	signalGroup := func(sig syscall.Signal) error {
		err := kill(pgid, sig)
		if err != nil && errors.Is(err, syscall.EPERM) {
			// A group that is already going away can be reported as EPERM on
			// macOS: the id is recycled or the group is in a transient exit
			// state. There is nothing actionable left to signal, so treat it
			// like ESRCH instead of turning a clean shutdown into STOP_FAILED.
			return nil
		}
		return err
	}
	if err := signalGroup(syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	select {
	case <-done:
	case <-time.After(termGrace):
	}
	// The parent can exit before its children; always kill remaining group members.
	if err := signalGroup(syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	select {
	case <-done:
		return nil
	case <-time.After(killGrace):
		return errors.New("process did not exit after SIGKILL")
	}
}
