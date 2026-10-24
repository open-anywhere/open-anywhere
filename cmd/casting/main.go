// Command casting runs the standalone LAN casting API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	core "github.com/open-anywhere/open-anywhere/internal/casting"
	"github.com/open-anywhere/open-anywhere/internal/casting/dlna"
)

func main() { os.Exit(command(os.Args[1:], os.Stderr, run)) }

// command keeps flag parsing and exit-code decisions testable without exiting
// the process or touching the network.
func command(args []string, output io.Writer, start func(string, string, time.Duration, time.Duration) error) int {
	flags := flag.NewFlagSet("casting", flag.ContinueOnError)
	flags.SetOutput(output)
	addr := flags.String("addr", ":8080", "HTTP listen address")
	lan := flags.String("interface", "", "LAN network interface for SSDP (recommended with a VPN)")
	interval := flags.Duration("scan-interval", 30*time.Second, "interval between SSDP scans and session sweeps")
	grace := flags.Duration("unreachable-grace", 30*time.Second, "how long a session may fail to reach its target before releasing the screen; negative disables automatic release")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(output, "casting does not accept positional arguments")
		return 2
	}
	if err := start(*addr, *lan, *interval, *grace); err != nil {
		fmt.Fprintln(output, err)
		return 1
	}
	return 0
}

type scanner interface {
	Scan(context.Context) ([]core.Device, error)
}

type runtimeDependencies struct {
	interfaceByName func(string) (*net.Interface, error)
	interfaceAddrs  func(*net.Interface) ([]net.Addr, error)
	newDiscoverer   func(net.IP, *http.Client) scanner
	listen          func(string, string) (net.Listener, error)
	logf            func(string, ...any)
}

func systemRuntime() runtimeDependencies {
	return runtimeDependencies{
		interfaceByName: net.InterfaceByName,
		interfaceAddrs:  func(nic *net.Interface) ([]net.Addr, error) { return nic.Addrs() },
		newDiscoverer:   func(ip net.IP, client *http.Client) scanner { return &dlna.Discoverer{LocalIP: ip, Client: client} },
		listen:          net.Listen,
		logf:            log.Printf,
	}
}

func run(addr, iface string, interval, grace time.Duration) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runWith(ctx, addr, iface, interval, grace, systemRuntime())
}

func runWith(ctx context.Context, addr, iface string, interval, grace time.Duration, deps runtimeDependencies) error {
	if interval <= 0 {
		return fmt.Errorf("scan-interval must be positive")
	}
	var ip net.IP
	if iface != "" {
		nic, err := deps.interfaceByName(iface)
		if err != nil {
			return err
		}
		addresses, err := deps.interfaceAddrs(nic)
		if err != nil {
			return err
		}
		for _, a := range addresses {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				ip = n.IP.To4()
				break
			}
		}
		if ip == nil {
			return fmt.Errorf("interface %s has no IPv4 address", iface)
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	registry := core.NewRegistry()
	service := core.NewService(registry, map[string]core.Adapter{"dlna": &dlna.Transport{Client: client}}, core.Config{UnreachableGrace: grace})
	discoverer := deps.newDiscoverer(ip, client)
	refresh := func(ctx context.Context) {
		devices, err := discoverer.Scan(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			deps.logf("discovery: %v", err)
		}
		for _, device := range devices {
			registry.Upsert(device)
			deps.logf("target discovered: %s (%s)", device.Target.Name, device.Target.ID)
		}
		// Reuse the discovery tick to refresh sessions nobody is querying and
		// to release reservations whose target stopped answering.
		service.Sweep(ctx)
	}
	listener, err := deps.listen("tcp", addr)
	if err != nil {
		return err
	}
	deps.logf("casting API listening on %s", listener.Addr())
	return core.Serve(ctx, listener, core.NewHandler(service), interval, refresh)
}
