package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

func newPublicationDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "store", "gateway.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func saveApp(t *testing.T, db *DB, id string) {
	t.Helper()
	if err := db.SaveApp(app.App{ID: id, Owner: app.Identity{DeviceID: "dev", SessionID: "ses"}}); err != nil {
		t.Fatalf("SaveApp(%s): %v", id, err)
	}
}

func TestPublishAppStartsPendingAndOnlyApprovedIsVisible(t *testing.T) {
	db := newPublicationDB(t)
	ctx := context.Background()
	saveApp(t, db, "app_1")

	if err := db.PublishApp(ctx, AppPublication{AppID: "app_1", OwnerUserID: "usr_1", Title: "答题夜", Summary: "双人答题"}); err != nil {
		t.Fatalf("PublishApp: %v", err)
	}
	item, err := db.AppPublicationByID(ctx, "app_1")
	if err != nil {
		t.Fatalf("AppPublicationByID: %v", err)
	}
	if item.Status != PublicationPending {
		t.Fatalf("new publication status = %q, want pending", item.Status)
	}

	visible, err := db.ApprovedPublications(ctx, "", 20)
	if err != nil {
		t.Fatalf("ApprovedPublications: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("pending entry is visible: %+v", visible)
	}

	if err := db.ReviewPublication(ctx, "app_1", PublicationApproved, "usr_admin", ""); err != nil {
		t.Fatalf("ReviewPublication: %v", err)
	}
	visible, err = db.ApprovedPublications(ctx, "", 20)
	if err != nil {
		t.Fatalf("ApprovedPublications: %v", err)
	}
	if len(visible) != 1 || visible[0].AppID != "app_1" {
		t.Fatalf("approved entry missing: %+v", visible)
	}
}

// Editing an approved entry must send it back to review: the reviewer approved
// the text that was there, not whatever replaced it.
func TestPublishingAgainReturnsToPending(t *testing.T) {
	db := newPublicationDB(t)
	ctx := context.Background()
	saveApp(t, db, "app_1")

	saveApp(t, db, "app_1")
	if err := db.PublishApp(ctx, AppPublication{AppID: "app_1", OwnerUserID: "usr_1", Title: "旧标题"}); err != nil {
		t.Fatalf("PublishApp: %v", err)
	}
	if err := db.ReviewPublication(ctx, "app_1", PublicationApproved, "usr_admin", ""); err != nil {
		t.Fatalf("ReviewPublication: %v", err)
	}
	if err := db.PublishApp(ctx, AppPublication{AppID: "app_1", OwnerUserID: "usr_1", Title: "新标题"}); err != nil {
		t.Fatalf("republish: %v", err)
	}
	item, err := db.AppPublicationByID(ctx, "app_1")
	if err != nil {
		t.Fatalf("AppPublicationByID: %v", err)
	}
	if item.Status != PublicationPending || item.Title != "新标题" || item.ReviewedBy != "" {
		t.Fatalf("republished entry = %+v, want pending with cleared review", item)
	}
}

func TestRejectedEntriesStayOutOfTheStore(t *testing.T) {
	db := newPublicationDB(t)
	ctx := context.Background()
	saveApp(t, db, "app_1")

	saveApp(t, db, "app_1")
	if err := db.PublishApp(ctx, AppPublication{AppID: "app_1", OwnerUserID: "usr_1", Title: "不合适"}); err != nil {
		t.Fatalf("PublishApp: %v", err)
	}
	if err := db.ReviewPublication(ctx, "app_1", PublicationRejected, "usr_admin", "内容不合适"); err != nil {
		t.Fatalf("ReviewPublication: %v", err)
	}
	visible, err := db.ApprovedPublications(ctx, "", 20)
	if err != nil || len(visible) != 0 {
		t.Fatalf("rejected entry visible: %+v err=%v", visible, err)
	}
	own, err := db.PublicationsByOwner(ctx, "usr_1")
	if err != nil {
		t.Fatalf("PublicationsByOwner: %v", err)
	}
	if len(own) != 1 || own[0].Status != PublicationRejected || own[0].ReviewNote == "" {
		t.Fatalf("author cannot see the rejection: %+v", own)
	}
}

func TestApprovedPublicationsMatchTitleSummaryAndEscapeWildcards(t *testing.T) {
	db := newPublicationDB(t)
	ctx := context.Background()
	saveApp(t, db, "app_1")
	saveApp(t, db, "app_2")
	for _, item := range []AppPublication{
		{AppID: "app_1", OwnerUserID: "usr_1", Title: "答题夜", Summary: "双人答题"},
		{AppID: "app_2", OwnerUserID: "usr_2", Title: "计分板", Summary: "户外 50% 亮度"},
	} {
		if err := db.PublishApp(ctx, item); err != nil {
			t.Fatalf("PublishApp: %v", err)
		}
		if err := db.ReviewPublication(ctx, item.AppID, PublicationApproved, "usr_admin", ""); err != nil {
			t.Fatalf("ReviewPublication: %v", err)
		}
	}
	cases := []struct {
		query string
		want  []string
	}{
		{query: "答题", want: []string{"app_1"}},
		{query: "计分", want: []string{"app_2"}},
		{query: "%", want: []string{"app_2"}}, // a literal percent, not a wildcard
		{query: "", want: []string{"app_2", "app_1"}}, // newest first
	}
	for _, tc := range cases {
		items, err := db.ApprovedPublications(ctx, tc.query, 20)
		if err != nil {
			t.Fatalf("ApprovedPublications(%q): %v", tc.query, err)
		}
		var got []string
		for _, item := range items {
			got = append(got, item.AppID)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("query %q = %v, want %v", tc.query, got, tc.want)
		}
		for index := range got {
			if got[index] != tc.want[index] {
				t.Fatalf("query %q = %v, want %v", tc.query, got, tc.want)
			}
		}
	}
}

func TestUnpublishAndDeleteRemoveTheListing(t *testing.T) {
	db := newPublicationDB(t)
	ctx := context.Background()
	saveApp(t, db, "app_1")

	saveApp(t, db, "app_1")
	if err := db.PublishApp(ctx, AppPublication{AppID: "app_1", OwnerUserID: "usr_1", Title: "答题"}); err != nil {
		t.Fatalf("PublishApp: %v", err)
	}
	if err := db.UnpublishApp(ctx, "app_1"); err != nil {
		t.Fatalf("UnpublishApp: %v", err)
	}
	if _, err := db.AppPublicationByID(ctx, "app_1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unpublished entry still readable: %v", err)
	}

	saveApp(t, db, "app_2")
	if err := db.PublishApp(ctx, AppPublication{AppID: "app_2", OwnerUserID: "usr_1", Title: "计分"}); err != nil {
		t.Fatalf("PublishApp: %v", err)
	}
	if err := db.DeletePublication(ctx, "app_2"); err != nil {
		t.Fatalf("DeletePublication: %v", err)
	}
	if _, err := db.AppPublicationByID(ctx, "app_2"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted entry still readable: %v", err)
	}
}

