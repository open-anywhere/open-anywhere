package casting

import (
	"context"
	"errors"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/casting/dlna"
	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

type fakeDiscovery struct {
	devices []dlna.Device
	err     error
	scans   int
}

func (f *fakeDiscovery) Scan(context.Context) ([]dlna.Device, error) {
	f.scans++
	return f.devices, f.err
}

type fakeControl struct {
	started  dlna.Device
	source   api.StreamSource
	stopped  dlna.Device
	startErr error
	stopErr  error
}

func (f *fakeControl) Start(_ context.Context, device dlna.Device, source api.StreamSource) error {
	f.started, f.source = device, source
	return f.startErr
}

func (f *fakeControl) Stop(_ context.Context, device dlna.Device) error {
	f.stopped = device
	return f.stopErr
}

func newTestCaster(discovery discovery, control control) *siteCaster {
	return &siteCaster{
		discovery: discovery,
		control:   control,
		devices:   map[string]dlna.Device{},
		targets:   map[string]api.Target{},
	}
}

func device(id, name string) dlna.Device {
	return dlna.Device{
		Target:     renderer(id, name),
		ControlURL: "http://" + id + "/control",
	}
}

func TestListSortsScreensAndCachesDevices(t *testing.T) {
	discovery := &fakeDiscovery{devices: []dlna.Device{device("2", "书房"), device("1", "客厅")}}
	caster := newTestCaster(discovery, &fakeControl{})
	targets, err := caster.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(targets) != 2 || targets[0].Name != "书房" || targets[1].Name != "客厅" {
		// Names sort by UTF-8 bytes, which is stable even without a collation
		// table; the list only has to be deterministic between calls.
		t.Fatalf("targets=%+v", targets)
	}
	if _, ok := caster.device("2"); !ok {
		t.Fatal("device was not cached")
	}
}

func TestListKeepsPartialResults(t *testing.T) {
	discovery := &fakeDiscovery{devices: []dlna.Device{device("1", "客厅")}, err: errors.New("one description failed")}
	caster := newTestCaster(discovery, &fakeControl{})
	targets, err := caster.List(context.Background())
	if err != nil {
		t.Fatalf("partial results should not fail the scan: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets=%+v", targets)
	}
}

func TestResolveSearchesAgainWhenTheCacheMisses(t *testing.T) {
	discovery := &fakeDiscovery{devices: []dlna.Device{device("uuid-1", "客厅电视")}}
	caster := newTestCaster(discovery, &fakeControl{})
	target, err := caster.Resolve(context.Background(), "客厅电视")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target.ID != "uuid-1" || discovery.scans != 1 {
		t.Fatalf("target=%+v scans=%d", target, discovery.scans)
	}
}

func TestResolveRejectsAmbiguousNames(t *testing.T) {
	discovery := &fakeDiscovery{devices: []dlna.Device{device("1", "电视"), device("2", "电视")}}
	caster := newTestCaster(discovery, &fakeControl{})
	if _, err := caster.Resolve(context.Background(), "电视"); !errors.Is(err, ErrAmbiguousName) {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveReportsMissingScreen(t *testing.T) {
	discovery := &fakeDiscovery{}
	caster := newTestCaster(discovery, &fakeControl{})
	if _, err := caster.Resolve(context.Background(), "客厅电视"); !errors.Is(err, ErrScreenNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestCastStartsTheStreamAndRemembersTheScreen(t *testing.T) {
	discovery := &fakeDiscovery{devices: []dlna.Device{device("uuid-1", "客厅电视")}}
	control := &fakeControl{}
	caster := newTestCaster(discovery, control)
	target, err := caster.Cast(context.Background(), "uuid-1", "http://stream/live.ts")
	if err != nil {
		t.Fatalf("cast: %v", err)
	}
	if target.ID != "uuid-1" || control.started.Target.ID != "uuid-1" {
		t.Fatalf("target=%+v started=%+v", target, control.started)
	}
	if control.source.Format != streamFormat || control.source.VideoCodec != videoCodec || control.source.AudioCodec != audioCodec {
		t.Fatalf("source=%+v", control.source)
	}
	if err := caster.Stop(context.Background(), ""); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if control.stopped.Target.ID != "uuid-1" {
		t.Fatalf("stopped=%+v", control.stopped)
	}
}

func TestStopWithoutCastIsANoOp(t *testing.T) {
	caster := newTestCaster(&fakeDiscovery{}, &fakeControl{})
	if err := caster.Stop(context.Background(), ""); !errors.Is(err, ErrNotCasting) {
		t.Fatalf("err=%v", err)
	}
	if err := caster.Stop(context.Background(), "uuid-1"); err != nil {
		t.Fatalf("stop after restart should be a no-op: %v", err)
	}
}

func TestStopRejectsAnotherScreen(t *testing.T) {
	discovery := &fakeDiscovery{devices: []dlna.Device{device("uuid-1", "客厅电视"), device("uuid-2", "书房电视")}}
	control := &fakeControl{}
	caster := newTestCaster(discovery, control)
	if _, err := caster.Cast(context.Background(), "uuid-1", "http://stream"); err != nil {
		t.Fatalf("cast: %v", err)
	}
	if err := caster.Stop(context.Background(), "uuid-2"); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("err=%v", err)
	}
	if control.stopped.Target.ID != "" {
		t.Fatal("the other screen was stopped")
	}
}

func TestStopTreatsUPnP701AsAlreadyStopped(t *testing.T) {
	discovery := &fakeDiscovery{devices: []dlna.Device{device("uuid-1", "客厅电视")}}
	control := &fakeControl{stopErr: errors.New("Stop: UPnP 701 Transition not available")}
	caster := newTestCaster(discovery, control)
	if _, err := caster.Cast(context.Background(), "uuid-1", "http://stream"); err != nil {
		t.Fatalf("cast: %v", err)
	}
	if err := caster.Stop(context.Background(), ""); err != nil {
		t.Fatalf("701 should be success: %v", err)
	}
	if err := caster.Stop(context.Background(), ""); !errors.Is(err, ErrNotCasting) {
		t.Fatalf("stop was not forgotten: %v", err)
	}
}

func TestUPnPCode(t *testing.T) {
	for input, want := range map[string]string{
		"SetAVTransportURI: UPnP 715 Resource not available": "715",
		"Stop: UPnP 701 Transition not available":            "701",
		"dial tcp: connection refused":                       "",
	} {
		if got := upnpCode(errors.New(input)); got != want {
			t.Errorf("%q: got %q want %q", input, got, want)
		}
	}
	if upnpCode(nil) != "" {
		t.Error("nil error should have no code")
	}
}
