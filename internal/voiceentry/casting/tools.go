package casting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// toolsVersion is the media profile the device asks the casting service for.
// The cloud's stream is produced in exactly this profile, so no negotiation is
// needed; an endpoint that does not support it cannot show the stream.
const (
	streamFormat = "mpegts"
	videoCodec   = "h264"
	audioCodec   = "aac"
)

// Tools reports the device's casting tools to Linx.
//
// They cover only what happens on site: discover a screen, put a given stream
// address on it, stop it. Building applications, starting them and talking to
// them stay in the control plane, which Linx calls directly.
type Tools struct {
	client  *Client
	publish func(string)

	mu      sync.Mutex
	targets map[string]api.Target // last discovery, for names and endpoints
	last    string                // session started most recently here
}

// NewTools builds the tool set. publish narrates long calls to the web console.
func NewTools(client *Client, publish func(string)) *Tools {
	if publish == nil {
		publish = func(string) {}
	}
	return &Tools{client: client, publish: publish}
}

// List returns the tools to register on a Linx session.
func (t *Tools) List() []linx.Tool {
	return []linx.Tool{
		{
			Name:        "self.cast.list_screens",
			Description: "查询附近可以投屏的电视及其状态。投屏前先调用本工具，拿到 target_id；用户问「附近有哪些电视」时也用它。",
			Call:        t.listScreens,
		},
		{
			Name: "self.cast.cast",
			Description: "把云端返回的 stream_url 投到指定的电视上播放。target_id 必须来自 self.cast.list_screens；" +
				"url 必须原样使用控制面 start-app 返回的地址，不要改写或拼接。电视正在播放别的内容时会被拒绝，不要擅自覆盖。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":       map[string]any{"type": "string", "description": "控制面返回的 stream_url，原样传入"},
					"target_id": map[string]any{"type": "string", "description": "list_screens 返回的 target_id"},
				},
				"required": []any{"url", "target_id"},
			},
			Call: t.cast,
		},
		{
			Name:        "self.cast.stop",
			Description: "停止投屏，电视回到空闲。不传 session_id 时停止本设备最近一次投上去的内容。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "投屏会话编号；不确定就留空"},
				},
			},
			Call: t.stop,
		},
	}
}

func (t *Tools) listScreens(_ map[string]any) (string, error) {
	targets, err := t.client.ListTargets(context.Background())
	if err != nil {
		return "", err
	}
	t.rememberTargets(targets)
	if len(targets) == 0 {
		return "附近没有发现可以投屏的电视", nil
	}
	parts := make([]string, 0, len(targets))
	for _, target := range targets {
		parts = append(parts, label(target)+statusWord(target))
	}
	return strings.Join(parts, "；"), nil
}

func (t *Tools) cast(args map[string]any) (string, error) {
	streamURL := strings.TrimSpace(asString(args["url"]))
	targetID := strings.TrimSpace(asString(args["target_id"]))
	if streamURL == "" {
		return "", fmt.Errorf("需要控制面返回的 stream_url")
	}
	if targetID == "" {
		return "", fmt.Errorf("需要目标屏幕，先调用 self.cast.list_screens")
	}
	ctx := context.Background()
	target, err := t.target(ctx, targetID)
	if err != nil {
		return "", err
	}
	endpointID, ok := compatibleEndpoint(target)
	if !ok {
		return "", fmt.Errorf("%s不支持这种画面格式，换一块屏试试", label(target))
	}
	t.publish(fmt.Sprintf("正在投屏到%s…", label(target)))
	session, err := t.client.CreateSession(ctx, api.CreateSessionRequest{
		TargetID:   target.ID,
		EndpointID: endpointID,
		Source: api.StreamSource{
			Type:       "stream",
			URL:        streamURL,
			Format:     streamFormat,
			VideoCodec: videoCodec,
			AudioCodec: audioCodec,
		},
	})
	if err != nil {
		return "", readableError(err, target)
	}
	t.rememberSession(session.ID)
	if session.State == api.SessionFailed && session.Error != nil {
		return "", fmt.Errorf("%s播放失败：%s", label(target), session.Error.Message)
	}
	if session.State == api.SessionPlaying {
		return fmt.Sprintf("已经投到%s了", label(target)), nil
	}
	return fmt.Sprintf("已经开始投到%s，电视正在连接", label(target)), nil
}

