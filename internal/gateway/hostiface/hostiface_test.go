package hostiface

import (
	"errors"
	"net"
	"testing"
)

func withList(t *testing.T, interfaces []Interface, err error) {
	t.Helper()
	previous := listFn
	listFn = func() ([]Interface, error) { return interfaces, err }
	t.Cleanup(func() { listFn = previous })
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":          Auto,
		" auto ":    Auto,
		"OFF":       Off,
		"Ethernet":  "Ethernet",
		"  WLAN  ":  "WLAN",
	}
	for input, want := range cases {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q)=%q want %q", input, got, want)
		}
	}
}

func TestResolveOffAndAuto(t *testing.T) {
	withList(t, []Interface{{Name: "eth0", IPv4: "192.168.1.10"}}, nil)

	ip, status, err := Resolve(Off)
	if err != nil || ip != nil || status.Active {
		t.Fatalf("off: ip=%v status=%+v err=%v", ip, status, err)
	}

	ip, status, err = Resolve(Auto)
	if err != nil || !status.Active || status.Name != "eth0" {
		t.Fatalf("auto single: ip=%v status=%+v err=%v", ip, status, err)
	}
	if !ip.Equal(net.IPv4(192, 168, 1, 10)) {
		t.Fatalf("auto ip=%v", ip)
	}
}

func TestResolveAutoMultipleDisables(t *testing.T) {
	withList(t, []Interface{
		{Name: "eth0", IPv4: "192.168.1.10"},
		{Name: "wlan0", IPv4: "10.0.0.5"},
	}, nil)
	ip, status, err := Resolve("")
	if err != nil || ip != nil || status.Active || status.Message == "" {
		t.Fatalf("auto multi: ip=%v status=%+v err=%v", ip, status, err)
	}
}

func TestResolveNamed(t *testing.T) {
	withList(t, []Interface{
		{Name: "eth0", IPv4: "192.168.1.10"},
		{Name: "wlan0", IPv4: "10.0.0.5"},
	}, nil)
	ip, status, err := Resolve("wlan0")
	if err != nil || !status.Active || status.Name != "wlan0" {
		t.Fatalf("named: ip=%v status=%+v err=%v", ip, status, err)
	}
	if !ip.Equal(net.IPv4(10, 0, 0, 5)) {
		t.Fatalf("named ip=%v", ip)
	}
	if _, _, err := Resolve("missing"); err == nil {
		t.Fatal("missing interface accepted")
	}
}

func TestResolveListError(t *testing.T) {
	withList(t, nil, errors.New("boom"))
	if _, _, err := Resolve(Auto); err == nil {
		t.Fatal("expected list error")
	}
}
