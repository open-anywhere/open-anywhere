package main

import (
	"net"
	"testing"
)

func TestPublicBase(t *testing.T) {
	defaultAddr := &net.TCPAddr{IP: net.IPv4zero, Port: 8095}
	cases := []struct {
		name    string
		raw     string
		addr    net.Addr
		want    string
		wantErr bool
	}{
		{name: "default wildcard", addr: defaultAddr, want: "http://127.0.0.1:8095"},
		{name: "default IPv6 wildcard", addr: &net.TCPAddr{IP: net.IPv6unspecified, Port: 8095}, want: "http://127.0.0.1:8095"},
		{name: "default specific address", addr: &net.TCPAddr{IP: net.ParseIP("192.168.1.4"), Port: 8095}, want: "http://192.168.1.4:8095"},
		{name: "explicit URL", raw: "http://192.168.1.4:8095/", addr: defaultAddr, want: "http://192.168.1.4:8095"},
		{name: "explicit HTTPS URL", raw: "https://tv.example", addr: defaultAddr, want: "https://tv.example"},
		{name: "URL with path", raw: "http://192.168.1.4:8095/path", addr: defaultAddr, wantErr: true},
		{name: "URL with query", raw: "http://192.168.1.4:8095?x=1", addr: defaultAddr, wantErr: true},
		{name: "URL with fragment", raw: "https://tv.example/#top", addr: defaultAddr, wantErr: true},
		{name: "non HTTP scheme", raw: "ftp://192.168.1.4", addr: defaultAddr, wantErr: true},
		{name: "missing host", raw: "http://", addr: defaultAddr, wantErr: true},
		{name: "malformed", raw: "://bad", addr: defaultAddr, wantErr: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := publicBase(testCase.raw, testCase.addr)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("publicBase(%q) = %q, want error", testCase.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("publicBase(%q): %v", testCase.raw, err)
			}
			if got != testCase.want {
				t.Fatalf("publicBase(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}
