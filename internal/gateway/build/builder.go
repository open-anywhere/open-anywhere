// Package build orchestrates AgentAPI sessions and turns their artifacts into
// registered applications.
package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
	"github.com/open-anywhere/open-anywhere/pkg/agentapi"
	"github.com/open-anywhere/open-anywhere/pkg/canvas"
)

// Config is the deployment configuration for builds.
type Config struct {
	AgentID        string
	AgentVersion   int // 0 means "use the agent's latest version"
	EnvironmentID  string
	BuildTimeout   time.Duration
	InterruptGrace time.Duration
	ArtifactLimit  int64
}

// Builder starts build sessions and watches them to completion.
type Builder struct {
	client   *agentapi.Client
	store    *artifact.Store
	registry *app.Registry
	notifier app.Notifier
	cfg      Config

	now     func() time.Time
	sleepFn func(context.Context, time.Duration) bool
	logf    func(string, ...any)
}

// New creates a builder.
func New(client *agentapi.Client, store *artifact.Store, registry *app.Registry, notifier app.Notifier, cfg Config) *Builder {
	if cfg.BuildTimeout <= 0 {
		cfg.BuildTimeout = 60 * time.Minute
	}
	if cfg.InterruptGrace <= 0 {
		cfg.InterruptGrace = 5 * time.Minute
	}
	if cfg.ArtifactLimit <= 0 {
		cfg.ArtifactLimit = artifact.MaxSize
	}
	return &Builder{
		client:   client,
		store:    store,
		registry: registry,
		notifier: notifier,
		cfg:      cfg,
		now:      func() time.Time { return time.Now().UTC() },
		logf:     log.Printf,
	}
}

// Start creates the AgentAPI session, submits the build prompt, and begins
// watching in the background. It returns once the task has been accepted.
func (b *Builder) Start(ctx context.Context, value app.App) error {
	if b.cfg.AgentID == "" || b.cfg.EnvironmentID == "" {
		return fmt.Errorf("build: agent and environment are not configured")
	}
	session, err := b.client.CreateSession(ctx, b.cfg.AgentID, b.cfg.AgentVersion, b.cfg.EnvironmentID, value.ID, map[string]string{"app_id": value.ID})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	started := b.now()
	if entry, ok := b.registry.Get(value.ID); ok {
		if err := entry.Do(func(a *app.App) error {
			a.SessionID = session.ID
			a.BuildStartedAt = started
			return nil
		}); err != nil {
			b.archive(session.ID)
			return fmt.Errorf("persist build session: %w", err)
		}
	} else {
		b.archive(session.ID)
		return fmt.Errorf("application record disappeared before build submission")
	}
	if err := b.client.SubmitMessage(ctx, session.ID, BuildPrompt(value.Prompt, value.PlayerNames)); err != nil {
		status := b.sessionStatus(session.ID)
		if status == "running" || status == "rescheduling" {
			b.logf("build %s: submit returned %v but session is %s; continuing", value.ID, err, status)
			go b.watch(value.ID, session.ID)
			return nil
		}
		b.archive(session.ID)
		return fmt.Errorf("submit build task: %w", err)
	}
	go b.watch(value.ID, session.ID)
	return nil
}

// Resume restarts watching an in-flight build after a control-plane restart.
func (b *Builder) Resume(value app.App) {
	if value.SessionID == "" {
		b.fail(value.ID, "无法恢复构建", fmt.Errorf("missing session id"))
		return
	}
	go b.watch(value.ID, value.SessionID)
}

func (b *Builder) sessionStatus(sessionID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := b.client.GetSession(ctx, sessionID)
	if err != nil {
		return ""
	}
	return session.Status
}

