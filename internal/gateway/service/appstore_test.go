package service

import (
	"context"
	"errors"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

type fakeStoreSource struct {
	items []store.AppPublication
	byID  map[string]store.AppPublication
	err   error
}

func (f *fakeStoreSource) ApprovedPublications(context.Context, string, int) ([]store.AppPublication, error) {
	return f.items, f.err
}

func (f *fakeStoreSource) AppPublicationByID(_ context.Context, appID string) (store.AppPublication, error) {
	if f.err != nil {
		return store.AppPublication{}, f.err
	}
	item, ok := f.byID[appID]
	if !ok {
		return store.AppPublication{}, errors.New("missing")
	}
	return item, nil
}

func toolErrorCode(err error) string {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr.Code
	}
	return ""
}

func TestCloneAppCreatesAnIndependentInstalledCopy(t *testing.T) {
	env := newTestEnv(t)
	artifacts := &fakeArtifacts{}
	env.svc.SetArtifactStore(artifacts)
	source := builtApp(t, "app_src", 0, app.StateIdle)
	env.registry.Add(source)

	result, err := env.svc.CloneApp(context.Background(), ownerFresh, "app_src", "我的答题")
	if err != nil {
		t.Fatalf("CloneApp: %v", err)
	}
	if result.AppID == "" || result.AppID == source.ID || result.State != app.StateIdle {
		t.Fatalf("clone result = %+v", result)
	}
	record, ok := env.registry.Get(result.AppID)
	if !ok {
		t.Fatal("clone is missing from the registry")
	}
	clone := record.Snapshot()
	if clone.InstalledFrom != "app_src" {
		t.Fatalf("InstalledFrom = %q, want app_src", clone.InstalledFrom)
	}
	if clone.Name != "我的答题" {
		t.Fatalf("name = %q", clone.Name)
	}
	if clone.Artifact.Path == source.Artifact.Path || clone.Artifact.SHA256 == "" {
		t.Fatalf("clone shares the source package: %+v", clone.Artifact)
	}
	// The author's own requirement text must not travel with the copy.
	if clone.Prompt == source.Prompt {
		t.Fatalf("clone kept the source prompt: %q", clone.Prompt)
	}
}

func TestUpdateAppRefusesInstalledApplications(t *testing.T) {
	env := newTestEnv(t)
	env.svc.SetArtifactStore(&fakeArtifacts{})
	env.registry.Add(builtApp(t, "app_src", 0, app.StateIdle))

	result, err := env.svc.CloneApp(context.Background(), ownerFresh, "app_src", "副本")
	if err != nil {
		t.Fatalf("CloneApp: %v", err)
	}
	_, err = env.svc.UpdateApp(context.Background(), ownerFresh, result.AppID, "改一下")
	if code := toolErrorCode(err); code != "APP_INSTALLED" {
		t.Fatalf("update-app error = %v (code %q), want APP_INSTALLED", err, code)
	}
}

func TestCloneAppRefusesWhenTheAccountIsFull(t *testing.T) {
	env := newTestEnv(t)
	env.svc.SetArtifactStore(&fakeArtifacts{})
	env.registry.Add(builtApp(t, "app_src", 0, app.StateIdle))
	for index := 0; index < HistoryLimit; index++ {
		value := builtApp(t, "app_own_"+string(rune('a'+index)), index+1, app.StateIdle)
		value.Owner = ownerFresh
		env.registry.Add(value)
	}
	_, err := env.svc.CloneApp(context.Background(), ownerFresh, "app_src", "再来一个")
	if code := toolErrorCode(err); code != "QUOTA_EXCEEDED" {
		t.Fatalf("clone error = %v (code %q), want QUOTA_EXCEEDED", err, code)
	}
}

func TestCloneAppRefusesMissingArtifacts(t *testing.T) {
	env := newTestEnv(t)
	env.svc.SetArtifactStore(&fakeArtifacts{missing: map[string]bool{"app_src": true}})
	env.registry.Add(builtApp(t, "app_src", 0, app.StateIdle))

	_, err := env.svc.CloneApp(context.Background(), ownerFresh, "app_src", "副本")
	if code := toolErrorCode(err); code != "ARTIFACT_MISSING" {
		t.Fatalf("clone error = %v (code %q), want ARTIFACT_MISSING", err, code)
	}
}

func TestInstallStoreAppRequiresAnApprovedEntry(t *testing.T) {
	env := newTestEnv(t)
	env.svc.SetArtifactStore(&fakeArtifacts{})
	env.registry.Add(builtApp(t, "app_src", 0, app.StateIdle))
	source := &fakeStoreSource{byID: map[string]store.AppPublication{
		"app_src": {AppID: "app_src", OwnerUserID: "usr_1", Title: "答题", Status: store.PublicationPending},
	}}
	env.svc.SetStoreSource(source)

	if _, err := env.svc.InstallStoreApp(context.Background(), ownerFresh, "app_src", "副本"); toolErrorCode(err) != "STORE_ITEM_NOT_FOUND" {
		t.Fatalf("pending entry installable: %v", err)
	}

	source.byID["app_src"] = store.AppPublication{AppID: "app_src", OwnerUserID: "usr_1", Title: "答题", Status: store.PublicationApproved}
	result, err := env.svc.InstallStoreApp(context.Background(), ownerFresh, "app_src", "副本")
	if err != nil {
		t.Fatalf("approved install: %v", err)
	}
	if result.State != app.StateIdle {
		t.Fatalf("installed state = %q", result.State)
	}
}
