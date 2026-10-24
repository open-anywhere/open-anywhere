// Package agentapi implements a single-turn app build using AgentAPI sessions and SSE events.
package agentapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/gateway/internal/artifact"
)

type Config struct {
	BaseURL       string `json:"base_url"`
	AgentID       string `json:"agent_id"`
	AgentVersion  int    `json:"agent_version"`
	EnvironmentID string `json:"environment_id"`
}
type Client struct {
	Config     Config
	Key        string
	HTTP       *http.Client
	Store      artifact.Store
	FileDelays []time.Duration
}

func New(config Config, key string, store artifact.Store) (*Client, error) {
	u, err := url.Parse(config.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid AgentAPI base URL")
	}
	if key == "" || config.AgentID == "" || config.AgentVersion < 1 || config.EnvironmentID == "" {
		return nil, fmt.Errorf("AgentAPI key, agent/version and environment required")
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	return &Client{config, key, &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, store, []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second}}, nil
}
func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Config.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("Accept", "application/json")
	if strings.HasSuffix(path, "/stream") {
		req.Header.Set("Accept", "text/event-stream")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AgentAPI transport failed")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		return nil, fmt.Errorf("AgentAPI HTTP %d", res.StatusCode)
	}
	return res, nil
}
func (c *Client) json(ctx context.Context, method, path string, body, out any) error {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := c.request(bounded, method, path, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return fmt.Errorf("invalid AgentAPI response")
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
func (c *Client) Create(ctx context.Context, r application.BuildRequest) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	err := c.json(ctx, "POST", "/v1/sessions", map[string]any{"agent": map[string]any{"type": "agent", "id": c.Config.AgentID, "version": c.Config.AgentVersion}, "environment_id": c.Config.EnvironmentID, "title": r.AppID, "metadata": map[string]string{"app_id": r.AppID}}, &out)
	if err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("AgentAPI returned no session id")
	}
	return out.ID, nil
}
func prompt(r application.BuildRequest) string {
	names, _ := json.Marshal(r.Players)
	return "需求：" + r.Prompt + "\n初始玩家 player_names：" + string(names) + "\n使用 app-builder 技能。仅交付 /mnt/session/outputs/artifact.tar.gz，归档根包含 app/、openapi.yaml、launch.json。app/app.js 是自包含 Node.js 入口，app/public/index.html 是页面；读取 PORT，提供 GET /health、GET / 和 /api/*，业务操作用 OpenAPI 3.1 描述，响应包含 message。launch.json 必须使用以下完整字段格式：{\"name\":\"counter\",\"runtimeExecutable\":\"node\",\"runtimeArgs\":[\"app/app.js\"],\"healthPath\":\"/health\",\"readyTimeoutMs\":10000}。name 可按应用修改，禁止用 command/args/cwd 替代上述字段，禁止 port 字段和固定端口参数。以 user_name 作为本局玩家唯一身份和展示名称，去空白拒绝重名，不生成 user_id，初始玩家未准备好。不要编造未知玩家。禁止外部服务/CDN/数据库，预打包依赖，运行时不 install/build。完成自测后打包。"
}
func (c *Client) Run(ctx context.Context, sid string, r application.BuildRequest, submitted bool, cursor application.BuildCursor, save func(bool, *application.BuildEvent) error) (product application.BuildProduct, err error) {
	path := "/v1/sessions/" + url.PathEscape(sid)
	if cursor.Completed {
		return c.collect(ctx, sid)
	}
	// Resume only a definitely submitted turn. An ambiguous submission must never be replayed.
	if submitted {
		var snapshot struct {
			Status string `json:"status"`
		}
		if e := c.json(ctx, "GET", path, nil, &snapshot); e != nil {
			return product, e
		}
		if snapshot.Status == "terminated" {
			return product, fmt.Errorf("build session terminated")
		}
	}
	defer func() {
		if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			clean, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = c.json(clean, "POST", path+"/events", map[string]any{"events": []any{map[string]string{"type": "user.interrupt"}}}, nil)
		}
	}()
	seen := map[string]bool{}
	for id, v := range cursor.Seen {
		seen[id] = v
	}
	turnSeen := cursor.TurnSeen
	delay := time.Second
	for {
		streamCtx, streamCancel := context.WithCancel(ctx)
		res, e := c.request(streamCtx, "GET", path+"/events/stream", nil)
		if e != nil {
			streamCancel()
			if !submitted {
				return product, e
			}
			if e = wait(ctx, delay); e != nil {
				return product, e
			}
			delay = grow(delay)
			continue
		}
		if !submitted {
			// Persist submission intent before POST. Recovery replays events but never
			// submits again when a network response or a local acknowledgement was lost.
			if e = save(true, nil); e != nil {
				res.Body.Close()
				streamCancel()
				return product, e
			}
			e = c.json(ctx, "POST", path+"/events", map[string]any{"events": []any{map[string]any{"type": "user.message", "input": map[string]any{"parts": []any{map[string]string{"type": "text", "text": prompt(r)}}}}}}, nil)
			if e != nil {
				res.Body.Close()
				streamCancel()
				return product, fmt.Errorf("build submission uncertain; not retrying")
			}
			submitted = true
			if e = save(true, nil); e != nil {
				res.Body.Close()
				streamCancel()
				return product, e
			}
		}
		done, e := consume(res.Body, func(event event) (bool, error) {
			if event.ID != "" && seen[event.ID] {
				return false, nil
			}
			if event.Type == "user.message" || event.Type == "session.status_running" || event.Type == "agent.message" {
				turnSeen = true
			}
			update := application.BuildEvent{ID: event.ID, Type: event.Type, StopReason: event.StopReason.Type}
			if event.Type == "agent.message" {
				var texts []string
				for _, part := range event.Content {
					if part.Type == "text" {
						texts = append(texts, part.Text)
					}
				}
				text := strings.Join(strings.Fields(strings.Join(texts, " ")), " ")
				runes := []rune(text)
				if len(runes) > 200 {
					runes = runes[:200]
				}
				update.Message = string(runes)
			}
			if event.Type == "session.status_idle" && turnSeen {
				if event.StopReason.Type != "end_turn" {
					return false, fmt.Errorf("build stopped: %s", event.StopReason.Type)
				}
				if err := save(true, &update); err != nil {
					return false, err
				}
				return true, nil
			}
			if event.Type == "session.status_terminated" {
				return false, fmt.Errorf("build session terminated")
			}
			if err := save(true, &update); err != nil {
				return false, err
			}
			if event.ID != "" {
				seen[event.ID] = true
			}
			return false, nil
		})
		res.Body.Close()
		streamCancel()
		if done {
			break
		}
		if e != nil && !errors.Is(e, io.EOF) {
			return product, e
		}
		if e = wait(ctx, delay); e != nil {
			return product, e
		}
		delay = grow(delay)
	}
	product, err = c.collect(ctx, sid)
	if err == nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.json(cleanup, "POST", path+"/archive", map[string]any{}, nil)
	}
	return product, err
}

