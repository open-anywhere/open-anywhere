package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/gateway/internal/agentapi"
	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/gateway/internal/artifact"
	"github.com/open-anywhere/open-anywhere/gateway/internal/downstream"
	"github.com/open-anywhere/open-anywhere/gateway/internal/openapi"
)

type config struct {
	Mode                    string               `json:"mode"`
	StateFile               string               `json:"state_file"`
	ArtifactDir             string               `json:"artifact_dir"`
	PublicURL               string               `json:"public_url"`
	RenderURL               string               `json:"render_url"`
	CastingURL              string               `json:"casting_url"`
	PortMin                 int                  `json:"port_min"`
	PortMax                 int                  `json:"port_max"`
	Screens                 []application.Screen `json:"screens"`
	AgentAPI                *agentapiConfig      `json:"agentapi,omitempty"`
	BuildTimeoutSeconds     int                  `json:"build_timeout_seconds"`
	OperationTimeoutSeconds int                  `json:"operation_timeout_seconds"`
	Services                []openapi.Service    `json:"services,omitempty"`
	DemoOwner               *application.Owner   `json:"demo_owner,omitempty"`
}

// agentapiConfig keeps deployment credentials out of the API session payload.
type agentapiConfig struct {
	agentapi.Config
	APIKey  string `json:"api_key"`
	Enabled *bool  `json:"enabled,omitempty"`
}

func buildClient(cfg *agentapiConfig, store artifact.Store) (*agentapi.Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("agentapi.api_key is required in the local configuration file")
	}
	return agentapi.New(cfg.Config, cfg.APIKey, store)
}

type dependencies struct {
	controller *openapi.Controller
	apps       *application.Manager
	artifacts  http.Handler
}

func loadDependencies(ctx context.Context, path string) (dependencies, error) {
	var d dependencies
	b, err := os.ReadFile(path)
	if err != nil {
		return d, err
	}
	var cfg config
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return d, err
	}
	if cfg.Mode == "static" || (cfg.Mode == "" && len(cfg.Services) > 0) {
		d.controller, err = openapi.New(ctx, openapi.Config{Services: cfg.Services})
		return d, err
	}
	if cfg.Mode != "" && cfg.Mode != "managed" {
		return d, fmt.Errorf("mode must be managed or static")
	}
	resolve := func(p, defaultPath string) string {
		if p == "" {
			p = defaultPath
		}
		if !filepath.IsAbs(p) {
			return filepath.Join(filepath.Dir(path), p)
		}
		return p
	}
	cfg.StateFile = resolve(cfg.StateFile, "../.runtime/apps.json")
	cfg.ArtifactDir = resolve(cfg.ArtifactDir, "../.runtime/artifacts")
	store := artifact.Store{Dir: cfg.ArtifactDir, PublicURL: cfg.PublicURL}
	opts := application.Options{StateFile: cfg.StateFile, Screens: cfg.Screens, PortMin: cfg.PortMin, PortMax: cfg.PortMax, BuildTimeout: time.Duration(cfg.BuildTimeoutSeconds) * time.Second, OperationTimeout: time.Duration(cfg.OperationTimeoutSeconds) * time.Second}
	if cfg.RenderURL != "" {
		c, e := downstream.New(cfg.RenderURL, os.Getenv("RENDER_TOKEN"))
		if e != nil {
			return d, e
		}
		opts.Runtime = downstream.Render{Client: c}
	}
	if cfg.CastingURL != "" {
		c, e := downstream.New(cfg.CastingURL, os.Getenv("CASTING_TOKEN"))
		if e != nil {
			return d, e
		}
		opts.Display = downstream.Casting{Client: c}
	}
	if cfg.AgentAPI != nil && (cfg.AgentAPI.Enabled == nil || *cfg.AgentAPI.Enabled) {
		if _, e := downstream.New(cfg.PublicURL, ""); e != nil {
			return d, fmt.Errorf("public_url required for Render artifact downloads")
		}
		builder, e := buildClient(cfg.AgentAPI, store)
		if e != nil {
			return d, e
		}
		opts.Builder = builder
	}
	d.apps, err = application.New(ctx, opts)
	if err != nil {
		return d, err
	}
	if len(cfg.Services) > 0 {
		if cfg.DemoOwner == nil {
			d.apps.Close()
			return d, fmt.Errorf("static demo import requires demo_owner")
		}
		c, e := openapi.New(ctx, openapi.Config{Services: cfg.Services})
		if e != nil {
			d.apps.Close()
			return d, e
		}
		for _, s := range cfg.Services {
			doc, _ := c.Document(s.ID)
			if e = d.apps.RegisterDemo(*cfg.DemoOwner, s.ID, s.BaseURL, doc); e != nil {
				d.apps.Close()
				return d, e
			}
		}
	}
	if err = d.apps.Recover(); err != nil {
		d.apps.Close()
		return d, err
	}
	d.apps.Watch(5 * time.Second)
	d.artifacts = store.Handler()
	return d, nil
}
