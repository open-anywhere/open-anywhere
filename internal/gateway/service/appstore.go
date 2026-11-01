package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// StoreSource is the read side of the app store the control plane needs: what
// other users may see, and one entry by id. It is implemented by the portal's
// database so the MCP tools and the portal share one definition of "public".
type StoreSource interface {
	ApprovedPublications(ctx context.Context, query string, limit int) ([]store.AppPublication, error)
	AppPublicationByID(ctx context.Context, appID string) (store.AppPublication, error)
}

// SetStoreSource lets the application service resolve store entries.
func (s *Service) SetStoreSource(source StoreSource) { s.store = source }

// StoreItemView is one store entry as the voice tools see it. It carries only
// what the author published; the prompt and the package address stay private.
type StoreItemView struct {
	StoreID        string `json:"store_id"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	Name           string `json:"name,omitempty"`
	OperationCount int    `json:"operation_count"`
}

// ListStoreApps returns the approved entries matching query.
func (s *Service) ListStoreApps(ctx context.Context, query string, limit int) ([]StoreItemView, error) {
	if s.store == nil {
		return nil, &ToolError{Code: "STORE_UNAVAILABLE", Message: "应用商城暂不可用"}
	}
	items, err := s.store.ApprovedPublications(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]StoreItemView, 0, len(items))
	for _, item := range items {
		view := StoreItemView{StoreID: item.AppID, Title: item.Title, Summary: item.Summary}
		if record, ok := s.apps.Get(item.AppID); ok {
			value := record.Snapshot()
			view.Name = value.Name
			if value.OpenAPI != nil {
				view.OperationCount = len(value.OpenAPI.Operations())
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// InstallStoreApp copies one approved store application into owner's account.
// It is the MCP-facing entry point: it also checks that the entry is public.
func (s *Service) InstallStoreApp(ctx context.Context, owner app.Identity, storeID, name string) (CreateResult, error) {
	if s.store == nil {
		return CreateResult{}, &ToolError{Code: "STORE_UNAVAILABLE", Message: "应用商城暂不可用"}
	}
	publication, err := s.store.AppPublicationByID(ctx, storeID)
	if err != nil || publication.Status != store.PublicationApproved {
		return CreateResult{}, &ToolError{Code: "STORE_ITEM_NOT_FOUND", Message: "商城里没有这个应用"}
	}
	return s.CloneApp(ctx, owner, publication.AppID, name)
}

// CloneApp copies one application's package into owner's account as a new idle
// application. The source does not have to belong to the caller: the portal and
// the store tool check visibility before calling this.
func (s *Service) CloneApp(ctx context.Context, owner app.Identity, sourceID, name string) (CreateResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CreateResult{}, &ToolError{Code: "INVALID_ARGUMENT", Message: "应用名字不能为空"}
	}
	if utf8.RuneCountInString(name) > 40 {
		return CreateResult{}, &ToolError{Code: "INVALID_ARGUMENT", Message: "应用名字太长（最多 40 个字）"}
	}
	record, ok := s.apps.Get(sourceID)
	if !ok {
		return CreateResult{}, &ToolError{Code: "STORE_ITEM_NOT_FOUND", Message: "商城里没有这个应用"}
	}
	if s.artifacts == nil || !s.artifacts.Has(sourceID) {
		return CreateResult{}, &ToolError{Code: "ARTIFACT_MISSING", Message: "应用产物已丢失，无法添加"}
	}
	if s.countOwned(owner) >= HistoryLimit {
		return CreateResult{}, &ToolError{Code: "QUOTA_EXCEEDED", Message: "应用数量已达上限，请先删除一个"}
	}
	source := record.Snapshot()
	id := app.NewID()
	snapshot := app.App{
		ID:            id,
		Owner:         owner,
		Name:          name,
		Prompt:        "从应用商城复制：" + displayName(source),
		State:         app.StateIdle,
		OpenAPI:       source.OpenAPI,
		InstalledFrom: sourceID,
	}
	s.apps.Add(snapshot)
	copied, err := s.artifacts.Copy(sourceID, id)
	if err != nil {
		s.apps.Remove(id)
		return CreateResult{}, &ToolError{Code: "ARTIFACT_MISSING", Message: "应用产物已丢失，无法添加"}
	}
	if err := s.updateRuntime(id, func(value *app.App) {
		value.Artifact = copied
		value.BuildFinishedAt = s.now()
	}); err != nil {
		s.apps.Remove(id)
		_ = s.artifacts.Remove(id)
		return CreateResult{}, &ToolError{Code: "PERSISTENCE_FAILED", Message: "无法保存应用，请稍后重试"}
	}
	s.PruneHistory()
	return CreateResult{AppID: id, State: app.StateIdle}, nil
}

func displayName(value app.App) string {
	if strings.TrimSpace(value.Name) != "" {
		return value.Name
	}
	return "未命名应用"
}

func (s *Service) countOwned(owner app.Identity) int {
	count := 0
	for _, value := range s.apps.List() {
		if value.Owner.SameOwner(owner) {
			count++
		}
	}
	return count
}
