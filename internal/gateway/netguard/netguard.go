// Package netguard applies the outbound URL policy for user-configured Render
// and Casting services. It rejects non-HTTP schemes and, by default, addresses
// on the private network so a binding cannot be used to probe the deployment.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Stable error codes shared with the portal's API responses.
const (
	CodeInvalidURL     = "INVALID_TARGET_URL"
	CodePrivateTarget  = "PRIVATE_TARGET_FORBIDDEN"
	CodeTargetUnreach  = "TARGET_UNREACHABLE"
	maxRedirects       = 5
	defaultDialTimeout = 10 * time.Second
)

// Error is a policy or connectivity failure with a stable code.
type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Code + ": " + e.Message + ": " + e.Cause.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

// CodeOf returns the netguard code carried by err, or an empty string.
func CodeOf(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

// Guard validates and dials outbound service addresses.
type Guard struct {
	allowPrivate bool
	resolver     *net.Resolver
	dialer       *net.Dialer
	// lookupIP overrides hostname resolution; tests use it to simulate DNS.
	lookupIP func(context.Context, string) ([]net.IP, error)
}

// New returns a guard. allowPrivate disables the private-network rejection and
// exists only for local debugging.
func New(allowPrivate bool) *Guard {
	return &Guard{
		allowPrivate: allowPrivate,
		resolver:     net.DefaultResolver,
		dialer:       &net.Dialer{Timeout: defaultDialTimeout, KeepAlive: 30 * time.Second},
	}
}

// AllowPrivate reports whether private addresses are permitted.
func (g *Guard) AllowPrivate() bool { return g.allowPrivate }

// ValidateURL checks the shape of an address without resolving it.
func (g *Guard) ValidateURL(raw string) error {
	_, err := g.parse(raw)
	return err
}

// CheckURL validates the shape and resolves the host, rejecting private
// addresses unless the guard allows them. It is used when an address is saved
// or tested rather than on every request.
func (g *Guard) CheckURL(ctx context.Context, raw string) error {
	parsed, err := g.parse(raw)
	if err != nil {
		return err
	}
	if g.allowPrivate {
		return nil
	}
	return g.checkHost(ctx, parsed.Hostname())
}

func (g *Guard) parse(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, &Error{Code: CodeInvalidURL, Message: "服务地址不能为空"}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, &Error{Code: CodeInvalidURL, Message: "服务地址不是有效的 URL", Cause: err}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, &Error{Code: CodeInvalidURL, Message: "服务地址必须使用 http 或 https"}
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, &Error{Code: CodeInvalidURL, Message: "服务地址缺少主机名"}
	}
	if parsed.User != nil {
		return nil, &Error{Code: CodeInvalidURL, Message: "服务地址不能包含用户名或密码"}
	}
	if parsed.Fragment != "" {
		return nil, &Error{Code: CodeInvalidURL, Message: "服务地址不能包含片段"}
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, &Error{Code: CodeInvalidURL, Message: "服务地址端口无效"}
		}
	}
	return parsed, nil
}

func (g *Guard) checkHost(ctx context.Context, host string) error {
	addresses, err := g.lookup(ctx, host)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if !g.allowed(address) {
			return &Error{Code: CodePrivateTarget, Message: "服务地址不能指向内网或本机"}
		}
	}
	return nil
}

func (g *Guard) lookup(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	var (
		values []net.IP
		err    error
	)
	if g.lookupIP != nil {
		values, err = g.lookupIP(ctx, host)
		if err != nil {
			return nil, &Error{Code: CodeTargetUnreach, Message: "无法解析服务地址", Cause: err}
		}
	} else {
		addresses, lookupErr := g.resolver.LookupIPAddr(ctx, host)
		if lookupErr != nil {
			return nil, &Error{Code: CodeTargetUnreach, Message: "无法解析服务地址", Cause: lookupErr}
		}
		values = make([]net.IP, 0, len(addresses))
		for _, address := range addresses {
			values = append(values, address.IP)
		}
	}
	if len(values) == 0 {
		return nil, &Error{Code: CodeTargetUnreach, Message: "服务地址没有可用的 IP"}
	}
	return values, nil
}

// allowed applies the address policy. Unspecified, loopback, private,
// link-local, multicast and carrier-grade NAT addresses are refused unless the
// guard permits private targets.
func (g *Guard) allowed(ip net.IP) bool {
	if g.allowPrivate {
		return true
	}
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10, shared address space used by carrier-grade NAT.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
		// 192.0.0.0/24, IETF protocol assignments.
		if v4[0] == 192 && v4[1] == 0 && v4[2] == 0 {
			return false
		}
	}
	return true
}

// DialContext resolves every dial and connects only to an allowed address, so
// a DNS answer cannot change between validation and use.
func (g *Guard) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, &Error{Code: CodeInvalidURL, Message: "服务地址无效", Cause: err}
	}
	addresses, err := g.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	var (
		lastErr error
		blocked bool
	)
	for _, ip := range addresses {
		if !g.allowed(ip) {
			blocked = true
			continue
		}
		conn, dialErr := g.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	// A policy refusal is reported as such even when some other address also
	// failed to connect: the caller needs to fix the address, not the network.
	if blocked {
		return nil, &Error{Code: CodePrivateTarget, Message: "服务地址不能指向内网或本机", Cause: lastErr}
	}
	if lastErr == nil {
		lastErr = errors.New("no address")
	}
	return nil, &Error{Code: CodeTargetUnreach, Message: "无法连接服务", Cause: lastErr}
}

// Transport returns an HTTP transport without environment proxies (which
// would bypass the dial policy) and with the guarded dialer.
func (g *Guard) Transport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           g.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// Client returns an HTTP client that applies the guard to the initial request
// and to every redirect.
func (g *Guard) Client(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: g.Transport(),
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("netguard: more than %d redirects", maxRedirects)
			}
			// Resolve at redirect time as well; the dial below rechecks the
			// address actually connected to.
			return g.CheckURL(request.Context(), request.URL.String())
		},
	}
}
