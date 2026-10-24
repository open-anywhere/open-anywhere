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
}

func NewPlayer(rate int) (*Player, error) {
	if rate <= 0 {
		rate = SampleRate
	}
	return &Player{rate: rate}, nil
}
func (p *Player) Write(pcm []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil {
		p.cmd = exec.Command("ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", "-fflags", "nobuffer", "-probesize", "32", "-f", "s16le", "-ar", fmt.Sprint(p.rate), "-ac", "1", "-i", "pipe:0")
		in, err := p.cmd.StdinPipe()
		if err != nil {
			return err
		}
		p.stdin = in
		p.started = time.Now()
		if err := p.cmd.Start(); err != nil {
			return err
		}
	}
	if _, err := p.stdin.Write(pcm); err != nil {
		return err
	}
	p.queued += time.Duration(len(pcm)/2) * time.Second / time.Duration(p.rate)
	return nil
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
	if p.cmd != nil {
		_ = p.cmd.Process.Kill()
		p.cmd = nil
		p.stdin = nil
		p.queued = 0
	}
}
func (p *Player) Drain() error {
	p.mu.Lock()
	cmd := p.cmd
	in := p.stdin
	p.mu.Unlock()
	if cmd == nil {
		return nil
	}
	_ = in.Close()
	err := cmd.Wait()
	p.mu.Lock()
	p.cmd = nil
	p.stdin = nil
	p.queued = 0
	p.mu.Unlock()
	return err
}
