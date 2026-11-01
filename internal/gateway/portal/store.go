package portal

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// Publication limits. Titles are shown as cards; summaries are one sentence.
const (
	publicationTitleRunes   = 40
	publicationSummaryRunes = 200
)

// storeItemView is one entry visible to other users. It deliberately carries
// only what the author chose to publish plus the application's public shape:
// never the original prompt, the player list, or a package address.
type storeItemView struct {
	StoreID        string    `json:"store_id"`
	Title          string    `json:"title"`
	Summary        string    `json:"summary"`
	AppName        string    `json:"app_name,omitempty"`
	OperationCount int       `json:"operation_count"`
	PublishedAt    time.Time `json:"published_at"`
}

// publicationView is what the author and the reviewer see: the store entry
// plus its review state.
type publicationView struct {
	storeItemView
	Status     string    `json:"status"`
	ReviewNote string    `json:"review_note,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type adminStoreItemView struct {
	publicationView
	OwnerUserID string `json:"owner_user_id"`
	Username    string `json:"username,omitempty"`
}

type publicationRequest struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

type reviewRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

// appFacts is the part of an application the store may show.
type appFacts struct {
	name           string
	operationCount int
}

// publishApp creates or updates the store entry of one of the user's
// applications. Every save lands in review, including an edit of an entry that
// was already approved.
func (a *API) publishApp(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	item, err := a.ownedApp(r, user, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	var in publicationRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	title, summary, err := cleanPublication(in)
	if err != nil {
		writeError(w, err)
		return
	}
	if item.State == app.StateBuilding {
		writeError(w, apiError(http.StatusConflict, "APP_BUILDING", "应用还在制作中，完成后再公开"))
		return
	}
	if a.apps == nil || !a.apps.HasArtifact(item.AppID) {
		writeError(w, apiError(http.StatusConflict, "ARTIFACT_MISSING", "应用产物已丢失，无法公开"))
		return
	}
	publication := store.AppPublication{
		AppID:       item.AppID,
		OwnerUserID: user.ID,
		Title:       title,
		Summary:     summary,
	}
	if err := a.db.PublishApp(r.Context(), publication); err != nil {
		writeError(w, err)
		return
	}
	saved, err := a.db.AppPublicationByID(r.Context(), item.AppID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publication": a.publicationView(r.Context(), saved)})
}

// unpublishApp withdraws the entry. Copies other users already made keep
// working, and the application itself is untouched.
func (a *API) unpublishApp(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	item, err := a.ownedApp(r, user, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := a.db.UnpublishApp(r.Context(), item.AppID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listStore returns what other users may see: approved entries only.
func (a *API) listStore(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	query := r.URL.Query().Get("query")
	limit := queryInt(r, "limit", 50, 100)
	items, err := a.db.ApprovedPublications(r.Context(), query, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	facts := a.storeFacts(r.Context())
	out := make([]storeItemView, 0, len(items))
	for _, item := range items {
		out = append(out, storeItemFrom(item, facts))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type cloneRequest struct {
	Name     string `json:"name"`
	DeviceID string `json:"device_id"`
}

// cloneStoreApp copies an approved store application onto one of the user's
// devices. The device is chosen by the caller: ownership stays per device.
func (a *API) cloneStoreApp(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	if a.apps == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "应用商城暂不可用"))
		return
	}
	publication, err := a.db.AppPublicationByID(r.Context(), r.PathValue("id"))
	if err != nil || publication.Status != store.PublicationApproved {
		writeError(w, apiError(http.StatusNotFound, "STORE_ITEM_NOT_FOUND", "商城里没有这个应用"))
		return
	}
	var in cloneRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(in.DeviceID) == "" {
		writeError(w, invalidArgument("请选择要安装到哪台设备"))
		return
	}
	device, err := a.ownedDevice(r, user, in.DeviceID)
	if err != nil {
		writeError(w, err)
		return
	}
	if device.State != store.PortalDeviceRegistered || device.LinxAgentID == "" {
		writeError(w, apiError(http.StatusConflict, "DEVICE_NOT_READY", "这台设备还没有完成开通"))
		return
	}
	result, err := a.apps.CloneApp(r.Context(),
		app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID},
		publication.AppID, in.Name)
	if err != nil {
		writeError(w, mapServiceError(err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"app_id": result.AppID,
		"name":   strings.TrimSpace(in.Name),
		"state":  result.State,
	})
}

// listMyPublications shows the author every entry including rejected ones, so
// they can see why something did not go live.
func (a *API) listMyPublications(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	items, err := a.db.PublicationsByOwner(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	facts := a.storeFacts(r.Context())
	out := make([]publicationView, 0, len(items))
	for _, item := range items {
		out = append(out, a.publicationViewWith(item, facts))
	}
	writeJSON(w, http.StatusOK, map[string]any{"publications": out})
}

// adminListStore shows the review queue. Pending entries come first.
func (a *API) adminListStore(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	items, err := a.db.AllPublications(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	facts := a.storeFacts(r.Context())
	usernames := map[string]string{}
	out := make([]adminStoreItemView, 0, len(items))
	for _, item := range items {
		username, ok := usernames[item.OwnerUserID]
		if !ok {
			if owner, err := a.db.PortalUserByID(r.Context(), item.OwnerUserID); err == nil {
				username = owner.Username
			}
			usernames[item.OwnerUserID] = username
		}
		out = append(out, adminStoreItemView{
			publicationView: a.publicationViewWith(item, facts),
			OwnerUserID:     item.OwnerUserID,
			Username:        username,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// adminReviewStore records the reviewer's decision. Approving publishes the
// entry; rejecting keeps it out of the store and shows the note to the author.
func (a *API) adminReviewStore(w http.ResponseWriter, r *http.Request, actor store.PortalUser) {
	var in reviewRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	var status store.PublicationStatus
	switch strings.TrimSpace(in.Decision) {
	case "approve":
		status = store.PublicationApproved
	case "reject":
		status = store.PublicationRejected
	default:
		writeError(w, invalidArgument("decision 只能是 approve 或 reject"))
		return
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > publicationSummaryRunes {
		writeError(w, invalidArgument("审核意见太长"))
		return
	}
	appID := r.PathValue("id")
	if err := a.db.ReviewPublication(r.Context(), appID, status, actor.ID, note); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, errNotFound)
			return
		}
		writeError(w, err)
		return
	}
	saved, err := a.db.AppPublicationByID(r.Context(), appID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publication": a.publicationView(r.Context(), saved)})
}

func (a *API) publicationView(ctx context.Context, item store.AppPublication) publicationView {
	return a.publicationViewWith(item, a.storeFacts(ctx))
}

func (a *API) publicationViewWith(item store.AppPublication, facts map[string]appFacts) publicationView {
	return publicationView{
		storeItemView: storeItemFrom(item, facts),
		Status:        string(item.Status),
		ReviewNote:    item.ReviewNote,
		UpdatedAt:     item.UpdatedAt,
	}
}

func storeItemFrom(item store.AppPublication, facts map[string]appFacts) storeItemView {
	view := storeItemView{
		StoreID:     item.AppID,
		Title:       item.Title,
		Summary:     item.Summary,
		PublishedAt: item.PublishedAt,
	}
	if fact, ok := facts[item.AppID]; ok {
		view.AppName = fact.name
		view.OperationCount = fact.operationCount
	}
	return view
}

// storeFacts maps an application id to the fields the store may show. Only the
// name and the operation count leave this function: the service's own summary
// is derived from the prompt and must not reach other users.
func (a *API) storeFacts(ctx context.Context) map[string]appFacts {
	facts := map[string]appFacts{}
	if a.apps == nil {
		return facts
	}
	for _, item := range a.apps.ListAllApps(ctx) {
		facts[item.AppID] = appFacts{
			name:           item.Name,
			operationCount: item.OperationCount,
		}
	}
	return facts
}

// cleanPublication validates the author's text. Title is required; summary may
// be empty and is shown as "—" by the portal.
func cleanPublication(in publicationRequest) (string, string, error) {
	title := strings.TrimSpace(in.Title)
	summary := strings.TrimSpace(in.Summary)
	if title == "" {
		return "", "", invalidArgument("标题不能为空")
	}
	if utf8.RuneCountInString(title) > publicationTitleRunes {
		return "", "", invalidArgument("标题太长（最多 40 个字）")
	}
	if utf8.RuneCountInString(summary) > publicationSummaryRunes {
		return "", "", invalidArgument("简介太长（最多 200 个字）")
	}
	return title, summary, nil
}
