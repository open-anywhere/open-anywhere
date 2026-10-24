// Command casting runs the standalone LAN casting API.
package main

import (
	"context"
	"flag"
	"fmt"
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

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	lan := flag.String("interface", "", "LAN network interface for SSDP (recommended with a VPN)")
	interval := flag.Duration("scan-interval", 30*time.Second, "interval between SSDP scans")
	flag.Parse()
	if err := run(*addr, *lan, *interval); err != nil {
		log.Fatal(err)
	}
}
func run(addr, iface string, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("scan-interval must be positive")
	}
	var ip net.IP
	if iface != "" {
		nic, err := net.InterfaceByName(iface)
		if err != nil {
			return err
		}
		addresses, err := nic.Addrs()
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second}
	registry := core.NewRegistry()
	discoverer := &dlna.Discoverer{LocalIP: ip, Client: client}
	refresh := func(ctx context.Context) {
		devices, err := discoverer.Scan(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("discovery: %v", err)
		}
		for _, device := range devices {
			registry.Upsert(device)
			log.Printf("target discovered: %s (%s)", device.Target.Name, device.Target.ID)
		}
	}

	service := core.NewService(registry, map[string]core.Adapter{"dlna": &dlna.Transport{Client: client}})
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("casting API listening on %s", listener.Addr())
	return core.Serve(ctx, listener, core.NewHandler(service), interval, refresh)
}
