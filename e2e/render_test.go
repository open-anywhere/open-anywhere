//go:build e2e && (darwin || linux)

package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	core "github.com/open-anywhere/open-anywhere/internal/render"
	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

func archive(t *testing.T, missingHealth bool) []byte {
	return archiveVariant(t, func(rel string, data []byte) []byte {
		if missingHealth && rel == "app/app.js" {
			data = bytes.ReplaceAll(data, []byte("url.pathname === '/health'"), []byte("url.pathname === '/missing-health'"))
		}
		if missingHealth && rel == "launch.json" {
			data = bytes.ReplaceAll(data, []byte("5000"), []byte("700"))
		}
		return data
	})
}

func archiveVariant(t *testing.T, transform func(string, []byte) []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir("testdata/scoreboard", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel("testdata/scoreboard", path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data = transform(rel, data)
		if err = tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0600, Size: int64(len(data))}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func freePort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
func request(t *testing.T, method, url string, body any, want int) []byte {
	t.Helper()
	var input io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		input = bytes.NewReader(b)
	}
	r, e := http.NewRequest(method, url, input)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 90 * time.Second}
	res, e := client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	if e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s: got %d want %d: %s", method, url, res.StatusCode, want, b)
	}
	return b
}
func decode[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if e := json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}

type frames struct {
	output <-chan []byte
	done   <-chan error
	cancel context.CancelFunc
}