func (b *Builder) watch(appID, sessionID string) {
	deadline := b.now().Add(b.cfg.BuildTimeout)
	if record, ok := b.registry.Get(appID); ok && !record.Snapshot().BuildStartedAt.IsZero() {
		deadline = record.Snapshot().BuildStartedAt.Add(b.cfg.BuildTimeout)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	seen := make(map[string]bool)
	active := false
	reconnects := 0
	for {
		if ctx.Err() != nil {
			b.timeout(appID, sessionID)
			return
		}
		stream := b.client.OpenEventStream(ctx, sessionID)
		received := false
		for {
			event, err := stream.Next()
			if err != nil {
				stream.Close()
				if ctx.Err() != nil {
					b.timeout(appID, sessionID)
					return
				}
				if !errors.Is(err, io.EOF) {
					b.logf("build %s: event stream read: %v", appID, err)
				}
				break
			}
			if !received {
				// Only a stream that actually delivered an event resets the
				// backoff; consecutive failed connections keep growing it.
				received = true
				reconnects = 0
			}
			if event.ID != "" {
				if seen[event.ID] {
					continue
				}
				seen[event.ID] = true
			}
			if entry, ok := b.registry.Get(appID); ok {
				if entry.Snapshot().State != app.StateBuilding {
					stream.Close()
					return
				}
				if event.ID != "" {
					entry.Update(func(a *app.App) {
						if a.State == app.StateBuilding {
							a.EventCursor = event.ID
						}
					})
				}
			}
			switch event.Type {
			case "session.status_running":
				active = true
			case "agent.message":
				active = true
				b.recordMessage(appID, event.Text())
			case "agent.thinking":
				active = true
				b.recordThinking(appID, event.Text())
			case "session.error":
				b.recordStatus(appID, event.MessageText())
			case "session.status_idle":
				if !active {
					continue
				}
				stream.Close()
				reason := ""
				if event.StopReason != nil {
					reason = event.StopReason.Type
				}
				if reason != "end_turn" {
					b.fail(appID, stopReasonMessage(reason), fmt.Errorf("stop_reason=%s", reason))
					b.archive(sessionID)
					return
				}
				b.complete(appID, sessionID)
				return
			case "session.status_terminated":
				stream.Close()
				b.fail(appID, "构建会话已终止", fmt.Errorf("session terminated"))
				b.archive(sessionID)
				return
			}
		}
		if !b.sleep(ctx, reconnectDelay(reconnects)) {
			b.timeout(appID, sessionID)
			return
		}
		reconnects++
	}
}

func (b *Builder) recordMessage(appID, raw string) {
	text := compact(raw)
	if text == "" {
		return
	}
	if entry, ok := b.registry.Get(appID); ok {
		entry.Update(func(a *app.App) {
			if a.State == app.StateBuilding {
				a.LastAgentMessage = text
				a.LastProgress = text
			}
		})
	}
}

// recordThinking keeps get-app-status informative while the agent is working
// through tool calls without emitting a formal message. Thinking text is
// best-effort: the provider may send an empty or redacted block.
func (b *Builder) recordThinking(appID, raw string) {
	text := progressText(raw)
	if text == "" {
		return
	}
	if entry, ok := b.registry.Get(appID); ok {
		entry.Update(func(a *app.App) {
			if a.State == app.StateBuilding {
				a.LastProgress = text
			}
		})
	}
}

func (b *Builder) recordStatus(appID, message string) {
	message = compact(message)
	if message == "" {
		return
	}
	if entry, ok := b.registry.Get(appID); ok {
		entry.Update(func(a *app.App) {
			if a.State == app.StateBuilding {
				a.LastStatusMessage = message
				a.LastProgress = message
			}
		})
	}
}

func (b *Builder) complete(appID, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	entry, ok := b.registry.Get(appID)
	if !ok {
		return
	}
	if entry.Snapshot().State != app.StateBuilding {
		b.archive(sessionID)
		return
	}
	path, size, digest, err := b.collect(ctx, sessionID)
	if err == nil {
		var result artifact.Result
		result, err = artifact.Validate(path)
		if err == nil {
			var item artifact.Item
			item, err = b.store.SaveFile(appID, path, size, digest)
			if err == nil {
				b.register(entry, result, item)
				os.Remove(path)
				// Keep a successful build session alive: AgentAPI pauses idle
				// sandboxes, so the cost is negligible, and the session stays
				// available for follow-up conversations.
				return
			}
		}
	}
	os.Remove(path)
	b.fail(appID, userMessage(err), err)
	b.archive(sessionID)
}

func (b *Builder) register(entry *app.Record, result artifact.Result, item artifact.Item) {
	finished := b.now()
	applied := false
	var snapshot app.App
	err := entry.Do(func(a *app.App) error {
		if a.State != app.StateBuilding {
			return nil
		}
		applied = true
		a.State = app.StateIdle
		a.Name = result.Launch.Name
		a.OpenAPI = result.OpenAPI
		a.Artifact = app.Artifact{
			Path:        item.Path,
			SHA256:      item.SHA256,
			Size:        item.Size,
			DownloadURL: item.DownloadURL,
			StoredAt:    item.StoredAt,
		}
		a.BuildFinishedAt = finished
		a.BuildDuration = finished.Sub(a.BuildStartedAt)
		a.Error = nil
		a.LastAgentMessage = ""
		a.LastStatusMessage = ""
		a.LastProgress = ""
		snapshot = *a
		return nil
	})
	if err != nil {
		b.logf("build %s persist ready failed: %v", snapshot.ID, err)
		return
	}
	if !applied {
		return
	}
	b.notifier.Notify(snapshot.Owner, app.Notification{
		AppID:   snapshot.ID,
		State:   app.StateIdle,
		Message: "APP 创建完成",
	})
}

func (b *Builder) fail(appID, message string, cause error) {
	entry, ok := b.registry.Get(appID)
	if !ok {
		return
	}
	applied := false
	var snapshot app.App
	entry.Update(func(a *app.App) {
		if a.State != app.StateBuilding {
			return
		}
		applied = true
		a.State = app.StateError
		a.BuildFinishedAt = b.now()
		a.Error = &app.Error{
			Stage:            app.StageBuilding,
			Code:             "BUILD_FAILED",
			Message:          message,
			LastAgentMessage: a.LastAgentMessage,
		}
		snapshot = *a
	})
	if !applied {
		return
	}
	b.logf("build %s failed: %s (%v)", appID, message, cause)
	b.notifier.Notify(snapshot.Owner, app.Notification{
		AppID:   appID,
		State:   app.StateError,
		Message: message,
		Error:   snapshot.Error,
	})
}

func (b *Builder) timeout(appID, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = b.client.Interrupt(ctx, sessionID)
	cancel()
	deadline := time.Now().Add(b.cfg.InterruptGrace)
	for time.Now().Before(deadline) {
		status := b.sessionStatus(sessionID)
		if status == "idle" || status == "terminated" || status == "" {
			break
		}
		if !b.sleep(context.Background(), 5*time.Second) {
			break
		}
	}
	b.fail(appID, "构建超时", context.DeadlineExceeded)
	b.archive(sessionID)
}

func (b *Builder) archive(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.client.ArchiveSession(ctx, sessionID); err != nil {
		b.logf("archive session %s: %v", sessionID, err)
	}
}

func (b *Builder) collect(ctx context.Context, sessionID string) (string, int64, string, error) {
	var chosen *agentapi.File
	var lastErr error
	for index, delay := range []time.Duration{0, 5 * time.Second, 15 * time.Second, 30 * time.Second} {
		if index > 0 && !b.sleep(ctx, delay) {
			return "", 0, "", ctx.Err()
		}
		files, err := b.client.ListFiles(ctx, sessionID)
		if err != nil {
			lastErr = err
			continue
		}
		for i := range files {
			file := files[i]
			if file.Filename != "artifact.tar.gz" || !file.Downloadable {
				continue
			}
			if chosen == nil || file.CreatedAt.After(chosen.CreatedAt) {
				copy := file
				chosen = &copy
			}
		}
		if chosen != nil {
			break
		}
	}
	if chosen == nil {
		if lastErr != nil {
			return "", 0, "", &stageError{Code: "OUTPUT_DOWNLOAD_FAILED", User: "构建产物下载失败", Cause: lastErr}
		}
		return "", 0, "", &stageError{Code: "OUTPUT_MISSING", User: "构建产物缺失"}
	}
	temp, err := os.CreateTemp("", "open-anywhere-artifact-*.tar.gz")
	if err != nil {
		return "", 0, "", err
	}
	name := temp.Name()
	hash := sha256.New()
	downloadCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	size, err := b.client.DownloadFile(downloadCtx, chosen.ID, io.MultiWriter(temp, hash), b.cfg.ArtifactLimit)
	cancel()
	temp.Close()
	if err != nil {
		os.Remove(name)
		if strings.Contains(err.Error(), "exceeds") {
			return "", 0, "", &stageError{Code: "ARTIFACT_TOO_LARGE", User: "构建产物超过大小限制", Cause: err}
		}
		return "", 0, "", &stageError{Code: "OUTPUT_DOWNLOAD_FAILED", User: "构建产物下载失败", Cause: err}
	}
	if chosen.SizeBytes > 0 && size != chosen.SizeBytes {
		os.Remove(name)
		return "", 0, "", &stageError{Code: "ARTIFACT_CORRUPT", User: "构建产物损坏", Cause: fmt.Errorf("size %d, expected %d", size, chosen.SizeBytes)}
	}
	return name, size, hex.EncodeToString(hash.Sum(nil)), nil
}

func (b *Builder) sleep(ctx context.Context, delay time.Duration) bool {
	if b.sleepFn != nil {
		return b.sleepFn(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type stageError struct {
	Code  string
	User  string
	Cause error
}

func (e *stageError) Error() string {
	if e.Cause != nil {
		return e.Code + ": " + e.Cause.Error()
	}
	return e.Code
}

func userMessage(err error) string {
	var staged *stageError
	if errors.As(err, &staged) {
		return staged.User
	}
	switch {
	case artifact.IsCode(err, "ARTIFACT_TOO_LARGE"):
		return "构建产物超过大小限制"
	case artifact.IsCode(err, "ARTIFACT_CORRUPT"):
		return "构建产物损坏"
	case artifact.IsCode(err, "INVALID_ARTIFACT"):
		return "构建产物校验失败"
	default:
		return "构建失败"
	}
}

func stopReasonMessage(reason string) string {
	switch reason {
	case "requires_action":
		return "构建需要人工处理"
	case "interrupted":
		return "构建被中断"
	case "budget_reached":
		return "构建超出执行预算"
	case "retries_exhausted":
		return "构建重试耗尽"
	default:
		return "构建未正常结束"
	}
}

func reconnectDelay(attempt int) time.Duration {
	delays := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 30 * time.Second}
	if attempt >= len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt]
}

func compact(text string) string {
	return truncateRunes(strings.Join(strings.Fields(text), " "), 200)
}

// progressText condenses agent thinking, which can run to thousands of runes,
// into a short status line: whitespace is collapsed and a long block is cut at
// its first sentence when possible.
func progressText(text string) string {
	value := strings.Join(strings.Fields(text), " ")
	if value == "" || utf8.RuneCountInString(value) <= 200 {
		return value
	}
	for _, separator := range []string{"。", "！", "？", ". ", "! ", "? "} {
		index := strings.Index(value, separator)
		if index <= 0 {
			continue
		}
		candidate := value[:index+len(separator)]
		if utf8.RuneCountInString(candidate) >= 8 {
			value = candidate
			break
		}
	}
	return truncateRunes(value, 200)
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

// BuildPrompt combines the user's request and the initial player names into the
// short message sent to the build agent. The full contract lives in the agent's
// system prompt and the app-builder skill, keeping this message lightweight.
func BuildPrompt(requirement string, playerNames []string) string {
	prompt := "需求：" + strings.TrimSpace(requirement)
	if len(playerNames) > 0 {
		encoded, _ := json.Marshal(playerNames)
		prompt += "\n\n初始玩家（按加入顺序）：" + string(encoded)
	}
	prompt += "\n\n渲染画布：" + canvas.Description() + "。页面必须在画布内完整显示，不允许页面滚动；关键内容距边缘至少 5%。"
	return prompt + "\n\n请按系统提示词和 app-builder 技能完成构建。"
}
