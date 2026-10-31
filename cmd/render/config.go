package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	core "github.com/open-anywhere/open-anywhere/internal/render"
	"github.com/open-anywhere/open-anywhere/pkg/canvas"
)

// processConfig is everything needed to listen and start Render.
// Token is not a command-line flag; it comes from render.yaml or RENDER_TOKEN
// so the secret does not appear in the process list.
type processConfig struct {
	Addr string
	core.Config
}

type fileConfig struct {
	Addr        string `yaml:"addr"`
	PublicURL   string `yaml:"public_url"`
	AppHost     string `yaml:"app_host"`
	WorkDir     string `yaml:"work_dir"`
	Node        string `yaml:"node"`
	Chrome      string `yaml:"chrome"`
	FFmpeg      string `yaml:"ffmpeg"`
	Width       *int   `yaml:"width"`
	Height      *int   `yaml:"height"`
	FPS         *int   `yaml:"fps"`
	AudioFormat string `yaml:"audio_format"`
	AudioInput  string `yaml:"audio_input"`
	Token       string `yaml:"token"`
}

func loadConfig(args []string, getenv func(string) string, output io.Writer) (processConfig, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	path, explicit := locateConfig(args, getenv)
	file, err := loadFile(path, explicit)
	if err != nil {
		return processConfig{}, err
	}
	cfg := processConfig{
		Addr: ":8090",
		Config: core.Config{
			WorkDir: ".render-data",
			Node:    "node",
			FFmpeg:  "ffmpeg",
			Width:   canvas.Width,
			Height:  canvas.Height,
			FPS:     10,
		},
	}
	applyFile(&cfg, file)
	if err := applyEnv(&cfg, getenv); err != nil {
		return processConfig{}, err
	}
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&cfg.Addr, "addr", cfg.Addr, "API and media listen address")
	flags.StringVar(&cfg.PublicURL, "public-url", cfg.PublicURL, "HTTP origin reachable by gateway and TV (required)")
	flags.StringVar(&cfg.AppHost, "app-host", cfg.AppHost, "application host reachable by gateway (defaults to public URL host)")
	flags.StringVar(&cfg.WorkDir, "work-dir", cfg.WorkDir, "temporary instance directories")
	flags.StringVar(&cfg.Node, "node", cfg.Node, "Node.js executable")
	flags.StringVar(&cfg.Chrome, "chrome", cfg.Chrome, "Chromium executable (auto-detected if empty)")
	flags.StringVar(&cfg.FFmpeg, "ffmpeg", cfg.FFmpeg, "FFmpeg executable with libx264 (and AAC when audio is enabled)")
	flags.IntVar(&cfg.Width, "width", cfg.Width, "video width (must keep a 16:9 ratio)")
	flags.IntVar(&cfg.Height, "height", cfg.Height, "video height (must keep a 16:9 ratio)")
	flags.IntVar(&cfg.FPS, "fps", cfg.FPS, "capture frames per second")
	flags.StringVar(&cfg.AudioFormat, "audio-format", cfg.AudioFormat, "FFmpeg audio input format, e.g. pulse or avfoundation (empty disables audio)")
	flags.StringVar(&cfg.AudioInput, "audio-input", cfg.AudioInput, "FFmpeg audio input, e.g. render.monitor or :6 (empty disables audio)")
	flags.StringVar(&path, "config", path, "YAML configuration file")
	if err := flags.Parse(args); err != nil {
		return processConfig{}, err
	}
	if flags.NArg() != 0 {
		return processConfig{}, errors.New("render does not accept positional arguments")
	}
	cfg.Token = strings.TrimSpace(cfg.Token)
	return cfg, nil
}

func locateConfig(args []string, getenv func(string) string) (string, bool) {
	for index, arg := range args {
		switch {
		case arg == "-config" || arg == "--config":
			if index+1 < len(args) {
				return args[index+1], true
			}
			return "", true
		case strings.HasPrefix(arg, "-config="):
			return strings.TrimPrefix(arg, "-config="), true
		case strings.HasPrefix(arg, "--config="):
			return strings.TrimPrefix(arg, "--config="), true
		}
	}
	if value := getenv("RENDER_CONFIG"); value != "" {
		return value, true
	}
	return "render.yaml", false
}

func loadFile(path string, explicit bool) (fileConfig, error) {
	var file fileConfig
	if explicit && path == "" {
		return file, errors.New("render: -config requires a file path")
	}
	if path == "" {
		return file, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if explicit {
			return file, fmt.Errorf("render: config file %q does not exist", path)
		}
		return file, nil
	}
	if err != nil {
		return file, fmt.Errorf("render: read config file: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		if errors.Is(err, io.EOF) {
			return file, nil
		}
		return file, fmt.Errorf("render: parse config file %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return file, fmt.Errorf("render: config file %q must contain one YAML document", path)
	}
	return file, nil
}

func applyFile(cfg *processConfig, file fileConfig) {
	setString(&cfg.Addr, file.Addr)
	setString(&cfg.PublicURL, file.PublicURL)
	setString(&cfg.AppHost, file.AppHost)
	setString(&cfg.WorkDir, file.WorkDir)
	setString(&cfg.Node, file.Node)
	setString(&cfg.Chrome, file.Chrome)
	setString(&cfg.FFmpeg, file.FFmpeg)
	setString(&cfg.AudioFormat, file.AudioFormat)
	setString(&cfg.AudioInput, file.AudioInput)
	setString(&cfg.Token, file.Token)
	if file.Width != nil {
		cfg.Width = *file.Width
	}
	if file.Height != nil {
		cfg.Height = *file.Height
	}
	if file.FPS != nil {
		cfg.FPS = *file.FPS
	}
}

func applyEnv(cfg *processConfig, getenv func(string) string) error {
	setString(&cfg.Addr, getenv("RENDER_ADDR"))
	setString(&cfg.PublicURL, getenv("RENDER_PUBLIC_URL"))
	setString(&cfg.AppHost, getenv("RENDER_APP_HOST"))
	setString(&cfg.WorkDir, getenv("RENDER_WORK_DIR"))
	setString(&cfg.Node, getenv("RENDER_NODE"))
	setString(&cfg.Chrome, getenv("RENDER_CHROME"))
	setString(&cfg.FFmpeg, getenv("RENDER_FFMPEG"))
	setString(&cfg.AudioFormat, getenv("RENDER_AUDIO_FORMAT"))
	setString(&cfg.AudioInput, getenv("RENDER_AUDIO_INPUT"))
	setString(&cfg.Token, getenv("RENDER_TOKEN"))
	if err := setEnvInt(&cfg.Width, getenv, "RENDER_WIDTH"); err != nil {
		return err
	}
	if err := setEnvInt(&cfg.Height, getenv, "RENDER_HEIGHT"); err != nil {
		return err
	}
	return setEnvInt(&cfg.FPS, getenv, "RENDER_FPS")
}

func setString(dest *string, value string) {
	if strings.TrimSpace(value) != "" {
		*dest = strings.TrimSpace(value)
	}
}

func setEnvInt(dest *int, getenv func(string) string, key string) error {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("render: %s=%q is not an integer", key, value)
	}
	*dest = parsed
	return nil
}
