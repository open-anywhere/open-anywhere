package config

import (
	"strings"
	"testing"
)

func TestParseWithoutAGetenvFunctionUsesDefaults(t *testing.T) {
	cfg, err := Parse(nil, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Addr != ":8090" || cfg.PortMin != 39000 || cfg.BuildTimeout.String() != "1h0m0s" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLocateConfigFlagForms(t *testing.T) {
	path := writeConfig(t, `addr: ":7400"`)
	for _, args := range [][]string{
		{"-config=" + path},
		{"--config=" + path},
		{"-config", path},
	} {
		cfg, err := Parse(args, nil)
		if err != nil {
			t.Fatalf("Parse(%v): %v", args, err)
		}
		if cfg.Addr != ":7400" || cfg.ConfigPath != path {
			t.Fatalf("Parse(%v) = %+v", args, cfg)
		}
	}
	for _, args := range [][]string{{"-config"}, {"--config"}} {
		got, explicit := locateConfig(args, nil)
		if got != "" || !explicit {
			t.Fatalf("locateConfig(%v) = %q %v", args, got, explicit)
		}
	}
}

func TestLoadFileEdgeCases(t *testing.T) {
	file, err := loadFile("", true)
	if err != nil || file.Addr != "" {
		t.Fatalf("loadFile(\"\") = %+v %v", file, err)
	}
	// A directory exists but cannot be read as a configuration file.
	_, err = loadFile(t.TempDir(), true)
	if err == nil || !strings.Contains(err.Error(), "read config file") {
		t.Fatalf("err = %v", err)
	}
}
