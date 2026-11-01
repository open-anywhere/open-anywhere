package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PublicationStatus is the review state of one app store entry. Only approved
// entries are visible to other users; a pending or rejected entry exists so its
// author can see what happened to it.
type PublicationStatus string

const (
	PublicationPending  PublicationStatus = "pending"
	PublicationApproved PublicationStatus = "approved"
	PublicationRejected PublicationStatus = "rejected"
)

// AppPublication is the store listing of one application. The application
// itself stays in apps; this row carries only what the author chose to publish
// and the reviewer's decision.
type AppPublication struct {
	AppID       string
	OwnerUserID string
	Title       string
	Summary     string
	Status      PublicationStatus
	ReviewNote  string
	ReviewedBy  string
	PublishedAt time.Time
	UpdatedAt   time.Time
}

const selectPublication = `SELECT app_id, owner_user_id, title, summary, status,
	review_note, reviewed_by, published_at, updated_at FROM app_publications`

func scanPublication(row interface{ Scan(...any) error }) (AppPublication, error) {
	var (
		item        AppPublication
		publishedAt string
		updatedAt   string
	)
	if err := row.Scan(&item.AppID, &item.OwnerUserID, &item.Title, &item.Summary,
		&item.Status, &item.ReviewNote, &item.ReviewedBy, &publishedAt, &updatedAt); err != nil {
		return AppPublication{}, err
	}
	var err error
	if item.PublishedAt, err = time.Parse(time.RFC3339Nano, publishedAt); err != nil {
		return AppPublication{}, fmt.Errorf("store: parse published_at: %w", err)
	}
	if item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return AppPublication{}, fmt.Errorf("store: parse updated_at: %w", err)
	}
	return item, nil
}

// PublishApp creates or replaces the listing of one application. Every write
// lands in pending: an author cannot approve their own entry, and any edit
// returns an approved one to review because the reviewer approved the text
// that was there, not what it became.
func (d *DB) PublishApp(ctx context.Context, item AppPublication) error {
	if strings.TrimSpace(item.AppID) == "" || strings.TrimSpace(item.OwnerUserID) == "" {
		return errors.New("store: publication needs an app and an owner")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.ExecContext(ctx, `INSERT INTO app_publications
		(app_id, owner_user_id, title, summary, status, review_note, reviewed_by, published_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', '', ?, ?)
		ON CONFLICT(app_id) DO UPDATE SET
			title = excluded.title,
			summary = excluded.summary,
			status = excluded.status,
			review_note = '',
			reviewed_by = '',
			published_at = excluded.published_at,
			updated_at = excluded.updated_at`,
		item.AppID, item.OwnerUserID, item.Title, item.Summary, PublicationPending, now, now)
	if err != nil {
		return fmt.Errorf("store: publish app: %w", err)
	}
	return nil
}

// UnpublishApp removes the listing. The application itself is untouched, as
// are copies other users already made.
func (d *DB) UnpublishApp(ctx context.Context, appID string) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM app_publications WHERE app_id = ?`, appID); err != nil {
		return fmt.Errorf("store: unpublish app: %w", err)
	}
	return nil
}

// AppPublicationByID returns one listing.
func (d *DB) AppPublicationByID(ctx context.Context, appID string) (AppPublication, error) {
	item, err := scanPublication(d.sql.QueryRowContext(ctx, selectPublication+` WHERE app_id = ?`, appID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AppPublication{}, err
		}
		return AppPublication{}, fmt.Errorf("store: read publication: %w", err)
	}
	return item, nil
}

// ApprovedPublications lists what other users may see, newest first. query
// matches the title or the summary; an empty query lists everything.
func (d *DB) ApprovedPublications(ctx context.Context, query string, limit int) ([]AppPublication, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	statement := selectPublication + ` WHERE status = ?`
	args := []any{PublicationApproved}
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		pattern := "%" + escapeLike(trimmed) + "%"
		statement += ` AND (title LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\')`
		args = append(args, pattern, pattern)
	}
	statement += ` ORDER BY published_at DESC, app_id LIMIT ?`
	args = append(args, limit)
	return d.queryPublications(ctx, statement, args...)
}

// PublicationsByOwner lists one author's entries, including rejected ones.
func (d *DB) PublicationsByOwner(ctx context.Context, userID string) ([]AppPublication, error) {
	return d.queryPublications(ctx, selectPublication+` WHERE owner_user_id = ? ORDER BY updated_at DESC, app_id`, userID)
}

// AllPublications lists every entry for the reviewer's screen. Pending entries
// come first so the queue is obvious.
func (d *DB) AllPublications(ctx context.Context) ([]AppPublication, error) {
	return d.queryPublications(ctx, selectPublication+`
		ORDER BY CASE status WHEN 'pending' THEN 0 WHEN 'approved' THEN 1 ELSE 2 END,
			updated_at DESC, app_id`)
}

// ReviewPublication records the reviewer's decision on one entry.
func (d *DB) ReviewPublication(ctx context.Context, appID string, status PublicationStatus, reviewerID, note string) error {
	if status != PublicationApproved && status != PublicationRejected && status != PublicationPending {
		return fmt.Errorf("store: invalid review status %q", status)
	}
	result, err := d.sql.ExecContext(ctx, `UPDATE app_publications
		SET status = ?, review_note = ?, reviewed_by = ?, updated_at = ?
		WHERE app_id = ?`,
		status, note, reviewerID, time.Now().UTC().Format(time.RFC3339Nano), appID)
	if err != nil {
		return fmt.Errorf("store: review publication: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: review publication: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeletePublication drops the listing of an application that no longer exists.
func (d *DB) DeletePublication(ctx context.Context, appID string) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM app_publications WHERE app_id = ?`, appID); err != nil {
		return fmt.Errorf("store: delete publication: %w", err)
	}
	return nil
}

func (d *DB) queryPublications(ctx context.Context, statement string, args ...any) ([]AppPublication, error) {
	rows, err := d.sql.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list publications: %w", err)
	}
	defer rows.Close()
	var items []AppPublication
	for rows.Next() {
		item, err := scanPublication(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan publication: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list publications: %w", err)
	}
	return items, nil
}

// escapeLike keeps a user's query from acting as a wildcard pattern.
func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
