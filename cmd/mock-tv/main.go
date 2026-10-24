// Command mock-tv runs a fake DLNA MediaRenderer for local development. The
// casting service discovers it like a television, and a browser page shows the
// stream that the control plane projects to it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/mocktv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("mock-tv", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	addr := flags.String("addr", ":8095", "HTTP listen address")
	ifaceName := flags.String("iface", "", "network interface for SSDP, e.g. en0")
	publicURL := flags.String("public-url", "", "base URL advertised to casting and opened in the browser (default http://127.0.0.1:<port>)")
	name := flags.String("name", "Mock TV", "DLNA friendly name")
	udn := flags.String("udn", "uuid:open-anywhere-mock-tv", "DLNA device UDN")
	maxAge := flags.Duration("max-age", 30*time.Minute, "SSDP max-age")
	noSSDP := flags.Bool("no-ssdp", false, "do not answer SSDP; drive the page with POST /api/play")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("mock-tv does not accept positional arguments")
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	base, err := publicBase(*publicURL, listener.Addr())
	if err != nil {
		_ = listener.Close()
		return err
	}

	display := mocktv.NewDisplay(mocktv.Config{Name: *name, UDN: *udn, Logf: log.Printf})
	server := &http.Server{
		Handler:           display.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()

	var responder *mocktv.Responder
	if !*noSSDP {
		var nic *net.Interface
		if *ifaceName != "" {
			nic, err = net.InterfaceByName(*ifaceName)
			if err != nil {
				_ = server.Close()
				return err
			}
		}
		responder, err = mocktv.ListenSSDP(mocktv.SSDPConfig{
			Interface: nic,
			Location:  base + "/device.xml",
			UDN:       *udn,
			MaxAge:    *maxAge,
			Logf:      log.Printf,
		})
		if err != nil {
			_ = server.Close()
			return fmt.Errorf("%w (try -iface en0 or -no-ssdp)", err)
		}
		defer responder.Close()
	}

	log.Printf("mock-tv listening on %s; open %s/", listener.Addr(), base)
	if *noSSDP {
		log.Printf("mock-tv: SSDP disabled; POST /api/play to drive the page")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	return nil
}

// publicBase decides which URL is advertised to casting and shown to the user.
func publicBase(raw string, addr net.Addr) (string, error) {
	if raw != "" {
		parsed, err := url.Parse(strings.TrimRight(raw, "/"))
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "", errors.New("-public-url must be an absolute HTTP(S) URL")
		}
		if parsed.Path != "" && parsed.Path != "/" {
			return "", errors.New("-public-url must not contain a path")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", errors.New("-public-url must not contain a query or fragment")
		}
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "", err
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected listen address %s", addr)
	}
	return "http://" + net.JoinHostPort(host, fmt.Sprint(tcp.Port)), nil
}
