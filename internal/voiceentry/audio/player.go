package audio

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

type Player struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	started time.Time
	queued  time.Duration
	rate    int
	// build creates the process that plays one stream; NewPlayer sets it to
	// ffplay. Tests replace it to exercise start and read-side failures.
	build func(rate int) *exec.Cmd
}

func NewPlayer(rate int) (*Player, error) {
	if rate <= 0 {
		rate = SampleRate
	}
	return &Player{rate: rate, build: ffplayCommand}, nil
}

func ffplayCommand(rate int) *exec.Cmd {
	return exec.Command("ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", "-fflags", "nobuffer", "-probesize", "32", "-f", "s16le", "-ar", fmt.Sprint(rate), "-ac", "1", "-i", "pipe:0")
}

func (p *Player) Write(pcm []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil {
		if err := p.startLocked(); err != nil {
			return err
		}
	}
	if _, err := p.stdin.Write(pcm); err != nil {
		// ffplay is gone (killed, crashed, or it exited on its own). Reap it,
		// otherwise every later chunk writes into a pipe nobody reads and
		// eventually blocks once the pipe buffer fills.
		p.stopLocked()
		return err
	}
	p.queued += time.Duration(len(pcm)/2) * time.Second / time.Duration(p.rate)
	return nil
}

// startLocked launches the player process. Nothing is retained on failure, so
// a caller that retries after a missing or unusable ffplay starts cleanly.
func (p *Player) startLocked() error {
	cmd := p.build(p.rate)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		return err
	}
	p.cmd, p.stdin = cmd, in
	p.started = time.Now()
	return nil
}

// stopLocked closes the input pipe, kills the process and reaps it. Without
// the Wait, every stopped player stays behind as a zombie.
func (p *Player) stopLocked() {
	cmd, in := p.cmd, p.stdin
	p.cmd, p.stdin = nil, nil
	p.queued = 0
	if cmd == nil {
		return
	}
	if in != nil {
		_ = in.Close()
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

func (p *Player) Remaining() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil {
		return 0
	}
	r := p.queued - time.Since(p.started)
	if r < 0 {
		return 0
	}
	return r
}

func (p *Player) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
}

// Drain closes the input pipe and waits for the buffered audio to finish. The
// process is detached from the player before the wait, so neither a concurrent
// Write, Flush nor a hung ffplay holds the lock while the buffered audio plays.
func (p *Player) Drain() error {
	p.mu.Lock()
	cmd, in := p.cmd, p.stdin
	p.cmd, p.stdin = nil, nil
	p.queued = 0
	p.mu.Unlock()
	if cmd == nil {
		return nil
	}
	if in != nil {
		_ = in.Close()
	}
	return cmd.Wait()
}
