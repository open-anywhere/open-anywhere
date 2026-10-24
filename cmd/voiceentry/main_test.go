package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

func TestCommandBuildsConfigAndStarts(t *testing.T) {
	var got voiceentry.Config
	var out bytes.Buffer
	code := command([]string{"-serve", "-listen", "127.0.0.1:9", "-device", "d.json", "-mode", "auto", "-no-play"}, &out, func(c voiceentry.Config) error {
		got = c
		return nil
	})
	if code != 0 || out.Len() != 0 {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
	if !got.Serve || got.Listen != "127.0.0.1:9" || got.DevicePath != "d.json" || got.Mode != voice.Auto || !got.NoPlay {
		t.Fatalf("config=%+v", got)
	}
}

func TestCommandOneShotFlags(t *testing.T) {
	var got voiceentry.Config
	if code := command([]string{"-text", "你好"}, &bytes.Buffer{}, func(c voiceentry.Config) error { got = c; return nil }); code != 0 || got.Text != "你好" || got.Serve {
		t.Fatalf("code=%d config=%+v", code, got)
	}
}

func TestCommandRejectsBadUsage(t *testing.T) {
	never := func(voiceentry.Config) error { t.Fatal("start called for invalid usage"); return nil }
	for name, args := range map[string][]string{
		"nothing to do":       {},
		"unknown flag":        {"-nope"},
		"positional argument": {"-serve", "extra"},
		"invalid mode":        {"-serve", "-mode", "loud"},
	} {
		var out bytes.Buffer
		if code := command(args, &out, never); code != 2 || out.Len() == 0 {
			t.Errorf("%s: code=%d out=%q", name, code, out.String())
		}
	}
}

func TestCommandHelpAndStartError(t *testing.T) {
	if code := command([]string{"-h"}, &bytes.Buffer{}, nil); code != 0 {
		t.Fatalf("help code=%d", code)
	}
	var out bytes.Buffer
	if code := command([]string{"-serve"}, &out, func(voiceentry.Config) error { return errors.New("boom") }); code != 1 || !strings.Contains(out.String(), "boom") {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
}

func TestRunReportsConfigurationErrors(t *testing.T) {
	cfg := voiceentry.Config{DevicePath: filepath.Join(t.TempDir(), "missing.json"), Text: "你好"}
	if err := run(cfg); err == nil {
		t.Fatal("run succeeded without a device file")
	}
}
