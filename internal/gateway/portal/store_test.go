package portal

import (
	"time"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

type fakeArtifactStore struct{ has bool }

func (f fakeArtifactStore) Has(string) bool { return f.has }
func (f fakeArtifactStore) Remove(string) error {
	return nil
}
func (f fakeArtifactStore) Copy(fromID, toID string) (app.Artifact, error) {
	return app.Artifact{Path: toID + ".tar.gz", SHA256: "sha-" + fromID, Size: 1}, nil
}

// storeEnv wires the portal to one owned application (`app_1`), one foreign
// application (`app_2`), and an artifact store that reports both as present.
func storeEnv(t *testing.T) (*portalHTTPEnv, *app.Registry) {
	t.Helper()
	env := newPortalHTTPEnv(t)
	owner, err := env.db.PortalUserByUsername(context.Background(), testPortalUsername)
	if err != nil {
		t.Fatalf("PortalUserByUsername: %v", err)
	}
	device := createPortalTestDevice(t, env, "dev_1", owner.ID, "aa:bb:cc:dd:ee:01", "agt_1", store.PortalDeviceRegistered)

	registry := app.NewRegistry()
	for _, value := range []app.App{
		{
			ID:    "app_1",
			Owner: app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID},
			Name:  "答题夜",
			State: app.StateIdle,
		},
		{
			ID:    "app_2",
			Owner: app.Identity{DeviceID: "ff:ff:ff:ff:ff:ff", AgentID: "agt_other"},
			Name:  "别人的应用",
			State: app.StateIdle,
		},
	} {
		registry.Add(value)
		// The publication table references apps(id), so the record must be
		// persisted exactly as the control plane does at runtime.
		if err := env.db.SaveApp(value); err != nil {
			t.Fatalf("SaveApp(%s): %v", value.ID, err)
		}
	}
	apps := service.NewWithBindings(registry, nil, nil, nil, nil, service.Config{})
	apps.SetArtifactStore(fakeArtifactStore{has: true})
	env.api.apps = apps
	return env, registry
}

