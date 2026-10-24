package render

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

func portAvailable(port int) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	listening, err := lsof(ctx, "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fn")
	if err != nil {
		return false, err
	}
	if len(listening) > 0 {
		return false, nil
	}
	// Node and Go both reuse TIME_WAIT sockets. lsof above catches live
	// specific-address listeners that macOS permits alongside a wildcard bind.
	lc := net.ListenConfig{}
	l, err := lc.Listen(ctx, "tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err == nil {
		return true, l.Close()
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return false, nil
	}
	return false, err
}

func lsof(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "lsof", append([]string{"-nP"}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stderr.Len() > 0 {
		return nil, fmt.Errorf("lsof: %s", stderr.String())
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(out) == 0 {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}
func systemPorts(ctx context.Context) ([]int, error) {
	out, err := lsof(ctx, "-iTCP", "-sTCP:LISTEN", "-Fn")
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		i := strings.LastIndexByte(line, ':')
		if i < 0 {
			continue
		}
		p, e := strconv.Atoi(strings.TrimSpace(line[i+1:]))
		if e == nil && p > 0 && p <= 65535 {
			seen[p] = true
		}
	}
	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports, nil
}
func ownsListener(ctx context.Context, pid, port int) bool {
	out, err := lsof(ctx, "-a", "-p", strconv.Itoa(pid), "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fn")
	return err == nil && strings.Contains(string(out), "n")
}
func owner(s string) *string { return &s }
func (s *Service) portLocked(port int) api.Port {
	p := api.Port{Port: port}
	if id, ok := s.ports[port]; ok {
		r := s.instances[id]
		p.Occupied = true
		p.Owner = owner("app")
		p.AppID = r.info.AppID
		p.InstanceID = id
		return p
	}
	if s.servicePorts[port] {
		p.Occupied = true
		p.Owner = owner("render")
	}
	return p
}
func (s *Service) Port(port int) (api.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.portLocked(port)
	if p.Occupied {
		return p, nil
	}
	free, err := portAvailable(port)
	if err != nil {
		return p, err
	}
	if !free {
		p.Occupied = true
		p.Owner = owner("external")
	}
	return p, nil
}
func (s *Service) Ports(ctx context.Context) ([]api.Port, error) {
	ports, err := systemPorts(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := map[int]api.Port{}
	for _, port := range ports {
		p := s.portLocked(port)
		if !p.Occupied {
			p.Occupied = true
			p.Owner = owner("external")
		}
		entries[port] = p
	}
	for port := range s.ports {
		entries[port] = s.portLocked(port)
	}
	for port := range s.servicePorts {
		entries[port] = s.portLocked(port)
	}
	result := make([]api.Port, 0, len(entries))
	for _, p := range entries {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Port < result[j].Port })
	return result, nil
}
