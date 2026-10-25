package netguard

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestErrorFormattingAndUnwrap(t *testing.T) {
	cause := errors.New("dial refused")
	withCause := &Error{Code: CodeTargetUnreach, Message: "无法连接服务", Cause: cause}
	if got, want := withCause.Error(), CodeTargetUnreach+": 无法连接服务: dial refused"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(withCause, cause) {
		t.Fatal("Unwrap did not expose the cause")
	}
	plain := &Error{Code: CodeInvalidURL, Message: "服务地址不能为空"}
	if got, want := plain.Error(), CodeInvalidURL+": 服务地址不能为空"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestCheckURLRejectsMalformedAddresses(t *testing.T) {
	guard := New(false)
	ctx := context.Background()
	for _, raw := range []string{"", "not a url", "http://example.com:bad"} {
		if err := guard.CheckURL(ctx, raw); CodeOf(err) != CodeInvalidURL {
			t.Errorf("CheckURL(%q) = %v, want %s", raw, err, CodeInvalidURL)
		}
	}
}

func TestLookupUsesTheSystemResolver(t *testing.T) {
	guard := New(false)
	ctx := context.Background()
	addresses, err := guard.lookup(ctx, "localhost")
	if err != nil {
		t.Skipf("localhost does not resolve in this environment: %v", err)
	}
	if len(addresses) == 0 {
		t.Fatal("localhost resolved to no addresses")
	}
	if err := guard.CheckURL(ctx, "http://localhost:8080"); CodeOf(err) != CodePrivateTarget {
		t.Fatalf("CheckURL(localhost) = %v, want %s", err, CodePrivateTarget)
	}
}

func TestCheckURLReportsResolverFailures(t *testing.T) {
	guard := New(false)
	guard.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("dns offline")
		},
	}
	err := guard.CheckURL(context.Background(), "http://unresolvable.example")
	if CodeOf(err) != CodeTargetUnreach {
		t.Fatalf("CheckURL = %v, want %s", err, CodeTargetUnreach)
	}
	if !strings.Contains(err.Error(), "无法解析服务地址") {
		t.Fatalf("error = %v", err)
	}
}

func TestDialContextReportsLookupFailures(t *testing.T) {
	guard := New(false)
	guard.lookupIP = func(context.Context, string) ([]net.IP, error) {
		return nil, errors.New("dns offline")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := guard.DialContext(ctx, "tcp", "unresolvable.example:80"); CodeOf(err) != CodeTargetUnreach {
		t.Fatalf("DialContext = %v, want %s", err, CodeTargetUnreach)
	}
}

func TestClientDefaultsAPositiveTimeout(t *testing.T) {
	client := New(true).Client(0)
	if client.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v", client.Timeout)
	}
	if client.Transport == nil {
		t.Fatal("the defaulted client lost its guarded transport")
	}
}
