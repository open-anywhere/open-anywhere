package store

import (
	"context"
	"testing"
	"time"
)

func TestPlatformSettingsWritesAreAtomic(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC)

	// The second entry is invalid, so the first must not be committed either.
	err := db.PutPlatformSettings(ctx, []PlatformSetting{
		{Key: "agentapi.base_url", Value: "https://agent.example.com", UpdatedAt: now},
		{Key: "", Value: "broken", UpdatedAt: now},
	})
	if err == nil {
		t.Fatal("an empty setting key was accepted")
	}
	values, err := db.PlatformSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("a failed batch was partially written: %+v", values)
	}
}

func TestPlatformSettingsEmptyReadsAndIdempotentDeletes(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC)

	values, err := db.PlatformSettings(ctx)
	if err != nil || len(values) != 0 {
		t.Fatalf("empty table: values=%+v err=%v", values, err)
	}
	if err := db.PutPlatformSettings(ctx, nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if err := db.DeletePlatformSettings(ctx); err != nil {
		t.Fatalf("no-op delete: %v", err)
	}
	if err := db.DeletePlatformSettings(ctx, "missing.key"); err != nil {
		t.Fatalf("deleting a missing key: %v", err)
	}

	if err := db.PutPlatformSettings(ctx, []PlatformSetting{
		{Key: "linx.api_key", Value: "enc:v1:ciphertext", IsSecret: true, UpdatedAt: now, UpdatedBy: "usr_admin"},
	}); err != nil {
		t.Fatal(err)
	}
	values, err = db.PlatformSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	setting, ok := values["linx.api_key"]
	if !ok || !setting.IsSecret || setting.Value != "enc:v1:ciphertext" ||
		setting.UpdatedBy != "usr_admin" || !setting.UpdatedAt.Equal(now) {
		t.Fatalf("setting=%+v present=%v", setting, ok)
	}
}
