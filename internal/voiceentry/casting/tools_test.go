package casting

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

type recorded struct {
	Method string
	Path   string
	Body   []byte
}

// fakeCasting is a stand-in for the local casting service: it serves discovery,
// accepts projection sessions, and records what it was asked to do.
type fakeCasting struct {
	mu         sync.Mutex
	calls      []recorded
	targets    []api.Target
	session    api.Session
	create     int    // status override for POST; 0 means 201
	createCode string // error code the POST failure reports
	stop       int    // status override for DELETE; 0 means 200
	delay      time.Duration
	lastBody   api.CreateSessionRequest
}

func (f *fakeCasting) handler() http.Handler {
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /v1/targets", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, nil)
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		write(w, http.StatusOK, map[string]any{"targets": f.targets})
	})
	mux.HandleFunc("GET /v1/targets/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, nil)
		for _, target := range f.targets {
			if target.ID == r.PathValue("id") {
				write(w, http.StatusOK, target)
				return
			}
		}
		write(w, http.StatusNotFound, errorBody(CodeNotFound, "target not found"))
	})
	mux.HandleFunc("POST /v1/projection-sessions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.record(r, body)
		if f.create >= 300 {
			write(w, f.create, errorBody(f.createCode, "request refused"))
			return
		}
		var request api.CreateSessionRequest
		_ = json.Unmarshal(body, &request)
		f.mu.Lock()
		f.lastBody = request
		f.mu.Unlock()
		write(w, http.StatusCreated, f.session)
	})
	mux.HandleFunc("DELETE /v1/projection-sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r, nil)
		if f.stop >= 300 {
			write(w, f.stop, errorBody(CodeNotFound, "session not found"))
			return
		}
		write(w, http.StatusOK, api.Session{ID: r.PathValue("id"), State: api.SessionStopped})
	})
	return mux
}

func (f *fakeCasting) record(r *http.Request, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, recorded{Method: r.Method, Path: r.URL.Path, Body: body})
}

func (f *fakeCasting) snapshot() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.calls...)
}

func (f *fakeCasting) posted() api.CreateSessionRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastBody
}

func errorBody(code, message string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": message}}
}

func livingRoom() api.Target {
	return api.Target{
		ID: "uuid-living-room", Name: "客厅电视", State: api.TargetOnline,
		Endpoints: []api.Endpoint{{
			ID: "dlna", Protocol: "dlna",
			Capabilities: api.Capabilities{
				Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"},
				AudioCodecs: []string{"aac"}, Streaming: true,
			},
		}},
	}
}

func studyBusy() api.Target {
	target := livingRoom()
	target.ID, target.Name, target.ActiveSessionID = "uuid-study", "书房电视", "projection-other"
	return target
}

func bedroomOffline() api.Target {
	target := livingRoom()
	target.ID, target.Name, target.State = "uuid-bedroom", "卧室电视", api.TargetOffline
	return target
}

func newTestTools(t *testing.T, fake *fakeCasting, timeout time.Duration) (*Tools, *[]string) {
	t.Helper()
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	client := New(Config{
		URL:         server.URL,
		CallTimeout: timeout,
		ListTimeout: timeout,
		HTTPClient:  server.Client(),
	})
	var events []string
	return NewTools(client, func(text string) { events = append(events, text) }), &events
}

