package render

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/pkg/casting"
	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

type Config struct {
	WorkDir        string
	PublicURL      string
	AppHost        string
	Node           string
	Chrome         string
	FFmpeg         string
	Width          int
	Height         int
	FPS            int
	StartupTimeout time.Duration
	ServicePorts   []int
	Logger         *log.Logger

	// browser optionally overrides the browser/DevTools layer in tests.
	browser browserHooks
	// stopGroup optionally overrides process-group shutdown in tests.
	stopGroup func(*exec.Cmd, <-chan struct{}) error
}

type run struct {
	info          api.Instance
	request       api.CreateRequest
	ctx           context.Context
	cancel        context.CancelFunc
	started       chan struct{}
	done          chan struct{}
	hub           *hub
	status        int
	stopRequested bool
	cleaned       bool
	cleanup       func() error // accessed only after done closes
}

type Service struct {
	mu           sync.Mutex
	cfg          Config
	ctx          context.Context
	cancel       context.CancelFunc
	instances    map[string]*run
	apps         map[string]string
	ports        map[int]string
	servicePorts map[int]bool
	client       *http.Client
	randRead     func([]byte) (int, error)
	closed       bool
}

type Failure struct {
	HTTP       int
	Detail     api.Error
	AppID      string
	InstanceID string
}

func (e *Failure) Error() string { return e.Detail.Message }
func failure(status int, code, msg string) *Failure {
	return &Failure{HTTP: status, Detail: api.Error{Stage: "starting", Code: code, Message: msg}}
}