func TestReviewUnknownEntryReportsNoRows(t *testing.T) {
	db := newPublicationDB(t)
	if err := db.ReviewPublication(context.Background(), "app_missing", PublicationApproved, "usr_admin", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ReviewPublication error = %v, want sql.ErrNoRows", err)
	}
}

// A caller cannot publish straight into approved: only ReviewPublication
// decides what other users see.
func TestPublishAppCannotSelfApprove(t *testing.T) {
	db := newPublicationDB(t)
	ctx := context.Background()

	saveApp(t, db, "app_1")
	if err := db.PublishApp(ctx, AppPublication{
		AppID: "app_1", OwnerUserID: "usr_1", Title: "绕过审核", Status: PublicationApproved,
	}); err != nil {
		t.Fatalf("PublishApp: %v", err)
	}
	item, err := db.AppPublicationByID(ctx, "app_1")
	if err != nil {
		t.Fatalf("AppPublicationByID: %v", err)
	}
	if item.Status != PublicationPending {
		t.Fatalf("published status = %q, want pending", item.Status)
	}
}

// Deleting an application must take its listing with it. The foreign key does
// this without the service layer remembering to call back.
func TestDeletingAnAppRemovesItsPublication(t *testing.T) {
	db := newPublicationDB(t)
	ctx := context.Background()

	if err := db.SaveApp(app.App{ID: "app_1", Owner: app.Identity{DeviceID: "dev", SessionID: "ses"}}); err != nil {
		t.Fatalf("SaveApp: %v", err)
	}
	if err := db.PublishApp(ctx, AppPublication{AppID: "app_1", OwnerUserID: "usr_1", Title: "答题"}); err != nil {
		t.Fatalf("PublishApp: %v", err)
	}
	if err := db.DeleteApp("app_1"); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	if _, err := db.AppPublicationByID(ctx, "app_1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("publication survived the app deletion: %v", err)
	}
}

// A listing cannot exist without its application.
func TestPublicationRequiresAnExistingApp(t *testing.T) {
	db := newPublicationDB(t)
	err := db.PublishApp(context.Background(), AppPublication{AppID: "app_missing", OwnerUserID: "usr_1", Title: "答题"})
	if err == nil {
		t.Fatal("publishing an app that does not exist succeeded")
	}
}
