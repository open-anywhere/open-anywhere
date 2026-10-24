package audio

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
)

// Source streams 16kHz mono signed PCM from ffmpeg in 60ms frames.
type Source struct {
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	stopped bool
}

func NewFileSource(path string, tailSeconds int) (*Source, error) {
	if path == "" {
		return nil, fmt.Errorf("audio file is empty")
	}
	if tailSeconds < 0 {
		tailSeconds = 0
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-re", "-i", path, "-af", fmt.Sprintf("apad=pad_dur=%d", tailSeconds), "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1"}
	return start(args)
}
func NewMicSource(device string) (*Source, error) {
	if device == "" {
		device = "default"
	}
	args, err := MicFFmpegArgs(device, runtime.GOOS)
	if err != nil {
		return nil, err
	}
	return start(args)
}

// MicFFmpegArgs returns the platform-specific ffmpeg input arguments.
// Windows uses DirectShow names (audio="Device Name"); macOS uses avfoundation
// indexes; Linux uses ALSA device names.
func MicFFmpegArgs(device, goos string) ([]string, error) {
	base := []string{"-hide_banner", "-loglevel", "error"}
	switch strings.ToLower(goos) {
	case "windows":
		return append(base, "-f", "dshow", "-i", "audio="+device, "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1"), nil
	case "darwin":
		return append(base, "-f", "avfoundation", "-i", ":"+device, "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1"), nil
	case "linux":
		return append(base, "-f", "alsa", "-i", device, "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1"), nil
	default:
		return nil, fmt.Errorf("unsupported microphone platform: %s", goos)
	}
}
func start(args []string) (*Source, error) {
	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Source{cmd: cmd, stdout: out}, nil
}
func (s *Source) ReadFrame() (p []byte, err error) {
	p = make([]byte, PCMFrameBytes)
	_, err = io.ReadFull(s.stdout, p)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return nil, io.EOF
	}
	return p, err
}
func (s *Source) Close() error {
	if s == nil || s.stopped {
		return nil
	}
	s.stopped = true
	_ = s.cmd.Process.Kill()
	return s.cmd.Wait()
}