func decodePublication(t *testing.T, payload []byte) publicationView {
	t.Helper()
	var envelope struct {
		Publication publicationView `json:"publication"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("publication body %q: %v", payload, err)
	}
	return envelope.Publication
}

func TestStorePublishReviewAndListing(t *testing.T) {
	env, _ := storeEnv(t)
	env.login(t)

	response, payload := env.do(t, http.MethodPut, "/api/portal/apps/app_1/publication",
		`{"title":"答题夜","summary":"双人轮流作答"}`, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", response.StatusCode, payload)
	}
	if publication := decodePublication(t, payload); publication.Status != string(store.PublicationPending) {
		t.Fatalf("new publication status = %q, want pending", publication.Status)
	}

	// Pending entries are invisible to other users until a reviewer approves.
	response, payload = env.do(t, http.MethodGet, "/api/portal/store", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("store status=%d body=%s", response.StatusCode, payload)
	}
	var listing struct {
		Items []storeItemView `json:"items"`
	}
	if err := json.Unmarshal(payload, &listing); err != nil {
		t.Fatalf("store body %q: %v", payload, err)
	}
	if len(listing.Items) != 0 {
		t.Fatalf("pending entry is listed: %+v", listing.Items)
	}

	response, payload = env.do(t, http.MethodPost, "/api/admin/store/app_1/review",
		`{"decision":"approve"}`, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("review status=%d body=%s", response.StatusCode, payload)
	}
	if publication := decodePublication(t, payload); publication.Status != string(store.PublicationApproved) {
		t.Fatalf("reviewed status = %q, want approved", publication.Status)
	}

	response, payload = env.do(t, http.MethodGet, "/api/portal/store", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("store status=%d body=%s", response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, &listing); err != nil {
		t.Fatalf("store body %q: %v", payload, err)
	}
	if len(listing.Items) != 1 || listing.Items[0].StoreID != "app_1" || listing.Items[0].Title != "答题夜" {
		t.Fatalf("approved entry missing: %+v", listing.Items)
	}
	if listing.Items[0].OperationCount != 0 || listing.Items[0].AppName != "答题夜" {
		t.Fatalf("store item facts = %+v", listing.Items[0])
	}
}

func TestStorePublishRejectsMissingTitleAndForeignApps(t *testing.T) {
	env, _ := storeEnv(t)
	env.login(t)

	response, payload := env.do(t, http.MethodPut, "/api/portal/apps/app_1/publication", `{"summary":"没有标题"}`, nil)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("empty title = %d %s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodPut, "/api/portal/apps/app_2/publication",
		`{"title":"别人的应用"}`, nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("publishing a foreign app = %d %s, want 404", response.StatusCode, payload)
	}
}

func TestStorePublishRefusesBuildingApps(t *testing.T) {
	env, _ := storeEnv(t)
	env.login(t)

	registry := app.NewRegistry()
	registry.Add(app.App{
		ID:    "app_building",
		Owner: app.Identity{DeviceID: "aa:bb:cc:dd:ee:01", AgentID: "agt_1"},
		State: app.StateBuilding,
	})
	apps := service.NewWithBindings(registry, nil, nil, nil, nil, service.Config{})
	apps.SetArtifactStore(fakeArtifactStore{has: true})
	env.api.apps = apps

	response, payload := env.do(t, http.MethodPut, "/api/portal/apps/app_building/publication",
		`{"title":"还在做"}`, nil)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "APP_BUILDING" {
		t.Fatalf("building publish = %d %s", response.StatusCode, payload)
	}
}

func TestAdminReviewUnknownEntryIsNotFound(t *testing.T) {
	env, _ := storeEnv(t)
	env.login(t)

	response, payload := env.do(t, http.MethodPost, "/api/admin/store/app_missing/review",
		`{"decision":"approve"}`, nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("review unknown = %d %s, want 404", response.StatusCode, payload)
	}
}

func TestStoreUnpublishHidesTheEntry(t *testing.T) {
	env, _ := storeEnv(t)
	env.login(t)

	if response, payload := env.do(t, http.MethodPut, "/api/portal/apps/app_1/publication",
		`{"title":"答题夜"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("publish = %d %s", response.StatusCode, payload)
	}
	if response, payload := env.do(t, http.MethodPost, "/api/admin/store/app_1/review",
		`{"decision":"approve"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("review = %d %s", response.StatusCode, payload)
	}
	// The same-origin guard requires a JSON content type on writes, so the
	// portal sends an empty object the way the frontend does.
	if response, payload := env.do(t, http.MethodDelete, "/api/portal/apps/app_1/publication", `{}`, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("unpublish = %d %s", response.StatusCode, payload)
	}
	response, payload := env.do(t, http.MethodGet, "/api/portal/store", "", nil)
	var listing struct {
		Items []storeItemView `json:"items"`
	}
	if err := json.Unmarshal(payload, &listing); err != nil {
		t.Fatalf("store body %q: %v", payload, err)
	}
	if len(listing.Items) != 0 {
		t.Fatalf("unpublished entry still listed: %+v", listing.Items)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("store status = %d", response.StatusCode)
	}
}

func TestStoreCloneInstallsOntoTheChosenDevice(t *testing.T) {
	env, registry := storeEnv(t)
	env.login(t)

	if response, payload := env.do(t, http.MethodPut, "/api/portal/apps/app_1/publication", `{"title":"答题夜"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("publish = %d %s", response.StatusCode, payload)
	}
	if response, payload := env.do(t, http.MethodPost, "/api/admin/store/app_1/review", `{"decision":"approve"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("review = %d %s", response.StatusCode, payload)
	}

	response, payload := env.do(t, http.MethodPost, "/api/portal/store/app_1/clone",
		`{"name":"我的答题","device_id":"dev_1"}`, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("clone = %d %s", response.StatusCode, payload)
	}
	var cloned struct {
		AppID string `json:"app_id"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(payload, &cloned); err != nil {
		t.Fatalf("clone body %q: %v", payload, err)
	}
	if cloned.AppID == "" || cloned.AppID == "app_1" || cloned.State != "idle" {
		t.Fatalf("clone = %+v", cloned)
	}
	record, ok := registry.Get(cloned.AppID)
	if !ok {
		t.Fatal("clone not in the registry")
	}
	installed := record.Snapshot()
	if installed.InstalledFrom != "app_1" || installed.Name != "我的答题" {
		t.Fatalf("installed record = %+v", installed)
	}
	if installed.Owner.DeviceID != "aa:bb:cc:dd:ee:01" || installed.Owner.AgentID != "agt_1" {
		t.Fatalf("installed owner = %+v", installed.Owner)
	}
}

func TestStoreCloneRequiresAnApprovedEntryAndAnOwnedDevice(t *testing.T) {
	env, _ := storeEnv(t)
	env.login(t)

	// Nothing has been reviewed yet.
	response, payload := env.do(t, http.MethodPost, "/api/portal/store/app_1/clone",
		`{"name":"我的答题","device_id":"dev_1"}`, nil)
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "STORE_ITEM_NOT_FOUND" {
		t.Fatalf("clone of an unreviewed entry = %d %s", response.StatusCode, payload)
	}

	if response, payload := env.do(t, http.MethodPut, "/api/portal/apps/app_1/publication", `{"title":"答题夜"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("publish = %d %s", response.StatusCode, payload)
	}
	if response, payload := env.do(t, http.MethodPost, "/api/admin/store/app_1/review", `{"decision":"approve"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("review = %d %s", response.StatusCode, payload)
	}

	// The device is required: installation is per device, not per account.
	response, payload = env.do(t, http.MethodPost, "/api/portal/store/app_1/clone", `{"name":"我的答题"}`, nil)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("clone without a device = %d %s", response.StatusCode, payload)
	}

	// A device of another account is not a valid target.
	other := store.PortalUser{
		ID: "usr_other", Username: "bob", PasswordHash: "x",
		Role: store.PortalRoleUser, Status: store.PortalUserActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := env.db.CreatePortalUser(context.Background(), other); err != nil {
		t.Fatalf("CreatePortalUser: %v", err)
	}
	createPortalTestDevice(t, env, "dev_other", other.ID, "ff:ff:ff:ff:ff:ff", "agt_other", store.PortalDeviceRegistered)
	response, payload = env.do(t, http.MethodPost, "/api/portal/store/app_1/clone",
		`{"name":"我的答题","device_id":"dev_other"}`, nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("clone onto a foreign device = %d %s", response.StatusCode, payload)
	}
}
