package store

import (
	"context"
	"fmt"
	"time"
)

// Service binding modes. A managed account uses the platform's default Render
// and Casting services; a custom account brings its own public addresses.
const (
	ServiceModeManaged = "managed"
	ServiceModeCustom  = "custom"
)

// PortalServiceBinding is a user's Render/Casting choice. Tokens are stored
// encrypted by the settings layer; nothing here ever decrypts them.
type PortalServiceBinding struct {
	UserID          string    `json:"user_id"`
	Mode            string    `json:"mode"`
	RenderURL       string    `json:"render_url"`
	CastingURL      string    `json:"casting_url"`
	RenderTokenEnc  string    `json:"-"`
	CastingTokenEnc string    `json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ServiceBindingByUser returns the user's binding. A user without a row has
// never chosen, and is treated as managed by the caller. sql.ErrNoRows is
// returned unchanged so callers can tell those cases apart.
func (d *DB) ServiceBindingByUser(ctx context.Context, userID string) (PortalServiceBinding, error) {
	if userID == "" {
		return PortalServiceBinding{}, fmt.Errorf("store: service binding user id is required")
	}
	var (
		binding              PortalServiceBinding
		createdAt, updatedAt string
	)
	err := d.sql.QueryRowContext(ctx, `SELECT user_id, mode, render_url, casting_url,
		render_token_enc, casting_token_enc, created_at, updated_at
		FROM portal_service_bindings WHERE user_id = ?`, userID).
		Scan(&binding.UserID, &binding.Mode, &binding.RenderURL, &binding.CastingURL,
			&binding.RenderTokenEnc, &binding.CastingTokenEnc, &createdAt, &updatedAt)
	if err != nil {
		return PortalServiceBinding{}, err
	}
	if binding.CreatedAt, err = parseTime(createdAt); err != nil {
		return PortalServiceBinding{}, fmt.Errorf("store: decode service binding time: %w", err)
	}
	if binding.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return PortalServiceBinding{}, fmt.Errorf("store: decode service binding time: %w", err)
	}
	return binding, nil
}

// HasEncryptedSecrets reports whether platform settings or service bindings
// already hold ciphertext. Startup uses it to avoid generating a new master key
// over an encrypted database.
func (d *DB) HasEncryptedSecrets(ctx context.Context) (bool, error) {
	var found int
	err := d.sql.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM platform_settings WHERE is_secret = 1 AND value LIKE 'enc:v1:%'
		UNION ALL
		SELECT 1 FROM portal_service_bindings
		WHERE render_token_enc LIKE 'enc:v1:%' OR casting_token_enc LIKE 'enc:v1:%'
	)`).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("store: check encrypted secrets: %w", err)
	}
	return found == 1, nil
}

