package dlna

import (
	"bufio"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	core "github.com/open-anywhere/open-anywhere/internal/casting"
	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// Discoverer sends SSDP searches from a selected LAN IPv4 address. Each result
// remains valid for the device's advertised max-age, rather than a fixed timeout.
type Discoverer struct {
	LocalIP     net.IP
	Destination string
	Window      time.Duration
	Client      *http.Client
}

type advertisement struct {
	location string
	ttl      time.Duration
}

func parseAdvertisement(data []byte) (advertisement, error) {
	resp, err := http.ReadResponse(bufio.NewReader(strings.NewReader(string(data))), nil)
	if err != nil {
		return advertisement{}, err
	}
	defer resp.Body.Close()
	u, err := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != 200 || err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return advertisement{}, fmt.Errorf("invalid SSDP location or status")
	}
	ttl := 30 * time.Minute
	for _, part := range strings.Split(resp.Header.Get("Cache-Control"), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(key, "max-age") {
			n, err := strconv.Atoi(strings.Trim(value, `"`))
			if err != nil || n < 0 {
				return advertisement{}, fmt.Errorf("invalid SSDP max-age")
			}
			if n > 86400 {
				n = 86400
			}
			ttl = time.Duration(n) * time.Second
		}
	}
	return advertisement{location: u.String(), ttl: ttl}, nil
}

func (d *Discoverer) Scan(ctx context.Context) ([]core.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	window := d.Window
	if window <= 0 {
		window = 3 * time.Second
	}
	destination := d.Destination
	if destination == "" {
		destination = "239.255.255.250:1900"
	}
	remote, err := net.ResolveUDPAddr("udp4", destination)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: d.LocalIP})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	cancelClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer cancelClose()
	deadline := time.Now().Add(window)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = conn.SetDeadline(deadline)
	query := "M-SEARCH * HTTP/1.1\r\nHOST:239.255.255.250:1900\r\nMAN:\"ssdp:discover\"\r\nMX:1\r\nST:urn:schemas-upnp-org:device:MediaRenderer:1\r\n\r\n"
	if _, err := conn.WriteToUDP([]byte(query), remote); err != nil {
		return nil, err
	}
	ads := map[string]advertisement{}
	buf := make([]byte, 65535)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if e, ok := err.(net.Error); ok && e.Timeout() {
				// The socket deadline may fire just before Context.Err updates.
				if end, ok := ctx.Deadline(); ok && !time.Now().Before(end) {
					return nil, context.DeadlineExceeded
				}
				break
			}
			return nil, err
		}
		if ad, err := parseAdvertisement(buf[:n]); err == nil {
			ads[ad.location] = ad
		}
	}
	devices := make([]core.Device, 0, len(ads))
	var failures []string
	for _, ad := range ads {
		device, err := d.describe(ctx, ad)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		devices = append(devices, device)
	}
	if len(failures) > 0 {
		return devices, fmt.Errorf("device descriptions: %s", strings.Join(failures, "; "))
	}
	return devices, nil
}

type descriptionDevice struct {
	Type     string `xml:"deviceType"`
	UDN      string `xml:"UDN"`
	Name     string `xml:"friendlyName"`
	Services []struct {
		Type    string `xml:"serviceType"`
		Control string `xml:"controlURL"`
	} `xml:"serviceList>service"`
	Children []descriptionDevice `xml:"deviceList>device"`
}

func findRenderer(d descriptionDevice) (descriptionDevice, string, bool) {
	if d.Type == "urn:schemas-upnp-org:device:MediaRenderer:1" {
		for _, s := range d.Services {
			if s.Type == avTransport && s.Control != "" {
				return d, s.Control, true
			}
		}
	}
	for _, child := range d.Children {
		if found, path, ok := findRenderer(child); ok {
			return found, path, true
		}
	}
	return descriptionDevice{}, "", false
}
func (d *Discoverer) describe(ctx context.Context, ad advertisement) (core.Device, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ad.location, nil)
	if err != nil {
		return core.Device{}, err
	}
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return core.Device{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return core.Device{}, fmt.Errorf("description: HTTP %d", resp.StatusCode)
	}
	var root struct {
		Base   string            `xml:"URLBase"`
		Device descriptionDevice `xml:"device"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&root); err != nil {
		return core.Device{}, err
	}
	renderer, control, ok := findRenderer(root.Device)
	if !ok || strings.TrimSpace(renderer.UDN) == "" {
		return core.Device{}, fmt.Errorf("missing MediaRenderer identity or AVTransport service")
	}
	base, _ := url.Parse(ad.location)
	if root.Base != "" {
		base, err = url.Parse(root.Base)
		if err != nil {
			return core.Device{}, err
		}
	}
	ref, err := url.Parse(control)
	if err != nil {
		return core.Device{}, err
	}
	resolved := base.ResolveReference(ref)
	if resolved.Hostname() == "" || (resolved.Scheme != "http" && resolved.Scheme != "https") {
		return core.Device{}, fmt.Errorf("invalid AVTransport control URL")
	}
	return core.Device{
		Target:     api.Target{ID: "dlna-" + strings.TrimPrefix(renderer.UDN, "uuid:"), Name: renderer.Name, State: api.TargetOnline, Endpoints: []api.Endpoint{{ID: "dlna-renderer", Protocol: "dlna", Capabilities: api.Capabilities{Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Streaming: true}}}},
		ControlURL: resolved.String(), ExpiresAt: time.Now().Add(ad.ttl),
	}, nil
}