type event struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason struct {
		Type string `json:"type"`
	} `json:"stop_reason"`
}

func consume(reader io.Reader, fn func(event) (bool, error)) (bool, error) {
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 4096), 1<<20)
	var data []string
	size := 0
	dispatch := func() (bool, error) {
		if len(data) == 0 {
			return false, nil
		}
		var e event
		if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &e); err != nil {
			return false, fmt.Errorf("invalid AgentAPI event")
		}
		data = nil
		size = 0
		return fn(e)
	}
	for scan.Scan() {
		line := strings.TrimSuffix(scan.Text(), "\r")
		if line == "" {
			done, err := dispatch()
			if done || err != nil {
				return done, err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			part := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			size += len(part)
			if size > 1<<20 {
				return false, fmt.Errorf("SSE event too large")
			}
			data = append(data, part)
		}
	}
	if scan.Err() != nil {
		return false, io.EOF
	}
	done, err := dispatch()
	if done || err != nil {
		return done, err
	}
	return false, io.EOF
}
func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func grow(d time.Duration) time.Duration {
	d *= 2
	if d > 15*time.Second {
		return 15 * time.Second
	}
	return d
}

type fileRecord struct {
	ID           string    `json:"id"`
	Filename     string    `json:"filename"`
	Downloadable bool      `json:"downloadable"`
	Size         int64     `json:"size_bytes"`
	Created      time.Time `json:"created_at"`
}

func (c *Client) collect(ctx context.Context, sid string) (application.BuildProduct, error) {
	var last error
	delays := append([]time.Duration{0}, c.FileDelays...)
	for _, delay := range delays {
		if err := wait(ctx, delay); err != nil {
			return application.BuildProduct{}, err
		}
		files := []fileRecord{}
		page := ""
		visited := map[string]bool{}
		for {
			query := url.Values{"scope_id": []string{sid}}
			if page != "" {
				query.Set("page", page)
			}
			var response struct {
				Data []fileRecord `json:"data"`
				Next string       `json:"next_page"`
				More bool         `json:"has_more"`
			}
			if err := c.json(ctx, "GET", "/v1/files?"+query.Encode(), nil, &response); err != nil {
				return application.BuildProduct{}, err
			}
			for _, file := range response.Data {
				if file.Filename == "artifact.tar.gz" && file.Downloadable {
					files = append(files, file)
				}
			}
			if response.Next == "" {
				if response.More {
					return application.BuildProduct{}, fmt.Errorf("files pagination incomplete")
				}
				break
			}
			if visited[response.Next] || len(visited) > 1000 {
				return application.BuildProduct{}, fmt.Errorf("files pagination loop")
			}
			visited[response.Next] = true
			page = response.Next
		}
		if len(files) == 0 {
			last = fmt.Errorf("OUTPUT_MISSING")
			continue
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Created.After(files[j].Created) })
		f := files[0]
		if f.Size < 1 || f.Size > artifact.MaxCompressed {
			return application.BuildProduct{}, fmt.Errorf("ARTIFACT_TOO_LARGE or invalid size")
		}
		dl, cancel := context.WithTimeout(ctx, 5*time.Minute)
		res, err := c.request(dl, "GET", "/v1/files/"+url.PathEscape(f.ID)+"/content", nil)
		if err != nil {
			cancel()
			last = err
			continue
		}
		product, err := c.Store.Save(res.Body, f.Size)
		res.Body.Close()
		cancel()
		if err != nil {
			return product, err
		}
		return product, nil
	}
	return application.BuildProduct{}, last
}