// ServiceBindingSecrets lists bindings that hold at least one encrypted token.
// Startup validation uses it to prove the configured master key can open every
// stored credential, without loading addresses or plaintext.
func (d *DB) ServiceBindingSecrets(ctx context.Context) ([]PortalServiceBinding, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT user_id, render_token_enc, casting_token_enc
		FROM portal_service_bindings
		WHERE render_token_enc <> '' OR casting_token_enc <> ''`)
	if err != nil {
		return nil, fmt.Errorf("store: list service binding secrets: %w", err)
	}
	defer rows.Close()
	var out []PortalServiceBinding
	for rows.Next() {
		var binding PortalServiceBinding
		if err := rows.Scan(&binding.UserID, &binding.RenderTokenEnc, &binding.CastingTokenEnc); err != nil {
			return nil, fmt.Errorf("store: scan service binding secret: %w", err)
		}
		out = append(out, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list service binding secrets: %w", err)
	}
	return out, nil
}

// UpsertServiceBinding inserts or replaces one user's binding.
func (d *DB) UpsertServiceBinding(ctx context.Context, binding PortalServiceBinding) error {
	if binding.UserID == "" {
		return fmt.Errorf("store: service binding user id is required")
	}
	mode := binding.Mode
	if mode == "" {
		mode = ServiceModeManaged
	}
	if mode != ServiceModeManaged && mode != ServiceModeCustom {
		return fmt.Errorf("store: invalid service binding mode %q", mode)
	}
	_, err := d.sql.ExecContext(ctx, `INSERT INTO portal_service_bindings
		(user_id, mode, render_url, casting_url, render_token_enc, casting_token_enc, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			mode = excluded.mode,
			render_url = excluded.render_url,
			casting_url = excluded.casting_url,
			render_token_enc = excluded.render_token_enc,
			casting_token_enc = excluded.casting_token_enc,
			updated_at = excluded.updated_at`,
		binding.UserID, mode, binding.RenderURL, binding.CastingURL,
		binding.RenderTokenEnc, binding.CastingTokenEnc,
		formatTime(binding.CreatedAt), formatTime(binding.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: save service binding: %w", err)
	}
	return nil
}

// DeleteServiceBinding removes a user's binding so it falls back to managed.
func (d *DB) DeleteServiceBinding(ctx context.Context, userID string) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM portal_service_bindings WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: delete service binding: %w", err)
	}
	return nil
}

// PlatformSetting is one platform-level value. Secret values hold the
// enc:v1:... envelope produced by the secret box, never plaintext.
type PlatformSetting struct {
	Key       string    `json:"key"`
	Value     string    `json:"-"`
	IsSecret  bool      `json:"is_secret"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// PlatformSettings returns every platform setting keyed by name.
func (d *DB) PlatformSettings(ctx context.Context) (map[string]PlatformSetting, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT key, value, is_secret, updated_at, updated_by FROM platform_settings`)
	if err != nil {
		return nil, fmt.Errorf("store: list platform settings: %w", err)
	}
	defer rows.Close()
	values := make(map[string]PlatformSetting)
	for rows.Next() {
		var (
			setting   PlatformSetting
			isSecret  int
			updatedAt string
		)
		if err := rows.Scan(&setting.Key, &setting.Value, &isSecret, &updatedAt, &setting.UpdatedBy); err != nil {
			return nil, fmt.Errorf("store: scan platform setting: %w", err)
		}
		setting.IsSecret = isSecret != 0
		if setting.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, fmt.Errorf("store: decode platform setting time: %w", err)
		}
		values[setting.Key] = setting
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list platform settings: %w", err)
	}
	return values, nil
}

// PutPlatformSettings writes several settings in one transaction.
func (d *DB) PutPlatformSettings(ctx context.Context, entries []PlatformSetting) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin platform settings: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, entry := range entries {
		if entry.Key == "" {
			return fmt.Errorf("store: platform setting key is required")
		}
		isSecret := 0
		if entry.IsSecret {
			isSecret = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO platform_settings
			(key, value, is_secret, updated_at, updated_by)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET
				value = excluded.value,
				is_secret = excluded.is_secret,
				updated_at = excluded.updated_at,
				updated_by = excluded.updated_by`,
			entry.Key, entry.Value, isSecret, formatTime(entry.UpdatedAt), entry.UpdatedBy); err != nil {
			return fmt.Errorf("store: put platform setting %s: %w", entry.Key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit platform settings: %w", err)
	}
	return nil
}

// DeletePlatformSettings removes the named platform settings. Missing keys are
// ignored so the call is idempotent.
func (d *DB) DeletePlatformSettings(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	for _, key := range keys {
		if _, err := d.sql.ExecContext(ctx, `DELETE FROM platform_settings WHERE key = ?`, key); err != nil {
			return fmt.Errorf("store: delete platform setting %s: %w", key, err)
		}
	}
	return nil
}

// DeletePortalSettings removes cached upstream resource IDs from the legacy
// portal_settings table, for example after Linx credentials change.
func (d *DB) DeletePortalSettings(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		if _, err := d.sql.ExecContext(ctx, `DELETE FROM portal_settings WHERE key = ?`, key); err != nil {
			return fmt.Errorf("store: delete portal setting %s: %w", key, err)
		}
	}
	return nil
}