func New(cfg Config) (*Service, error) {
	if cfg.Node == "" {
		cfg.Node = "node"
	}
	if cfg.FFmpeg == "" {
		cfg.FFmpeg = "ffmpeg"
	}
	if cfg.Width == 0 {
		cfg.Width = 1280
	}
	if cfg.Height == 0 {
		cfg.Height = 720
	}
	if cfg.FPS == 0 {
		cfg.FPS = 10
	}
	if cfg.Width < 320 || cfg.Height < 180 || cfg.Width > 3840 || cfg.Height > 2160 || cfg.Width%2 != 0 || cfg.Height%2 != 0 || cfg.FPS < 1 || cfg.FPS > 30 {
		return nil, errors.New("invalid video dimensions or FPS")
	}
	if cfg.StartupTimeout == 0 {
		cfg.StartupTimeout = 60 * time.Second
	}
	if cfg.StartupTimeout < time.Second {
		return nil, errors.New("startup timeout must be at least one second")
	}
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("public URL must be an http origin")
	}
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	if cfg.AppHost == "" {
		cfg.AppHost = u.Hostname()
	}
	if net.ParseIP(cfg.AppHost) == nil && strings.ContainsAny(cfg.AppHost, "/: ") {
		return nil, errors.New("invalid app host")
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	if cfg.WorkDir == "" {
		return nil, errors.New("work directory is required")
	}
	cfg.WorkDir, err = filepath.Abs(cfg.WorkDir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(cfg.WorkDir, 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{cfg: cfg, ctx: ctx, cancel: cancel, instances: map[string]*run{}, apps: map[string]string{}, ports: map[int]string{}, servicePorts: map[int]bool{}, client: &http.Client{Timeout: 30 * time.Second}, randRead: rand.Read}
	for _, p := range cfg.ServicePorts {
		s.servicePorts[p] = true
	}
	return s, nil
}

func snapshot(r *run) api.Instance {
	v := r.info
	if v.Error != nil {
		e := *v.Error
		v.Error = &e
	}
	if v.Source != nil {
		src := *v.Source
		v.Source = &src
	}
	return v
}

func (s *Service) Create(ctx context.Context, req api.CreateRequest) (api.Instance, *Failure) {
	u, err := url.Parse(req.ArtifactURL)
	if strings.TrimSpace(req.AppID) == "" || len(req.AppID) > 256 || strings.ContainsAny(req.AppID, "\r\n\x00") || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || req.Port < 1 || req.Port > 65535 {
		return api.Instance{}, failure(400, "INVALID_REQUEST", "app_id, HTTP(S) artifact_url and port (1–65535) are required")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return api.Instance{}, failure(503, "RENDER_STOPPING", "Render is shutting down")
	}
	if id, ok := s.apps[req.AppID]; ok {
		v := snapshot(s.instances[id])
		s.mu.Unlock()
		e := failure(409, "APP_ALREADY_RUNNING", "app_id already has an active instance")
		e.AppID = req.AppID
		e.InstanceID = id
		return v, e
	}
	p := s.portLocked(req.Port)
	free := false
	if !p.Occupied {
		free, err = portAvailable(req.Port)
	}
	if err != nil {
		s.mu.Unlock()
		return api.Instance{}, failure(503, "PORT_INSPECTION_FAILED", err.Error())
	}
	if !free {
		s.mu.Unlock()
		return api.Instance{}, failure(409, "PORT_IN_USE", "requested port is occupied")
	}
	var idBytes [16]byte
	if _, err = s.randRead(idBytes[:]); err != nil {
		s.mu.Unlock()
		return api.Instance{}, failure(500, "APP_START_FAILED", err.Error())
	}
	id := "instance-" + hex.EncodeToString(idBytes[:])
	life, cancel := context.WithCancel(s.ctx)
	r := &run{info: api.Instance{AppID: req.AppID, InstanceID: id, State: "starting", Port: req.Port}, request: req, ctx: life, cancel: cancel, started: make(chan struct{}), done: make(chan struct{}), hub: newHub(), status: 502}
	s.instances[id] = r
	s.apps[req.AppID] = id
	s.ports[req.Port] = id
	s.mu.Unlock()
	go s.execute(r)
	select {
	case <-r.started:
	case <-ctx.Done():
		r.cancel()
		return api.Instance{}, &Failure{HTTP: 504, Detail: api.Error{Stage: "starting", Code: "START_TIMEOUT", Message: "startup request canceled"}, AppID: req.AppID, InstanceID: id}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := snapshot(r)
	if v.State == "ready" {
		return v, nil
	}
	e := v.Error
	if e == nil {
		e = &api.Error{Stage: "starting", Code: "APP_START_FAILED", Message: "instance stopped during startup"}
	}
	return v, &Failure{HTTP: r.status, Detail: *e, AppID: v.AppID, InstanceID: id}
}

func (s *Service) execute(r *run) {
	startup, stopStartup := context.WithTimeout(r.ctx, s.cfg.StartupTimeout)
	defer stopStartup()
	var cmd *exec.Cmd
	var logFile *os.File
	var video *media
	var dir string
	processDone := make(chan struct{})
	started := false
	var cleanupMu sync.Mutex
	cleanup := func() error {
		cleanupMu.Lock()
		defer cleanupMu.Unlock()
		r.cancel()
		r.hub.close()
		var errs []error
		if video != nil {
			if err := video.close(); err != nil {
				errs = append(errs, err)
			} else {
				video = nil
			}
		}
		if cmd != nil {
			if err := s.stopGroupProcess(cmd, processDone); err != nil {
				errs = append(errs, err)
			} else {
				cmd = nil
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		if logFile != nil {
			_ = logFile.Close()
			logFile = nil
		}
		if dir != "" {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
			dir = ""
		}
		return nil
	}
	r.cleanup = cleanup
	defer func() {
		err := cleanup()
		s.mu.Lock()
		if err != nil {
			r.info.State = "error"
			r.info.Error = &api.Error{Stage: "running", Code: "STOP_FAILED", Message: err.Error()}
		} else {
			r.cleaned = true
			s.releaseLocked(r)
			if r.stopRequested || r.info.State == "starting" {
				r.info.State = "stopped"
				r.info.Error = nil
			}
		}
		r.info.Source = nil
		r.info.AppBaseURL = ""
		s.mu.Unlock()
		if !started {
			close(r.started)
		}
		close(r.done)
	}()
	fail := func(status int, code string, err error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		stage := "starting"
		if r.info.State == "ready" {
			stage = "running"
		}
		if !started && startup.Err() != nil && !r.stopRequested {
			status = 504
			code = "START_TIMEOUT"
		}
		r.status = status
		r.info.State = "error"
		r.info.Source = nil
		r.info.AppBaseURL = ""
		r.info.Error = &api.Error{Stage: stage, Code: code, Message: err.Error()}
		s.cfg.Logger.Printf("app=%q instance=%s %s: %v", r.info.AppID, r.info.InstanceID, code, err)
	}
	var err error
	dir, err = os.MkdirTemp(s.cfg.WorkDir, r.info.InstanceID+"-")
	if err != nil {
		fail(500, "APP_START_FAILED", err)
		return
	}
	if err = download(startup, s.client, r.request.ArtifactURL, dir); err != nil {
		fail(502, "ARTIFACT_DOWNLOAD_FAILED", err)
		return
	}
	appDir := filepath.Join(dir, "artifact")
	if err = os.Mkdir(appDir, 0700); err != nil {
		fail(500, "APP_START_FAILED", err)
		return
	}
	if err = unpack(startup, filepath.Join(dir, "artifact.tar.gz"), appDir); err != nil {
		fail(422, "INVALID_ARTIFACT", err)
		return
	}
	l, err := validateArtifact(appDir)
	if err != nil {
		fail(422, "INVALID_ARTIFACT", err)
		return
	}
	logFile, err = os.Create(filepath.Join(dir, "app.log"))
	if err != nil {
		fail(500, "APP_START_FAILED", err)
		return
	}
	cmd = exec.Command(s.cfg.Node, l.RuntimeArgs...)
	cmd.Dir = appDir
	cmd.Env = appEnvironment(r.info.Port)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	ownProcessGroup(cmd)
	if err = cmd.Start(); err != nil {
		cmd = nil
		fail(502, "APP_START_FAILED", err)
		return
	}
	go func(c *exec.Cmd) { _ = c.Wait(); close(processDone) }(cmd)
	localURL := "http://127.0.0.1:" + strconv.Itoa(r.info.Port)
	healthCtx, cancelHealth := context.WithTimeout(startup, time.Duration(l.ReadyTimeoutMS)*time.Millisecond)
	err = waitHealth(healthCtx, localURL, cmd.Process.Pid, r.info.Port, processDone)
	cancelHealth()
	if err != nil {
		code := "APP_HEALTH_FAILED"
		status := 502
		if errors.Is(err, context.DeadlineExceeded) {
			code = "START_TIMEOUT"
			status = 504
		}
		if free, _ := portAvailable(r.info.Port); !free && !ownsListener(startup, cmd.Process.Pid, r.info.Port) {
			code = "PORT_IN_USE"
			status = 409
		}
		fail(status, code, err)
		return
	}
	if err = checkPage(startup, localURL+"/"); err != nil {
		fail(502, "PAGE_LOAD_FAILED", err)
		return
	}
	video, err = startMedia(r.ctx, startup, s.cfg, localURL+"/", filepath.Join(dir, "encoder.log"), r.hub)
	if err != nil {
		fail(502, "STREAM_START_FAILED", err)
		return
	}
	select {
	case <-r.ctx.Done():
		return
	case <-processDone:
		fail(502, "APP_EXITED", errors.New("application exited during startup"))
		return
	default:
	}
	s.mu.Lock()
	if r.stopRequested {
		s.mu.Unlock()
		return
	}
	r.info.State = "ready"
	r.info.AppBaseURL = "http://" + net.JoinHostPort(s.cfg.AppHost, strconv.Itoa(r.info.Port))
	r.info.Source = &casting.StreamSource{Type: "stream", URL: s.cfg.PublicURL + "/streams/" + r.info.InstanceID + "/live.ts", Format: "mpegts", VideoCodec: "h264", AudioCodec: ""}
	s.mu.Unlock()
	started = true
	close(r.started)
	stopStartup()
	s.cfg.Logger.Printf("app=%q instance=%s ready port=%d", r.info.AppID, r.info.InstanceID, r.info.Port)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	misses := 0
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-processDone:
			fail(502, "APP_EXITED", errors.New("application process exited"))
			return
		case err := <-video.failure:
			fail(502, "STREAM_FAILED", err)
			return
		case <-ticker.C:
			if r.hub.stalled() {
				fail(502, "STREAM_FAILED", errors.New("encoder output stalled"))
				return
			}
			if healthy(r.ctx, localURL) {
				misses = 0
			} else {
				misses++
				if misses >= 3 {
					fail(502, "APP_UNHEALTHY", errors.New("three consecutive health checks failed"))
					return
				}
			}
		}
	}
}

func appEnvironment(port int) []string {
	// Do not forward gateway secrets or Node loader flags to generated code.
	env := []string{"HOST=0.0.0.0", "PORT=" + strconv.Itoa(port), "NODE_ENV=production"}
	for _, key := range []string{"PATH", "LANG", "TZ", "TMPDIR"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}
func healthy(ctx context.Context, base string) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var v struct {
		Status string `json:"status"`
	}
	return res.StatusCode == 200 && json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&v) == nil && v.Status == "ok"
}
func waitHealth(ctx context.Context, base string, pid, port int, done <-chan struct{}) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return errors.New("application exited before becoming healthy")
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if healthy(ctx, base) && ownsListener(ctx, pid, port) {
			select {
			case <-done:
				return errors.New("application exited")
			default:
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return errors.New("application exited before becoming healthy")
		case <-ticker.C:
		}
	}
}
func (s *Service) stopGroupProcess(cmd *exec.Cmd, done <-chan struct{}) error {
	if s.cfg.stopGroup != nil {
		return s.cfg.stopGroup(cmd, done)
	}
	return stopProcess(cmd, done)
}

func (s *Service) Get(id string) (api.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.instances[id]
	if !ok {
		return api.Instance{}, false
	}
	return snapshot(r), true
}
func (s *Service) Stop(ctx context.Context, id string) (api.Instance, *Failure) {
	s.mu.Lock()
	r, ok := s.instances[id]
	if !ok {
		s.mu.Unlock()
		return api.Instance{}, failure(404, "INSTANCE_NOT_FOUND", "unknown instance")
	}
	if r.cleaned {
		r.info.State = "stopped"
		r.info.Error = nil
		v := snapshot(r)
		s.mu.Unlock()
		return v, nil
	}
	r.stopRequested = true
	r.info.State = "stopping"
	r.info.Source = nil
	r.info.AppBaseURL = ""
	r.cancel()
	s.mu.Unlock()
	select {
	case <-r.done:
	case <-ctx.Done():
		e := failure(500, "STOP_FAILED", "cleanup is still in progress")
		e.Detail.Stage = "running"
		e.AppID = r.info.AppID
		e.InstanceID = id
		return api.Instance{}, e
	}
	// Failed cleanup retains ownership. DELETE can retry it after the worker exits.
	s.mu.Lock()
	cleaned := r.cleaned
	s.mu.Unlock()
	if !cleaned {
		err := r.cleanup()
		s.mu.Lock()
		if err == nil {
			r.cleaned = true
			s.releaseLocked(r)
			r.info.State = "stopped"
			r.info.Error = nil
		}
		s.mu.Unlock()
		if err != nil {
			e := failure(500, "STOP_FAILED", err.Error())
			e.Detail.Stage = "running"
			e.AppID = r.info.AppID
			e.InstanceID = id
			return api.Instance{}, e
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshot(r), nil
}
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	ids := make([]string, 0, len(s.instances))
	for id := range s.instances {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	var errs []error
	for _, id := range ids {
		if _, err := s.Stop(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	s.cancel()
	return errors.Join(errs...)
}

// Old cleanup retries must never release a replacement instance's reservation.
func (s *Service) releaseLocked(r *run) {
	if s.apps[r.info.AppID] == r.info.InstanceID {
		delete(s.apps, r.info.AppID)
	}
	if s.ports[r.info.Port] == r.info.InstanceID {
		delete(s.ports, r.info.Port)
	}
}

func checkPage(ctx context.Context, pageURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return err
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(strings.ToLower(res.Header.Get("Content-Type")), "text/html") {
		return errors.New("application root must return HTTP 200 and text/html")
	}
	return nil
}
