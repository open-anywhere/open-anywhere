// Package hostiface picks the LAN interface virtual devices use for SSDP/DLNA.
// An empty or "auto" selection enables casting only when exactly one suitable
// IPv4 NIC is present; "off" keeps casting disabled; any other value must name
// an interface that currently has an IPv4 address.
package hostiface

import (
	"fmt"
	"net"
	"strings"
)

// Selection sentinels stored in platform settings and accepted from config.
const (
	Off  = "off"
	Auto = "auto"
)

// Interface is one up, non-loopback NIC that has at least one IPv4 address.
type Interface struct {
	Name string `json:"name"`
	IPv4 string `json:"ipv4"`
}

// Status is the resolved casting bind address for a stored selection.
type Status struct {
	Selection string `json:"selection"`
	Active    bool   `json:"active"`
	Name      string `json:"name,omitempty"`
	IPv4      string `json:"ipv4,omitempty"`
	Message   string `json:"message,omitempty"`
}

// listFn is replaced in tests.
var listFn = listSystem

// List returns candidate interfaces in a stable order.
func List() ([]Interface, error) {
	return listFn()
}

// Normalize maps blank input to Auto and lowercases the off/auto sentinels.
func Normalize(selection string) string {
	trimmed := strings.TrimSpace(selection)
	switch strings.ToLower(trimmed) {
	case "", Auto:
		return Auto
	case Off:
		return Off
	default:
		return trimmed
	}
}

// Resolve turns a selection into a bind address. A disabled result returns a
// nil IP and a Status explaining why; unknown interface names are errors.
func Resolve(selection string) (net.IP, Status, error) {
	normalized := Normalize(selection)
	status := Status{Selection: normalized}
	candidates, err := List()
	if err != nil {
		return nil, status, err
	}
	switch normalized {
	case Off:
		status.Message = "投屏已关闭"
		return nil, status, nil
	case Auto:
		switch len(candidates) {
		case 0:
			status.Message = "没有可用的局域网网卡，投屏未启用"
			return nil, status, nil
		case 1:
			ip := net.ParseIP(candidates[0].IPv4).To4()
			if ip == nil {
				status.Message = "唯一网卡没有有效的 IPv4 地址，投屏未启用"
				return nil, status, nil
			}
			status.Active = true
			status.Name = candidates[0].Name
			status.IPv4 = candidates[0].IPv4
			return ip, status, nil
		default:
			names := make([]string, 0, len(candidates))
			for _, item := range candidates {
				names = append(names, item.Name)
			}
			status.Message = fmt.Sprintf("检测到多张网卡（%s），请在管理设置里指定投屏网卡", strings.Join(names, ", "))
			return nil, status, nil
		}
	default:
		for _, item := range candidates {
			if item.Name != normalized {
				continue
			}
			ip := net.ParseIP(item.IPv4).To4()
			if ip == nil {
				return nil, status, fmt.Errorf("interface %s has no IPv4 address", normalized)
			}
			status.Active = true
			status.Name = item.Name
			status.IPv4 = item.IPv4
			return ip, status, nil
		}
		return nil, status, fmt.Errorf("interface %s not found or has no IPv4 address", normalized)
	}
}

func listSystem() ([]Interface, error) {
	nics, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]Interface, 0, len(nics))
	for _, nic := range nics {
		if nic.Flags&net.FlagUp == 0 || nic.Flags&net.FlagLoopback != 0 {
			continue
		}
		if skipVirtual(nic.Name) {
			continue
		}
		addresses, err := nic.Addrs()
		if err != nil {
			return nil, err
		}
		var ipv4 string
		for _, address := range addresses {
			network, ok := address.(*net.IPNet)
			if !ok {
				continue
			}
			if ip := network.IP.To4(); ip != nil && !ip.IsLoopback() {
				ipv4 = ip.String()
				break
			}
		}
		if ipv4 == "" {
			continue
		}
		out = append(out, Interface{Name: nic.Name, IPv4: ipv4})
	}
	return out, nil
}

// skipVirtual hides common container/VM bridges that are almost never the
// LAN facing the TV. Operators can still type the name manually if needed.
func skipVirtual(name string) bool {
	lower := strings.ToLower(name)
	prefixes := []string{
		"docker", "br-", "veth", "vmnet", "vboxnet", "hyper-v",
		"vethernet", "tailscale", "zt", "tun", "tap", "wg", "utun",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
