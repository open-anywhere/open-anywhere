package main

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestStartupRejectsInvalidConfiguration(t *testing.T) {
	if err := run("127.0.0.1:0", "", 0); err == nil || !strings.Contains(err.Error(), "scan-interval") {
		t.Fatalf("invalid scan interval accepted: %v", err)
	}
	if err := run("127.0.0.1:0", "casting-interface-does-not-exist", time.Second); err == nil {
		t.Fatal("invalid interface accepted")
	}
}

func TestStartupUsesLocalInterfaceAndReportsListenFailure(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			t.Fatal(err)
		}
		for _, address := range addresses {
			ip, ok := address.(*net.IPNet)
			if !ok || ip.IP.To4() == nil {
				continue
			}
			// An invalid listen address fails before discovery starts. Only inspect
			// loopback interface metadata; no multicast or LAN traffic is sent.
			err := run("invalid listen address", iface.Name, time.Second)
			var opErr *net.OpError
			if !errors.As(err, &opErr) {
				t.Fatalf("want listen failure after interface selection, got %v", err)
			}
			return
		}
	}
	t.Fatal("no local IPv4 loopback interface")
}
func TestStartupDefaultInterfaceReportsListenFailure(t *testing.T) {
	var opErr *net.OpError
	if err := run("invalid listen address", "", time.Second); !errors.As(err, &opErr) {
		t.Fatalf("want listen error, got %v", err)
	}
}