func TestToolsListNamesAndUniqueness(t *testing.T) {
	tools, _ := newTestTools(t, &fakeCasting{}, time.Second)
	list := tools.List()
	if len(list) != 3 {
		t.Fatalf("tools=%d", len(list))
	}
	seen := map[string]bool{}
	for _, tool := range list {
		if !strings.HasPrefix(tool.Name, "self.cast.") {
			t.Errorf("tool %q outside self.cast namespace", tool.Name)
		}
		if seen[tool.Name] {
			t.Errorf("duplicate %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Call == nil {
			t.Errorf("%s has no call", tool.Name)
		}
	}
	for _, want := range []string{"self.cast.list_screens", "self.cast.cast", "self.cast.stop"} {
		if !seen[want] {
			t.Errorf("missing %s", want)
		}
	}
}

func TestListScreensWordsTargets(t *testing.T) {
	fake := &fakeCasting{targets: []api.Target{livingRoom(), studyBusy(), bedroomOffline()}}
	tools, _ := newTestTools(t, fake, time.Second)
	text, err := tools.listScreens(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "客厅电视空闲；书房电视正在播放；卧室电视离线"
	if text != want {
		t.Fatalf("got %q want %q", text, want)
	}
}

func TestListScreensWithoutTargetsSaysSo(t *testing.T) {
	tools, _ := newTestTools(t, &fakeCasting{}, time.Second)
	text, err := tools.listScreens(nil)
	if err != nil || !strings.Contains(text, "没有发现") {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestCastPostsTheCloudStreamToTheChosenEndpoint(t *testing.T) {
	fake := &fakeCasting{
		targets: []api.Target{livingRoom(), studyBusy()},
		session: api.Session{ID: "projection-1", State: api.SessionPlaying, TargetID: "uuid-living-room"},
	}
	tools, events := newTestTools(t, fake, time.Second)
	if _, err := tools.listScreens(nil); err != nil {
		t.Fatal(err)
	}

	text, err := tools.cast(map[string]any{
		"url":       "https://render.example.com/streams/inst_1/live.ts",
		"target_id": "uuid-living-room",
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "已经投到客厅电视了" {
		t.Fatalf("text=%q", text)
	}
	if len(*events) != 1 || !strings.Contains((*events)[0], "客厅电视") {
		t.Fatalf("events=%v", *events)
	}

	request := fake.posted()
	if request.TargetID != "uuid-living-room" || request.EndpointID != "dlna" {
		t.Fatalf("request=%+v", request)
	}
	want := api.StreamSource{
		Type: "stream", URL: "https://render.example.com/streams/inst_1/live.ts",
		Format: "mpegts", VideoCodec: "h264", AudioCodec: "aac",
	}
	if request.Source != want {
		t.Fatalf("source=%+v want %+v", request.Source, want)
	}
	// The URL is passed through untouched: the device never rewrites it.
	calls := fake.snapshot()
	if last := calls[len(calls)-1]; last.Method != http.MethodPost || last.Path != "/v1/projection-sessions" {
		t.Fatalf("last call = %+v", last)
	}
}

func TestCastWithoutPriorListLooksUpTheTarget(t *testing.T) {
	fake := &fakeCasting{
		targets: []api.Target{livingRoom()},
		session: api.Session{ID: "projection-1", State: api.SessionPlaying},
	}
	tools, _ := newTestTools(t, fake, time.Second)
	if _, err := tools.cast(map[string]any{"url": "http://cloud/live.ts", "target_id": "uuid-living-room"}); err != nil {
		t.Fatal(err)
	}
	calls := fake.snapshot()
	if len(calls) != 2 || calls[0].Method != http.MethodGet || calls[0].Path != "/v1/targets/uuid-living-room" {
		t.Fatalf("calls=%+v", calls)
	}
	if fake.posted().EndpointID != "dlna" {
		t.Fatalf("endpoint=%q", fake.posted().EndpointID)
	}
}

func TestCastAcceptsTheScreenNameFromTheLastList(t *testing.T) {
	fake := &fakeCasting{
		targets: []api.Target{livingRoom(), studyBusy()},
		session: api.Session{ID: "projection-1", State: api.SessionPlaying},
	}
	tools, _ := newTestTools(t, fake, time.Second)
	if _, err := tools.listScreens(nil); err != nil {
		t.Fatal(err)
	}
	// list_screens speaks "客厅电视空闲"; the model repeats the name it heard.
	if _, err := tools.cast(map[string]any{"url": "http://cloud/live.ts", "target_id": "客厅电视"}); err != nil {
		t.Fatal(err)
	}
	if fake.posted().TargetID != "uuid-living-room" {
		t.Fatalf("target=%q", fake.posted().TargetID)
	}
	for _, call := range fake.snapshot() {
		if strings.HasPrefix(call.Path, "/v1/targets/") {
			t.Fatalf("looked the name up remotely: %+v", call)
		}
	}
}

func TestCastRejectsAnAmbiguousScreenName(t *testing.T) {
	twin := livingRoom()
	twin.ID = "uuid-living-room-2"
	fake := &fakeCasting{targets: []api.Target{livingRoom(), twin}}
	tools, _ := newTestTools(t, fake, time.Second)
	if _, err := tools.listScreens(nil); err != nil {
		t.Fatal(err)
	}
	_, err := tools.cast(map[string]any{"url": "http://cloud/live.ts", "target_id": "客厅电视"})
	if err == nil || !strings.Contains(err.Error(), "好几块") {
		t.Fatalf("err=%v", err)
	}
}

func TestCastRejectsAScreenWithoutTheStreamProfile(t *testing.T) {
	target := livingRoom()
	target.Endpoints[0].Capabilities.AudioCodecs = []string{"opus"}
	fake := &fakeCasting{targets: []api.Target{target}}
	tools, _ := newTestTools(t, fake, time.Second)
	_, err := tools.cast(map[string]any{"url": "http://cloud/live.ts", "target_id": "uuid-living-room"})
	if err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("err=%v", err)
	}
	for _, call := range fake.snapshot() {
		if call.Method == http.MethodPost {
			t.Fatalf("posted anyway: %+v", call)
		}
	}
}

func TestCastExplainsBusyAndOfflineScreens(t *testing.T) {
	for name, test := range map[string]struct {
		status int
		code   string
		want   string
	}{
		"busy":    {http.StatusConflict, CodeTargetBusy, "正在播放别的内容"},
		"offline": {http.StatusConflict, CodeTargetOffline, "离线"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeCasting{targets: []api.Target{livingRoom()}, create: test.status, createCode: test.code}
			tools, _ := newTestTools(t, fake, time.Second)
			_, err := tools.cast(map[string]any{"url": "http://cloud/live.ts", "target_id": "uuid-living-room"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestCastRejectsAnUnknownScreenBeforePosting(t *testing.T) {
	fake := &fakeCasting{}
	tools, _ := newTestTools(t, fake, time.Second)
	_, err := tools.cast(map[string]any{"url": "http://cloud/live.ts", "target_id": "made-up"})
	if err == nil || !strings.Contains(err.Error(), "先调用 self.cast.list_screens") {
		t.Fatalf("err=%v", err)
	}
	if calls := fake.snapshot(); len(calls) != 1 || calls[0].Method != http.MethodGet {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestCastNeedsBothArguments(t *testing.T) {
	tools, _ := newTestTools(t, &fakeCasting{}, time.Second)
	for _, args := range []map[string]any{
		{"target_id": "uuid-living-room"},
		{"url": "http://cloud/live.ts"},
	} {
		if _, err := tools.cast(args); err == nil {
			t.Fatalf("accepted %+v", args)
		}
	}
}

func TestStopUsesTheSessionJustStarted(t *testing.T) {
	fake := &fakeCasting{
		targets: []api.Target{livingRoom()},
		session: api.Session{ID: "projection-9", State: api.SessionPlaying},
	}
	tools, _ := newTestTools(t, fake, time.Second)
	if _, err := tools.cast(map[string]any{"url": "http://cloud/live.ts", "target_id": "uuid-living-room"}); err != nil {
		t.Fatal(err)
	}
	text, err := tools.stop(nil)
	if err != nil || text != "已经停止投屏" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	calls := fake.snapshot()
	last := calls[len(calls)-1]
	if last.Method != http.MethodDelete || last.Path != "/v1/projection-sessions/projection-9" {
		t.Fatalf("last call = %+v", last)
	}
	// A second stop has nothing left to stop.
	if _, err := tools.stop(nil); err == nil {
		t.Fatal("stopped again without a session")
	}
}

func TestStopOfAMissingSessionReadsAsDone(t *testing.T) {
	fake := &fakeCasting{stop: http.StatusNotFound}
	tools, _ := newTestTools(t, fake, time.Second)
	text, err := tools.stop(map[string]any{"session_id": "projection-gone"})
	if err != nil || !strings.Contains(text, "已经不在投屏") {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestUnconfiguredCastingURLFailsBeforeNetwork(t *testing.T) {
	tools := NewTools(New(Config{}), nil)
	if _, err := tools.listScreens(nil); err == nil || !strings.Contains(err.Error(), "未配置") {
		t.Fatalf("err=%v", err)
	}
}

func TestSlowCastingServiceTimesOutReadably(t *testing.T) {
	fake := &fakeCasting{targets: []api.Target{livingRoom()}, delay: 300 * time.Millisecond}
	tools, _ := newTestTools(t, fake, 30*time.Millisecond)
	_, err := tools.listScreens(nil)
	if err == nil || !strings.Contains(err.Error(), "没有响应") {
		t.Fatalf("err=%v", err)
	}
}
