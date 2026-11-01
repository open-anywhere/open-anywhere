package portal

import (
	"context"
	"net"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func virtualDevice(userID string) store.PortalDevice {
	return store.PortalDevice{
		ID:     "dev_" + userID,
		UserID: userID,
		Kind:   store.PortalDeviceVirtual,
		MAC:    "02:00:00:00:00:01",
		State:  store.PortalDeviceRegistered,
	}
}

// A tenant's virtual device must never cast from the host's LAN: the address
// is the operator's own network, so the tools would expose the operator's TVs.
func TestCastingFollowsTheAccountPolicy(t *testing.T) {
	local := net.ParseIP("192.0.2.10")
	manager := NewVoiceManager(context.Background(),
		WithOnSiteCasting(local),
		WithCastingFor(func(userID string) bool { return userID == "usr_admin" }))

	if got := manager.castingIPFor(virtualDevice("usr_admin")); !got.Equal(local) {
		t.Fatalf("admin casting address = %v, want %v", got, local)
	}
	if got := manager.castingIPFor(virtualDevice("usr_tenant")); got != nil {
		t.Fatalf("tenant casting address = %v, want nil", got)
	}
}

// A single-tenant portal has no predicate: the only account may cast.
func TestCastingWithoutPolicyAppliesToEveryDevice(t *testing.T) {
	local := net.ParseIP("192.0.2.10")
	manager := NewVoiceManager(context.Background(), WithOnSiteCasting(local))

	if got := manager.castingIPFor(virtualDevice("usr_any")); !got.Equal(local) {
		t.Fatalf("casting address = %v, want %v", got, local)
	}
}

// A deployment with casting turned off keeps the tools off even for accounts
// the policy would allow.
func TestCastingOffOverridesThePolicy(t *testing.T) {
	manager := NewVoiceManager(context.Background(),
		WithOnSiteCasting(nil),
		WithCastingFor(func(string) bool { return true }))

	if got := manager.castingIPFor(virtualDevice("usr_admin")); got != nil {
		t.Fatalf("casting address = %v, want nil", got)
	}
}

// Turning casting off at runtime reaches devices started afterwards.
func TestSetOnSiteCastingAppliesToLaterStarts(t *testing.T) {
	local := net.ParseIP("192.0.2.10")
	manager := NewVoiceManager(context.Background(), WithOnSiteCasting(local))

	if got := manager.castingIPFor(virtualDevice("usr_admin")); got == nil {
		t.Fatal("casting address is nil before the change")
	}
	manager.SetOnSiteCasting(nil)
	if got := manager.castingIPFor(virtualDevice("usr_admin")); got != nil {
		t.Fatalf("casting address = %v after turning casting off, want nil", got)
	}
}