func (t *Tools) stop(args map[string]any) (string, error) {
	sessionID := strings.TrimSpace(asString(args["session_id"]))
	if sessionID == "" {
		t.mu.Lock()
		sessionID = t.last
		t.mu.Unlock()
	}
	if sessionID == "" {
		return "", fmt.Errorf("不知道要停哪一次投屏，先告诉我停哪块电视")
	}
	_, err := t.client.StopSession(context.Background(), sessionID)
	if err != nil {
		var failure *APIError
		if errors.As(err, &failure) && failure.Code == CodeNotFound {
			return "这块电视已经不在投屏了", nil
		}
		return "", readableError(err, api.Target{})
	}
	t.forgetSession(sessionID)
	return "已经停止投屏", nil
}

// target returns the freshest view of a screen: the last discovery result when
// it has endpoints, otherwise a direct lookup so a cast works without a prior
// list_screens call.
func (t *Tools) target(ctx context.Context, targetID string) (api.Target, error) {
	t.mu.Lock()
	target, ok := t.targets[targetID]
	byName, nameHits := byNameLocked(t.targets, targetID)
	t.mu.Unlock()
	if ok && len(target.Endpoints) > 0 {
		return target, nil
	}
	// The model often repeats the name it just heard instead of the id that
	// list_screens returned. A unique cached name is good enough to cast.
	if nameHits == 1 && len(byName.Endpoints) > 0 {
		return byName, nil
	}
	if nameHits > 1 {
		return api.Target{}, fmt.Errorf("附近有好几块叫这个名字的电视，先调用 self.cast.list_screens 确认是哪一块")
	}
	target, err := t.client.GetTarget(ctx, targetID)
	if err != nil {
		var failure *APIError
		if errors.As(err, &failure) && failure.Code == CodeNotFound {
			return api.Target{}, fmt.Errorf("没有找到这块电视，先调用 self.cast.list_screens 重新查询")
		}
		return api.Target{}, err
	}
	t.rememberTargets([]api.Target{target})
	return target, nil
}

// byNameLocked counts cached screens whose name matches want exactly.
func byNameLocked(targets map[string]api.Target, want string) (api.Target, int) {
	var match api.Target
	hits := 0
	for _, target := range targets {
		if strings.TrimSpace(target.Name) != want {
			continue
		}
		match = target
		hits++
	}
	return match, hits
}

func compatibleEndpoint(target api.Target) (string, bool) {
	for _, endpoint := range target.Endpoints {
		capabilities := endpoint.Capabilities
		if capabilities.Streaming &&
			contains(capabilities.Formats, streamFormat) &&
			contains(capabilities.VideoCodecs, videoCodec) &&
			contains(capabilities.AudioCodecs, audioCodec) {
			return endpoint.ID, true
		}
	}
	return "", false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// readableError turns a casting service failure into something the model can
// say out loud.
func readableError(err error, target api.Target) error {
	var failure *APIError
	if !errors.As(err, &failure) {
		return err
	}
	name := label(target)
	switch failure.Code {
	case CodeTargetBusy:
		return fmt.Errorf("%s正在播放别的内容，先停掉再投", name)
	case CodeTargetOffline:
		return fmt.Errorf("%s现在离线，连不上", name)
	case CodeUnsupportedMedia:
		return fmt.Errorf("%s不支持这种画面格式", name)
	case CodeDeviceControlFailure:
		return fmt.Errorf("%s没有响应投屏指令", name)
	}
	return err
}

func (t *Tools) rememberTargets(targets []api.Target) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.targets == nil {
		t.targets = map[string]api.Target{}
	}
	for _, target := range targets {
		t.targets[target.ID] = target
	}
}

func (t *Tools) rememberSession(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.last = id
}

func (t *Tools) forgetSession(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == id {
		t.last = ""
	}
}

// label is the name a person would use for the screen. The target id is a
// discovery UUID, so it is only a fallback and never read out in full.
func label(target api.Target) string {
	if strings.TrimSpace(target.Name) != "" {
		return target.Name
	}
	if len(target.ID) > 6 {
		return "电视（" + target.ID[len(target.ID)-6:] + "）"
	}
	return "电视（" + target.ID + "）"
}

func statusWord(target api.Target) string {
	switch {
	case target.State == api.TargetOffline:
		return "离线"
	case target.ActiveSessionID != "":
		return "正在播放"
	default:
		return "空闲"
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
