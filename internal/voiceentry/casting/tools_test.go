package casting

import (
	"context"
	"errors"
	"strings"
	"testing"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// fakeCaster stands in for the LAN: tests decide what discovery returns and
// which failure the next control call reports.
type fakeCaster struct {
	targets    []api.Target
	listErr    error
	resolveErr error
	castErr    error
	stopErr    error

	resolvedRef string
	castTarget  string
	castURL     string
	stopped     string
	stoppedAll  bool
}

func (f *fakeCaster) List(context.Context) ([]api.Target, error) {
	return f.targets, f.listErr
}

func (f *fakeCaster) Resolve(_ context.Context, ref string) (api.Target, error) {
	f.resolvedRef = ref
	if f.resolveErr != nil {
		return api.Target{}, f.resolveErr
	}
	for _, target := range f.targets {
		if target.ID == ref || target.Name == ref {
			return target, nil
		}
	}
	return api.Target{}, ErrScreenNotFound
}

func (f *fakeCaster) Cast(_ context.Context, targetID, streamURL string) (api.Target, error) {
	f.castTarget, f.castURL = targetID, streamURL
	if f.castErr != nil {
		return api.Target{}, f.castErr
	}
	for _, target := range f.targets {
		if target.ID == targetID {
			return target, nil
		}
	}
	return api.Target{}, ErrScreenNotFound
}

func (f *fakeCaster) Stop(_ context.Context, targetID string) error {
	f.stopped = targetID
	f.stoppedAll = targetID == ""
	return f.stopErr
}

func renderer(id, name string) api.Target {
	return api.Target{
		ID:    id,
		Name:  name,
		State: api.TargetOnline,
		Endpoints: []api.Endpoint{{
			ID:       "dlna-renderer",
			Protocol: "dlna",
			Capabilities: api.Capabilities{
				Formats:     []string{"mpegts"},
				VideoCodecs: []string{"h264"},
				AudioCodecs: []string{"aac"},
				Streaming:   true,
			},
		}},
	}
}

func call(t *testing.T, tools *Tools, name string, args map[string]any) (string, error) {
	t.Helper()
	for _, tool := range tools.List() {
		if tool.Name == name {
			return tool.Call(args)
		}
	}
	t.Fatalf("tool %s is not registered", name)
	return "", nil
}

func TestListScreensReportsEmptyNetwork(t *testing.T) {
	tools := NewTools(&fakeCaster{}, nil)
	text, err := call(t, tools, "self.cast.list_screens", nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if text != "附近没有发现可以投屏的电视" {
		t.Fatalf("text=%q", text)
	}
}

func TestListScreensSpeaksNames(t *testing.T) {
	fake := &fakeCaster{targets: []api.Target{renderer("uuid-1", "客厅电视"), renderer("uuid-2", "书房电视")}}
	tools := NewTools(fake, nil)
	text, err := call(t, tools, "self.cast.list_screens", nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if text != "客厅电视空闲；书房电视空闲" {
		t.Fatalf("text=%q", text)
	}
}

func TestListScreensPropagatesDiscoveryFailure(t *testing.T) {
	tools := NewTools(&fakeCaster{listErr: errors.New("multicast unavailable")}, nil)
	if _, err := call(t, tools, "self.cast.list_screens", nil); err == nil {
		t.Fatal("discovery failure was swallowed")
	}
}

func TestCastRequiresURLAndTarget(t *testing.T) {
	tools := NewTools(&fakeCaster{}, nil)
	if _, err := call(t, tools, "self.cast.cast", map[string]any{"target_id": "x"}); err == nil {
		t.Fatal("cast accepted an empty url")
	}
	if _, err := call(t, tools, "self.cast.cast", map[string]any{"url": "http://stream"}); err == nil {
		t.Fatal("cast accepted an empty target")
	}
}

func TestCastUsesResolvedScreenAndRemembersIt(t *testing.T) {
	fake := &fakeCaster{targets: []api.Target{renderer("uuid-1", "客厅电视")}}
	tools := NewTools(fake, nil)
	text, err := call(t, tools, "self.cast.cast", map[string]any{"url": "http://stream/live.ts", "target_id": "客厅电视"})
	if err != nil {
		t.Fatalf("cast: %v", err)
	}
	if text != "已经投到客厅电视了" {
		t.Fatalf("text=%q", text)
	}
	if fake.castTarget != "uuid-1" || fake.castURL != "http://stream/live.ts" {
		t.Fatalf("cast target=%q url=%q", fake.castTarget, fake.castURL)
	}
	if text, err := call(t, tools, "self.cast.stop", nil); err != nil || text != "已经停止投屏" {
		t.Fatalf("stop after cast: text=%q err=%v", text, err)
	}
	if fake.stopped != "uuid-1" {
		t.Fatalf("stopped=%q", fake.stopped)
	}
}

func TestCastRejectsIncompatibleScreen(t *testing.T) {
	target := renderer("uuid-1", "旧电视")
	target.Endpoints[0].Capabilities.Formats = []string{"hls"}
	tools := NewTools(&fakeCaster{targets: []api.Target{target}}, nil)
	_, err := call(t, tools, "self.cast.cast", map[string]any{"url": "http://stream", "target_id": "uuid-1"})
	if err == nil || !strings.Contains(err.Error(), "不支持这种画面格式") {
		t.Fatalf("err=%v", err)
	}
}

func TestCastSpeaksUPnPFailures(t *testing.T) {
	cases := map[string]string{
		"upnp 714": "不支持这种画面格式",
		"upnp 715": "正在播放别的内容",
		"upnp 716": "找不到这个视频地址",
		"upnp 718": "拒绝了投屏请求",
	}
	for raw, want := range cases {
		fake := &fakeCaster{
			targets: []api.Target{renderer("uuid-1", "客厅电视")},
			castErr: errors.New("SetAVTransportURI: UPnP " + strings.TrimPrefix(raw, "upnp ") + " fault"),
		}
		tools := NewTools(fake, nil)
		_, err := call(t, tools, "self.cast.cast", map[string]any{"url": "http://stream", "target_id": "uuid-1"})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err=%v, want %q", raw, err, want)
		}
	}
}

func TestCastSpeaksTransportFailure(t *testing.T) {
	fake := &fakeCaster{
		targets: []api.Target{renderer("uuid-1", "客厅电视")},
		castErr: errors.New("dial tcp: connection refused"),
	}
	tools := NewTools(fake, nil)
	_, err := call(t, tools, "self.cast.cast", map[string]any{"url": "http://stream", "target_id": "uuid-1"})
	if err == nil || !strings.Contains(err.Error(), "没有响应投屏指令") {
		t.Fatalf("err=%v", err)
	}
}

func TestStopWithoutSessionExplainsItself(t *testing.T) {
	tools := NewTools(&fakeCaster{}, nil)
	_, err := call(t, tools, "self.cast.stop", nil)
	if err == nil || !strings.Contains(err.Error(), "不知道要停哪一次投屏") {
		t.Fatalf("err=%v", err)
	}
}

func TestStopOnAnotherScreenIsNotAnError(t *testing.T) {
	fake := &fakeCaster{stopErr: ErrSessionGone}
	tools := NewTools(fake, nil)
	_, err := call(t, tools, "self.cast.stop", map[string]any{"session_id": "uuid-1"})
	if err == nil || !strings.Contains(err.Error(), "已经不在投屏了") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveFailuresBecomeSpeakable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{ErrScreenNotFound, "没有找到这块电视"},
		{ErrAmbiguousName, "有好几块叫这个名字的电视"},
	} {
		tools := NewTools(&fakeCaster{resolveErr: tc.err}, nil)
		_, err := call(t, tools, "self.cast.cast", map[string]any{"url": "http://stream", "target_id": "客厅"})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err=%v", tc.err, err)
		}
	}
}