func decodeStream(t *testing.T, url string) frames {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-probesize", "1000000", "-analyzeduration", "1000000", "-i", url, "-an", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ch := make(chan []byte, 8)
	done := make(chan error, 1)
	go func() {
		defer close(ch)
		for {
			b := make([]byte, 1280*720*3)
			if _, err := io.ReadFull(stdout, b); err != nil {
				break
			}
			select {
			case ch <- b:
			case <-ctx.Done():
				break
			default: // Keep draining decoded output even when assertions are between steps.
			}
		}
		err := cmd.Wait()
		if err != nil {
			err = fmt.Errorf("%w: %s", err, stderr.String())
		}
		done <- err
	}()
	t.Cleanup(cancel)
	return frames{ch, done, cancel}
}
func waitColor(t *testing.T, f frames, want [3]int, label string) []byte {
	t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	var last []byte
	for {
		select {
		case b, ok := <-f.output:
			if !ok {
				t.Fatalf("decoder ended while waiting for %s", label)
			}
			last = b
			// The deterministic swatch is x=44..164, y=578..648 at 1280x720.
			var total [3]int
			for y := 600; y < 620; y++ {
				for x := 70; x < 90; x++ {
					i := (y*1280 + x) * 3
					for c := 0; c < 3; c++ {
						total[c] += int(b[i+c])
					}
				}
			}
			match := true
			for c := 0; c < 3; c++ {
				delta := total[c]/400 - want[c]
				if delta < -25 || delta > 25 {
					match = false
				}
			}
			if match {
				return b
			}

		case <-deadline.C:
			if last != nil {
				saveFrame(t, last, "failure-"+label)
			}
			t.Fatalf("video never showed %s", label)
		}
	}
}
func saveFrame(t *testing.T, b []byte, name string) {
	t.Helper()
	dir := os.Getenv("RENDER_E2E_OUTPUT")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 1280, 720))
	for i := 0; i < 1280*720; i++ {
		copy(img.Pix[i*4:i*4+3], b[i*3:i*3+3])
		img.Pix[i*4+3] = 255
	}
	f, e := os.Create(filepath.Join(dir, name+".png"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = png.Encode(f, img); e != nil {
		t.Fatal(e)
	}
}

func TestRenderScoreboard(t *testing.T) {
	for _, bin := range []string{"node", "ffmpeg", "ffprobe", "lsof"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("E2E prerequisite %s: %v", bin, err)
		}
	}
	good, bad := archive(t, false), archive(t, true)
	artifacts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		if r.URL.Path == "/bad.tar.gz" {
			_, _ = w.Write(bad)
		} else {
			_, _ = w.Write(good)
		}
	}))
	defer artifacts.Close()
	server := httptest.NewUnstartedServer(nil)
	apiPort := server.Listener.Addr().(*net.TCPAddr).Port
	base := "http://" + server.Listener.Addr().String()
	work := t.TempDir()
	s, err := core.New(core.Config{
		WorkDir:      work,
		PublicURL:    base,
		Chrome:       os.Getenv("RENDER_CHROME"),
		AudioFormat:  os.Getenv("RENDER_AUDIO_FORMAT"),
		AudioInput:   os.Getenv("RENDER_AUDIO_INPUT"),
		ServicePorts: []int{apiPort},
		Logger:       log.New(os.Stderr, "render-e2e ", log.LstdFlags),
	})
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = core.NewHandler(s)
	server.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
		server.Close()
	})
	port := freePort(t)
	p := decode[api.Port](t, request(t, "GET", base+"/v1/ports/"+strconv.Itoa(port), nil, 200))
	if p.Occupied {
		t.Fatal("expected empty port")
	}
	req := api.CreateRequest{AppID: "app-scoreboard", ArtifactURL: artifacts.URL + "/scoreboard.tar.gz", Port: port}
	instance := decode[api.Instance](t, request(t, "POST", base+"/v1/apps", req, 201))
	if instance.State != "ready" || instance.AppID != req.AppID || instance.Source == nil {
		t.Fatalf("bad instance: %+v", instance)
	}
	request(t, "GET", instance.AppBaseURL+"/", nil, 200)
	p = decode[api.Port](t, request(t, "GET", base+"/v1/ports/"+strconv.Itoa(port), nil, 200))
	if !p.Occupied || p.AppID != req.AppID {
		t.Fatalf("bad ownership: %+v", p)
	}
	listed := decode[struct {
		Ports []api.Port `json:"ports"`
	}](t, request(t, "GET", base+"/v1/ports", nil, 200))
	found := false
	for _, p := range listed.Ports {
		if p.AppID == req.AppID && p.Port == port {
			found = true
		}
	}
	if !found {
		t.Fatal("app missing from occupied port list")
	}
	duplicate := req
	duplicate.Port = freePort(t)
	if !bytes.Contains(request(t, "POST", base+"/v1/apps", duplicate, 409), []byte("APP_ALREADY_RUNNING")) {
		t.Fatal("wrong duplicate error")
	}
	other := req
	other.AppID = "another-app"
	if !bytes.Contains(request(t, "POST", base+"/v1/apps", other, 409), []byte("PORT_IN_USE")) {
		t.Fatal("wrong port error")
	}
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	probe, probeErr := exec.CommandContext(probeCtx, "ffprobe", "-v", "error", "-analyzeduration", "5000000", "-probesize", "5000000", "-show_entries", "stream=codec_name,codec_type,width,height", "-of", "json", instance.Source.URL).Output()
	probeCancel()
	if probeErr != nil {
		t.Fatalf("probe stream: %v", probeErr)
	}
	metadata := decode[struct {
		Streams []struct {
			Codec  string `json:"codec_name"`
			Type   string `json:"codec_type"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
	}](t, probe)
	wantStreams := 1
	if os.Getenv("RENDER_AUDIO_FORMAT") != "" {
		wantStreams = 2
	}
	if len(metadata.Streams) != wantStreams || metadata.Streams[0].Codec != "h264" || metadata.Streams[0].Width != 1280 || metadata.Streams[0].Height != 720 {
		t.Fatalf("unexpected stream: %s", probe)
	}
	if wantStreams == 2 && (metadata.Streams[1].Type != "audio" || metadata.Streams[1].Codec != "aac" || instance.Source.AudioCodec != "aac") {
		t.Fatalf("unexpected audio stream: %s source=%+v", probe, instance.Source)
	}
	f := decodeStream(t, instance.Source.URL)
	saveFrame(t, waitColor(t, f, [3]int{128, 128, 128}, "initial gray"), "01-initial")
	for _, step := range []struct {
		team  string
		color [3]int
	}{{"red", [3]int{232, 58, 58}}, {"blue", [3]int{36, 91, 232}}} {
		request(t, "POST", instance.AppBaseURL+"/api/score", map[string]any{"team_id": step.team, "points": 1}, 200)
		state := decode[map[string]any](t, request(t, "GET", instance.AppBaseURL+"/api/state", nil, 200))
		if state[step.team] != float64(1) || state["last_team"] != step.team {
			t.Fatalf("bad state: %v", state)
		}
		saveFrame(t, waitColor(t, f, step.color, step.team), "02-"+step.team)
		if step.team == "red" {
			reconnected := decodeStream(t, instance.Source.URL)
			waitColor(t, reconnected, step.color, "red after reconnect")
			reconnected.cancel()
			select {
			case <-reconnected.done:
			case <-time.After(5 * time.Second):
				t.Fatal("reconnected decoder leaked")
			}
		}
	}
	request(t, "POST", instance.AppBaseURL+"/api/reset", map[string]any{}, 200)
	saveFrame(t, waitColor(t, f, [3]int{128, 128, 128}, "reset gray"), "03-reset")
	stopped := decode[api.Instance](t, request(t, "DELETE", base+"/v1/apps/"+instance.InstanceID, nil, 200))
	if stopped.State != "stopped" {
		t.Fatal(stopped)
	}
	select {
	case <-f.done:
	case <-time.After(10 * time.Second):
		t.Fatal("media decoder did not terminate after stop")
	}
	request(t, "DELETE", base+"/v1/apps/"+instance.InstanceID, nil, 200)
	request(t, "GET", instance.Source.URL, nil, 404)
	p = decode[api.Port](t, request(t, "GET", base+"/v1/ports/"+strconv.Itoa(port), nil, 200))
	if p.Occupied {
		t.Fatal("port not released")
	}
	entries, err := os.ReadDir(work)
	if err != nil || len(entries) != 0 {
		t.Fatalf("work directories leaked: %v %v", entries, err)
	}
	// The same app can start again on a different injected port after cleanup.
	req.Port = freePort(t)
	restarted := decode[api.Instance](t, request(t, "POST", base+"/v1/apps", req, 201))
	if restarted.InstanceID == instance.InstanceID {
		t.Fatal("instance id reused")
	}
	// Kill the real Node.js process, then observe runtime error and cleanup.
	pidText, pidErr := exec.Command("lsof", "-nP", "-t", "-iTCP:"+strconv.Itoa(req.Port), "-sTCP:LISTEN").Output()
	if pidErr != nil {
		t.Fatal(pidErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidText)))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		v := decode[api.Instance](t, request(t, "GET", base+"/v1/apps/"+restarted.InstanceID, nil, 200))
		if v.State == "error" && v.Error != nil && v.Error.Code == "APP_EXITED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not observe process failure: %+v", v)
		}
		time.Sleep(100 * time.Millisecond)
	}
	request(t, "DELETE", base+"/v1/apps/"+restarted.InstanceID, nil, 200)
	badReq := api.CreateRequest{AppID: "missing-health", ArtifactURL: artifacts.URL + "/bad.tar.gz", Port: freePort(t)}
	failure := request(t, "POST", base+"/v1/apps", badReq, 504)
	if !strings.Contains(string(failure), "START_TIMEOUT") {
		t.Fatalf("unexpected missing health error: %s", failure)
	}
	p = decode[api.Port](t, request(t, "GET", base+"/v1/ports/"+strconv.Itoa(badReq.Port), nil, 200))
	if p.Occupied {
		t.Fatal("failed startup leaked port")
	}
}

func TestRenderStartupFailureStages(t *testing.T) {
	for _, bin := range []string{"node", "ffmpeg", "lsof"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("E2E prerequisite %s: %v", bin, err)
		}
	}
	good := archive(t, false)
	badPage := archiveVariant(t, func(rel string, data []byte) []byte {
		if rel == "app/app.js" {
			data = bytes.ReplaceAll(data, []byte("['/', ['index.html', 'text/html; charset=utf-8']]"), []byte("['/', ['index.html', 'application/json']]"))
		}
		return data
	})
	artifacts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		if r.URL.Path == "/bad-page.tar.gz" {
			_, _ = w.Write(badPage)
			return
		}
		_, _ = w.Write(good)
	}))
	defer artifacts.Close()

	newService := func(t *testing.T, ffmpeg string) (*core.Service, *httptest.Server) {
		t.Helper()
		server := httptest.NewUnstartedServer(nil)
		base := "http://" + server.Listener.Addr().String()
		cfg := core.Config{WorkDir: t.TempDir(), PublicURL: base, Chrome: os.Getenv("RENDER_CHROME"), FFmpeg: ffmpeg, Logger: log.New(io.Discard, "", 0)}
		s, err := core.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		server.Config.Handler = core.NewHandler(s)
		server.Start()
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := s.Close(ctx); err != nil {
				t.Error(err)
			}
			server.Close()
		})
		return s, server
	}

	_, pageServer := newService(t, "ffmpeg")
	badPageReq := api.CreateRequest{AppID: "bad-page", ArtifactURL: artifacts.URL + "/bad-page.tar.gz", Port: freePort(t)}
	pageBody := request(t, "POST", pageServer.URL+"/v1/apps", badPageReq, 502)
	if !bytes.Contains(pageBody, []byte("PAGE_LOAD_FAILED")) {
		t.Fatalf("unexpected page failure: %s", pageBody)
	}

	_, encoderServer := newService(t, "/no-such-ffmpeg")
	encoderReq := api.CreateRequest{AppID: "bad-encoder", ArtifactURL: artifacts.URL + "/good.tar.gz", Port: freePort(t)}
	encoderBody := request(t, "POST", encoderServer.URL+"/v1/apps", encoderReq, 502)
	if !bytes.Contains(encoderBody, []byte("STREAM_START_FAILED")) {
		t.Fatalf("unexpected encoder failure: %s", encoderBody)
	}
}
