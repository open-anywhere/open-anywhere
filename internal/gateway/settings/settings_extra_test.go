package settings

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func closedProvider(t *testing.T) *Provider {
	t.Helper()
	provider, db := newProvider(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return provider
}

func keylessProvider(t *testing.T) (*Provider, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db, nil), db
}

func execOnDatabaseFile(t *testing.T, path string, statements ...string) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer raw.Close()
	for _, statement := range statements {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatalf("exec %q: %v", statement, err)
		}
	}
}

func TestSetURLValidatorContext(t *testing.T) {
	provider, db := newProvider(t)
	ctx := context.Background()
	rejected := errors.New("private address")
	var seen []string
	provider.SetURLValidatorContext(func(_ context.Context, value string) error {
		seen = append(seen, value)
		if strings.Contains(value, "private") {
			return rejected
		}
		return nil
	})

	_, err := provider.Preview(ctx, Patch{AgentAPI: &AgentAPIPatch{BaseURL: ptr("https://private.example.com")}})
	if !errors.Is(err, ErrInvalidURL) || !errors.Is(err, rejected) {
		t.Fatalf("err = %v", err)
	}
	snapshot, err := provider.Preview(ctx, Patch{AgentAPI: &AgentAPIPatch{BaseURL: ptr("https://public.example.com")}})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if snapshot.AgentAPI.BaseURL != "https://public.example.com" {
		t.Fatalf("BaseURL = %q", snapshot.AgentAPI.BaseURL)
	}
	if len(seen) != 2 || seen[0] != "https://private.example.com" {
		t.Fatalf("context validator saw %v", seen)
	}
	// Preview must not persist anything.
	rows, err := db.PlatformSettings(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("preview wrote settings: %+v err=%v", rows, err)
	}
}

func TestSeedEdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("closed database", func(t *testing.T) {
		provider := closedProvider(t)
		if err := provider.Seed(ctx, []Seed{{Key: "linx.api_base", Value: "https://linx.example.com"}}); err == nil {
			t.Fatal("Seed succeeded on a closed database")
		}
	})

	t.Run("empty seeds are skipped", func(t *testing.T) {
		provider, db := newProvider(t)
		if err := provider.Seed(ctx, []Seed{
			{Key: "", Value: "orphan"},
			{Key: "linx.api_base", Value: ""},
		}); err != nil {
			t.Fatalf("Seed: %v", err)
		}
		rows, err := db.PlatformSettings(ctx)
		if err != nil || len(rows) != 0 {
			t.Fatalf("rows=%+v err=%v", rows, err)
		}
	})

	t.Run("secret without a master key", func(t *testing.T) {
		provider, db := keylessProvider(t)
		err := provider.Seed(ctx, []Seed{{Key: KeyLinxAPIKey, Value: "linx-secret"}})
		if err == nil || !strings.Contains(err.Error(), "cannot seed secret") {
			t.Fatalf("err = %v", err)
		}
		rows, err := db.PlatformSettings(ctx)
		if err != nil || len(rows) != 0 {
			t.Fatalf("a rejected secret seed was partially written: %+v err=%v", rows, err)
		}
		if err := provider.Seed(ctx, []Seed{{Key: KeyLinxAPIBase, Value: "https://linx.example.com"}}); err != nil {
			t.Fatalf("non-secret seed without a box: %v", err)
		}
	})
}

func TestSnapshotErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("closed database", func(t *testing.T) {
		provider := closedProvider(t)
		if _, err := provider.Snapshot(ctx); err == nil {
			t.Fatal("Snapshot succeeded on a closed database")
		}
	})

	t.Run("encrypted secret without a master key", func(t *testing.T) {
		provider, db := keylessProvider(t)
		if err := db.PutPlatformSettings(ctx, []store.PlatformSetting{
			{Key: KeyLinxAPIKey, Value: "enc:v1:ciphertext", IsSecret: true},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Snapshot(ctx); err == nil ||
			!strings.Contains(err.Error(), "no master key is available") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("invalid agent version", func(t *testing.T) {
		for _, value := range []string{"abc", "-1"} {
			provider, db := newProvider(t)
			if err := db.PutPlatformSettings(ctx, []store.PlatformSetting{
				{Key: KeyAgentAPIAgentVersion, Value: value},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.Snapshot(ctx); err == nil ||
				!strings.Contains(err.Error(), KeyAgentAPIAgentVersion) {
				t.Fatalf("value=%q err=%v", value, err)
			}
		}
	})

	t.Run("invalid registration value", func(t *testing.T) {
		provider, db := newProvider(t)
		if err := db.PutPlatformSettings(ctx, []store.PlatformSetting{
			{Key: KeyRegistrationOpen, Value: "maybe"},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Snapshot(ctx); err == nil ||
			!strings.Contains(err.Error(), KeyRegistrationOpen) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("registration spellings", func(t *testing.T) {
		for value, want := range map[string]bool{
			"":       false,
			"false":  false,
			"0":      false,
			"true":   true,
			"1":      true,
			" TRUE ": true,
		} {
			provider, db := newProvider(t)
			if value != "" {
				if err := db.PutPlatformSettings(ctx, []store.PlatformSetting{
					{Key: KeyRegistrationOpen, Value: value},
				}); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := provider.Snapshot(ctx)
			if err != nil || snapshot.Registration.Open != want {
				t.Fatalf("value=%q open=%v err=%v", value, snapshot.Registration.Open, err)
			}
		}
	})
}

func TestRegistrationOpenErrorPaths(t *testing.T) {
	ctx := context.Background()

	if _, err := closedProvider(t).RegistrationOpen(ctx); err == nil {
		t.Fatal("RegistrationOpen succeeded on a closed database")
	}

	for value, want := range map[string]bool{"1": true, "0": false, "true": true, "false": false} {
		provider, db := newProvider(t)
		if err := db.PutPlatformSettings(ctx, []store.PlatformSetting{
			{Key: KeyRegistrationOpen, Value: value},
		}); err != nil {
			t.Fatal(err)
		}
		open, err := provider.RegistrationOpen(ctx)
		if err != nil || open != want {
			t.Fatalf("value=%q open=%v err=%v", value, open, err)
		}
	}

	provider, db := newProvider(t)
	if err := db.PutPlatformSettings(ctx, []store.PlatformSetting{
		{Key: KeyRegistrationOpen, Value: "enabled"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.RegistrationOpen(ctx); err == nil ||
		!strings.Contains(err.Error(), "invalid registration.open value") {
		t.Fatalf("err = %v", err)
	}
}

func TestMaskedErrorPaths(t *testing.T) {
	ctx := context.Background()

	if _, err := closedProvider(t).Masked(ctx); err == nil {
		t.Fatal("Masked succeeded on a closed database")
	}

	provider, db := newProvider(t)
	if err := db.PutPlatformSettings(ctx, []store.PlatformSetting{
		{Key: KeyLinxAPIKey, Value: "enc:v1:bm90LXJlYWw=", IsSecret: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Masked(ctx); !errors.Is(err, secret.ErrDecrypt) {
		t.Fatalf("err = %v", err)
	}
}

func TestPreview(t *testing.T) {
	ctx := context.Background()
	seedPreviewProvider := func(t *testing.T) *Provider {
		t.Helper()
		provider, _ := newProvider(t)
		if err := provider.Seed(ctx, []Seed{
			{Key: KeyAgentAPIBaseURL, Value: "https://agent.example.com"},
			{Key: KeyAgentAPIKey, Value: "agent-secret"},
			{Key: KeyAgentAPIAgentID, Value: "agent-1"},
			{Key: KeyAgentAPIAgentVersion, Value: "2"},
			{Key: KeyAgentAPIEnvironmentID, Value: "env-1"},
			{Key: KeyLinxAPIBase, Value: "https://linx.example.com"},
			{Key: KeyLinxOTAURL, Value: "https://ota.example.com"},
			{Key: KeyLinxAPIKey, Value: "linx-secret"},
			{Key: KeyDefaultRenderURL, Value: "https://render.example.com"},
			{Key: KeyDefaultRenderToken, Value: "render-secret"},
			{Key: KeyDefaultCastingURL, Value: "https://casting.example.com"},
			{Key: KeyDefaultCastingToken, Value: "casting-secret"},
		}); err != nil {
			t.Fatal(err)
		}
		return provider
	}

	t.Run("closed database", func(t *testing.T) {
		if _, err := closedProvider(t).Preview(ctx, Patch{}); err == nil {
			t.Fatal("Preview succeeded on a closed database")
		}
	})

	t.Run("empty patch returns the stored configuration", func(t *testing.T) {
		snapshot, err := seedPreviewProvider(t).Preview(ctx, Patch{})
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if snapshot.AgentAPI.BaseURL != "https://agent.example.com" ||
			snapshot.AgentAPI.APIKey != "agent-secret" ||
			snapshot.AgentAPI.AgentVersion != 2 ||
			snapshot.Linx.OTAURL != "https://ota.example.com" ||
			snapshot.Services.CastingToken != "casting-secret" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	})

	t.Run("agentapi fields", func(t *testing.T) {
		snapshot, err := seedPreviewProvider(t).Preview(ctx, Patch{AgentAPI: &AgentAPIPatch{
			BaseURL:       ptr("  https://agent2.example.com  "),
			AgentID:       ptr(" agent-2 "),
			AgentVersion:  ptr(7),
			EnvironmentID: ptr(" env-2 "),
			APIKey:        &SecretValue{Value: "new-agent-secret"},
		}})
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		agent := snapshot.AgentAPI
		if agent.BaseURL != "https://agent2.example.com" || agent.AgentID != "agent-2" ||
			agent.AgentVersion != 7 || agent.EnvironmentID != "env-2" || agent.APIKey != "new-agent-secret" {
			t.Fatalf("agentapi = %+v", agent)
		}
	})

	t.Run("secrets keep and clear", func(t *testing.T) {
		provider := seedPreviewProvider(t)
		snapshot, err := provider.Preview(ctx, Patch{
			AgentAPI: &AgentAPIPatch{APIKey: &SecretValue{}},
			Linx:     &LinxPatch{APIKey: &SecretValue{Value: "  "}},
			Services: &ServicesPatch{RenderToken: &SecretValue{Clear: true}},
		})
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if snapshot.AgentAPI.APIKey != "agent-secret" || snapshot.Linx.APIKey != "linx-secret" {
			t.Fatalf("empty secrets changed values: %+v", snapshot)
		}
		if snapshot.Services.RenderToken != "" || snapshot.Services.CastingToken != "casting-secret" {
			t.Fatalf("services = %+v", snapshot.Services)
		}
	})

	t.Run("negative agent version", func(t *testing.T) {
		_, err := seedPreviewProvider(t).Preview(ctx, Patch{AgentAPI: &AgentAPIPatch{AgentVersion: ptr(-2)}})
		if !errors.Is(err, ErrInvalidSetting) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("blank url clears the address", func(t *testing.T) {
		snapshot, err := seedPreviewProvider(t).Preview(ctx, Patch{AgentAPI: &AgentAPIPatch{BaseURL: ptr("   ")}})
		if err != nil || snapshot.AgentAPI.BaseURL != "" {
			t.Fatalf("BaseURL=%q err=%v", snapshot.AgentAPI.BaseURL, err)
		}
	})

	t.Run("default validator rejects a non-http url", func(t *testing.T) {
		provider := seedPreviewProvider(t)
		cases := map[string]Patch{
			"agentapi": {AgentAPI: &AgentAPIPatch{BaseURL: ptr("ftp://agent.example.com")}},
			"linx":     {Linx: &LinxPatch{APIBase: ptr("ftp://linx.example.com")}},
			"ota":      {Linx: &LinxPatch{OTAURL: ptr("ftp://ota.example.com")}},
			"render":   {Services: &ServicesPatch{RenderURL: ptr("ftp://render.example.com")}},
			"casting":  {Services: &ServicesPatch{CastingURL: ptr("ftp://casting.example.com")}},
			"relative": {AgentAPI: &AgentAPIPatch{BaseURL: ptr("agent.example.com")}},
		}
		for name, patch := range cases {
			t.Run(name, func(t *testing.T) {
				if _, err := provider.Preview(ctx, patch); !errors.Is(err, ErrInvalidURL) {
					t.Fatalf("err = %v", err)
				}
			})
		}
	})

	t.Run("linx and services fields", func(t *testing.T) {
		snapshot, err := seedPreviewProvider(t).Preview(ctx, Patch{
			Linx: &LinxPatch{
				APIBase: ptr(" https://linx2.example.com "),
				OTAURL:  ptr(" https://ota2.example.com "),
				APIKey:  &SecretValue{Value: "linx-2"},
			},
			Services: &ServicesPatch{
				RenderURL:    ptr(" https://render2.example.com "),
				CastingURL:   ptr(" https://casting2.example.com "),
				RenderToken:  &SecretValue{Value: "render-2"},
				CastingToken: &SecretValue{Clear: true},
			},
			Registration: &RegistrationPatch{Open: ptr(false)},
		})
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if snapshot.Linx.APIBase != "https://linx2.example.com" || snapshot.Linx.OTAURL != "https://ota2.example.com" ||
			snapshot.Linx.APIKey != "linx-2" {
			t.Fatalf("linx = %+v", snapshot.Linx)
		}
		if snapshot.Services.RenderURL != "https://render2.example.com" ||
			snapshot.Services.CastingURL != "https://casting2.example.com" ||
			snapshot.Services.RenderToken != "render-2" || snapshot.Services.CastingToken != "" {
			t.Fatalf("services = %+v", snapshot.Services)
		}
	})

	t.Run("registration switch", func(t *testing.T) {
		provider := seedPreviewProvider(t)
		for _, want := range []bool{true, false} {
			snapshot, err := provider.Preview(ctx, Patch{Registration: &RegistrationPatch{Open: ptr(want)}})
			if err != nil || snapshot.Registration.Open != want {
				t.Fatalf("open=%v err=%v", snapshot.Registration.Open, err)
			}
		}
		if snapshot, err := provider.Preview(ctx, Patch{Registration: &RegistrationPatch{}}); err != nil || snapshot.Registration.Open {
			t.Fatalf("a nil switch changed registration: %+v err=%v", snapshot.Registration, err)
		}
	})
}

func TestUpdateErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("closed database", func(t *testing.T) {
		provider := closedProvider(t)
		if err := provider.Update(ctx, Patch{Registration: &RegistrationPatch{Open: ptr(true)}}, "admin"); err == nil {
			t.Fatal("Update succeeded on a closed database")
		}
	})

	t.Run("undecryptable stored secret", func(t *testing.T) {
		provider, db := newProvider(t)
		if err := db.PutPlatformSettings(ctx, []store.PlatformSetting{
			{Key: KeyLinxAPIKey, Value: "enc:v1:bm90LXJlYWw=", IsSecret: true},
		}); err != nil {
			t.Fatal(err)
		}
		if err := provider.Update(ctx, Patch{Registration: &RegistrationPatch{Open: ptr(true)}}, "admin"); !errors.Is(err, secret.ErrDecrypt) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("secrets need a master key", func(t *testing.T) {
		cases := map[string]Patch{
			"agentapi": {AgentAPI: &AgentAPIPatch{APIKey: &SecretValue{Value: "secret"}}},
			"linx":     {Linx: &LinxPatch{APIKey: &SecretValue{Value: "secret"}}},
			"render":   {Services: &ServicesPatch{RenderToken: &SecretValue{Value: "secret"}}},
			"casting":  {Services: &ServicesPatch{CastingToken: &SecretValue{Value: "secret"}}},
		}
		for name, patch := range cases {
			t.Run(name, func(t *testing.T) {
				provider, _ := keylessProvider(t)
				if err := provider.Update(ctx, patch, "admin"); err == nil ||
					!strings.Contains(err.Error(), "without a master key") {
					t.Fatalf("err = %v", err)
				}
			})
		}
	})

	t.Run("invalid urls are rejected without writes", func(t *testing.T) {
		provider, db := newProvider(t)
		cases := map[string]Patch{
			"agentapi": {AgentAPI: &AgentAPIPatch{BaseURL: ptr("ftp://agent.example.com")}},
			"linx":     {Linx: &LinxPatch{APIBase: ptr("ftp://linx.example.com")}},
			"ota":      {Linx: &LinxPatch{OTAURL: ptr("ftp://ota.example.com")}},
			"render":   {Services: &ServicesPatch{RenderURL: ptr("ftp://render.example.com")}},
			"casting":  {Services: &ServicesPatch{CastingURL: ptr("ftp://casting.example.com")}},
		}
		for name, patch := range cases {
			t.Run(name, func(t *testing.T) {
				if err := provider.Update(ctx, patch, "admin"); !errors.Is(err, ErrInvalidURL) {
					t.Fatalf("err = %v", err)
				}
			})
		}
		rows, err := db.PlatformSettings(ctx)
		if err != nil || len(rows) != 0 {
			t.Fatalf("a rejected update wrote settings: %+v err=%v", rows, err)
		}
	})

	t.Run("fields are persisted", func(t *testing.T) {
		provider, db := newProvider(t)
		if err := provider.Update(ctx, Patch{
			AgentAPI: &AgentAPIPatch{
				BaseURL:       ptr(" https://agent.example.com "),
				AgentID:       ptr(" agent-1 "),
				AgentVersion:  ptr(3),
				EnvironmentID: ptr(" env-1 "),
				APIKey:        &SecretValue{Value: "agent-secret"},
			},
			Linx: &LinxPatch{
				APIBase: ptr("https://linx.example.com"),
				OTAURL:  ptr(" https://ota.example.com "),
				APIKey:  &SecretValue{Value: "linx-secret"},
			},
			Services: &ServicesPatch{
				RenderURL:    ptr("https://render.example.com"),
				CastingURL:   ptr(" https://casting.example.com "),
				RenderToken:  &SecretValue{Value: "render-secret"},
				CastingToken: &SecretValue{Value: "casting-secret"},
			},
			Registration: &RegistrationPatch{Open: ptr(true)},
		}, "usr_admin"); err != nil {
			t.Fatalf("Update: %v", err)
		}
		snapshot, err := provider.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.AgentAPI.BaseURL != "https://agent.example.com" || snapshot.AgentAPI.AgentID != "agent-1" ||
			snapshot.AgentAPI.AgentVersion != 3 || snapshot.AgentAPI.EnvironmentID != "env-1" ||
			snapshot.AgentAPI.APIKey != "agent-secret" {
			t.Fatalf("agentapi = %+v", snapshot.AgentAPI)
		}
		if snapshot.Linx.APIBase != "https://linx.example.com" || snapshot.Linx.OTAURL != "https://ota.example.com" ||
			snapshot.Linx.APIKey != "linx-secret" {
			t.Fatalf("linx = %+v", snapshot.Linx)
		}
		if snapshot.Services.RenderURL != "https://render.example.com" ||
			snapshot.Services.CastingURL != "https://casting.example.com" ||
			snapshot.Services.RenderToken != "render-secret" || snapshot.Services.CastingToken != "casting-secret" {
			t.Fatalf("services = %+v", snapshot.Services)
		}
		if !snapshot.Registration.Open {
			t.Fatal("registration switch was not persisted")
		}
		rows, err := db.PlatformSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if rows[KeyAgentAPIBaseURL].UpdatedBy != "usr_admin" || rows[KeyAgentAPIBaseURL].UpdatedAt.IsZero() {
			t.Fatalf("base url setting = %+v", rows[KeyAgentAPIBaseURL])
		}
		if !strings.HasPrefix(rows[KeyDefaultRenderToken].Value, "enc:v1:") {
			t.Fatalf("render token at rest = %q", rows[KeyDefaultRenderToken].Value)
		}
	})

	t.Run("empty and cleared values", func(t *testing.T) {
		provider, _ := newProvider(t)
		if err := provider.Update(ctx, Patch{AgentAPI: &AgentAPIPatch{APIKey: &SecretValue{Value: "keep"}}}, "admin"); err != nil {
			t.Fatal(err)
		}
		if err := provider.Update(ctx, Patch{
			AgentAPI: &AgentAPIPatch{BaseURL: ptr("  "), APIKey: &SecretValue{}},
			Linx:     &LinxPatch{APIBase: ptr(""), APIKey: &SecretValue{Value: " "}},
			Services: &ServicesPatch{
				RenderURL:    ptr(" "),
				CastingURL:   ptr(""),
				RenderToken:  &SecretValue{},
				CastingToken: &SecretValue{},
			},
			Registration: &RegistrationPatch{},
		}, "admin"); err != nil {
			t.Fatalf("Update: %v", err)
		}
		snapshot, err := provider.Snapshot(ctx)
		if err != nil || snapshot.AgentAPI.APIKey != "keep" {
			t.Fatalf("APIKey=%q err=%v", snapshot.AgentAPI.APIKey, err)
		}
		if err := provider.Update(ctx, Patch{AgentAPI: &AgentAPIPatch{APIKey: &SecretValue{Clear: true}}}, "admin"); err != nil {
			t.Fatal(err)
		}
		if snapshot, err = provider.Snapshot(ctx); err != nil || snapshot.AgentAPI.APIKey != "" {
			t.Fatalf("APIKey=%q err=%v", snapshot.AgentAPI.APIKey, err)
		}
	})

	t.Run("negative agent version", func(t *testing.T) {
		provider, _ := newProvider(t)
		if err := provider.Update(ctx, Patch{AgentAPI: &AgentAPIPatch{AgentVersion: ptr(-1)}}, "admin"); !errors.Is(err, ErrInvalidSetting) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestUpdateReportsDatabaseFailures(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Date(2026, 10, 25, 18, 0, 0, 0, time.UTC) }

	t.Run("insert rejected", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gateway.db")
		first, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		execOnDatabaseFile(t, path, `CREATE TRIGGER refuse_insert BEFORE INSERT ON platform_settings
			BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		box, err := secret.New(bytes.Repeat([]byte{4}, 32))
		if err != nil {
			t.Fatal(err)
		}
		provider := New(db, box)
		err = provider.Update(ctx, Patch{Registration: &RegistrationPatch{Open: ptr(true)}}, "admin")
		if err == nil || !strings.Contains(err.Error(), "blocked") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("delete rejected", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gateway.db")
		first, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		execOnDatabaseFile(t, path, `CREATE TRIGGER refuse_delete BEFORE DELETE ON platform_settings
			BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		box, err := secret.New(bytes.Repeat([]byte{4}, 32))
		if err != nil {
			t.Fatal(err)
		}
		provider := New(db, box)
		provider.now = now
		if err := provider.Update(ctx, Patch{Linx: &LinxPatch{APIKey: &SecretValue{Value: "linx"}}}, "admin"); err != nil {
			t.Fatalf("seed secret: %v", err)
		}
		err = provider.Update(ctx, Patch{Linx: &LinxPatch{APIKey: &SecretValue{Clear: true}}}, "admin")
		if err == nil || !strings.Contains(err.Error(), "blocked") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("cached mcp cleanup rejected", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gateway.db")
		first, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		execOnDatabaseFile(t, path, `DROP TABLE portal_settings`)
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		box, err := secret.New(bytes.Repeat([]byte{4}, 32))
		if err != nil {
			t.Fatal(err)
		}
		provider := New(db, box)
		provider.now = now
		err = provider.Update(ctx, Patch{Linx: &LinxPatch{APIBase: ptr("https://linx.example.com")}}, "admin")
		if err == nil || !strings.Contains(err.Error(), "delete portal setting") {
			t.Fatalf("err = %v", err)
		}
		// The setting itself was already committed before the cache cleanup failed.
		if snapshot, snapshotErr := provider.Snapshot(ctx); snapshotErr != nil || snapshot.Linx.APIBase != "https://linx.example.com" {
			t.Fatalf("snapshot=%+v err=%v", snapshot, snapshotErr)
		}
	})
}
