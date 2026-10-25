package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Portal account and device states. They mirror the design in
// docs/user-portal.md and are stored as plain strings so the database stays
// readable without a migration tool.
const (
	PortalDeviceProvisioning = "provisioning"
	PortalDeviceRegistered   = "registered"
	PortalDeviceError        = "error"
	PortalDeviceDisabled     = "disabled"

	PortalDeviceVirtual  = "virtual"
	PortalDevicePhysical = "physical"

	PortalRoleAdmin = "admin"
	PortalRoleUser  = "user"

	PortalUserActive   = "active"
	PortalUserDisabled = "disabled"
)

// PortalUser is a portal account. The account named in gateway configuration
// is bootstrapped as an administrator; every other account is created by the
// registration flow.
type PortalUser struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
}

// PortalSession is a login session. Only the SHA-256 of the cookie token is
// stored, so a database leak does not expose live sessions.
type PortalSession struct {
	TokenHash  string    `json:"-"`
	UserID     string    `json:"user_id"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	UserAgent  string    `json:"user_agent"`
}

// PortalDevice is a virtual or physical Linx device owned by a portal user.
type PortalDevice struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	Kind           string     `json:"kind"`
	MAC            string     `json:"mac"`
	ClientID       string     `json:"client_id"`
	LinxAgentID    string     `json:"agent_id"`
	AgentName      string     `json:"agent_name"`
	Name           string     `json:"name"`
	State          string     `json:"state"`
	Step           string     `json:"step"`
	Error          string     `json:"error"`
	CredentialPath string     `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	RegisteredAt   *time.Time `json:"registered_at,omitempty"`
}

// CreatePortalUser inserts a portal account. A duplicate username surfaces as
// the driver's unique-constraint error so callers can map it. Empty role and
// status fall back to "user" and "active".
func (d *DB) CreatePortalUser(ctx context.Context, user PortalUser) error {
	return d.createPortalUser(ctx, d.sql, user)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (d *DB) createPortalUser(ctx context.Context, q execer, user PortalUser) error {
	if user.ID == "" || user.Username == "" {
		return fmt.Errorf("store: portal user id and username are required")
	}
	role := user.Role
	if role == "" {
		role = PortalRoleUser
	}
	status := user.Status
	if status == "" {
		status = PortalUserActive
	}
	_, err := q.ExecContext(ctx, `INSERT INTO portal_users
		(id, username, password_hash, role, status, created_at, updated_at, last_login_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.PasswordHash, role, status,
		formatTime(user.CreatedAt), formatTime(user.UpdatedAt), formatTimePtr(user.LastLoginAt))
	if err != nil {
		return fmt.Errorf("store: create portal user: %w", err)
	}
	return nil
}

// CreatePortalUserWithDevice inserts an account and its first device in one
// transaction so registration cannot leave a user without an agent.
func (d *DB) CreatePortalUserWithDevice(ctx context.Context, user PortalUser, device PortalDevice) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin create portal user: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := d.createPortalUser(ctx, tx, user); err != nil {
		return err
	}
	if err := insertPortalDevice(ctx, tx, device); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit create portal user: %w", err)
	}
	return nil
}

// PortalUserByUsername finds an account by its normalized username.
func (d *DB) PortalUserByUsername(ctx context.Context, username string) (PortalUser, error) {
	return scanPortalUser(d.sql.QueryRowContext(ctx, selectPortalUser+` WHERE username = ?`, username))
}

// PortalUserByID finds an account by ID.
func (d *DB) PortalUserByID(ctx context.Context, id string) (PortalUser, error) {
	return scanPortalUser(d.sql.QueryRowContext(ctx, selectPortalUser+` WHERE id = ?`, id))
}

const selectPortalUser = `SELECT id, username, password_hash, role, status, created_at, updated_at, last_login_at
	FROM portal_users`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPortalUser(row rowScanner) (PortalUser, error) {
	var (
		user               PortalUser
		createdAt, updated string
		lastLoginAt        sql.NullString
	)
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.Status,
		&createdAt, &updated, &lastLoginAt); err != nil {
		return PortalUser{}, err
	}
	var err error
	if user.CreatedAt, err = parseTime(createdAt); err != nil {
		return PortalUser{}, fmt.Errorf("store: decode portal user time: %w", err)
	}
	if user.UpdatedAt, err = parseTime(updated); err != nil {
		return PortalUser{}, fmt.Errorf("store: decode portal user time: %w", err)
	}
	if lastLoginAt.Valid {
		parsed, err := parseTime(lastLoginAt.String)
		if err != nil {
			return PortalUser{}, fmt.Errorf("store: decode portal user login time: %w", err)
		}
		user.LastLoginAt = &parsed
	}
	return user, nil
}

// UpdatePortalUser persists an account's username, password hash, role and
// status.
func (d *DB) UpdatePortalUser(ctx context.Context, user PortalUser) error {
	if user.ID == "" {
		return fmt.Errorf("store: portal user id is required")
	}
	role := user.Role
	if role == "" {
		role = PortalRoleUser
	}
	status := user.Status
	if status == "" {
		status = PortalUserActive
	}
	_, err := d.sql.ExecContext(ctx, `UPDATE portal_users SET
		username = ?, password_hash = ?, role = ?, status = ?, updated_at = ?, last_login_at = ?
		WHERE id = ?`,
		user.Username, user.PasswordHash, role, status,
		formatTime(user.UpdatedAt), formatTimePtr(user.LastLoginAt), user.ID)
	if err != nil {
		return fmt.Errorf("store: update portal user: %w", err)
	}
	return nil
}

// SetPortalUserStatus enables or disables one account.
func (d *DB) SetPortalUserStatus(ctx context.Context, id, status string, now time.Time) error {
	if id == "" {
		return fmt.Errorf("store: portal user id is required")
	}
	if status != PortalUserActive && status != PortalUserDisabled {
		return fmt.Errorf("store: invalid portal user status %q", status)
	}
	result, err := d.sql.ExecContext(ctx, `UPDATE portal_users SET status = ?, updated_at = ? WHERE id = ?`,
		status, formatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set portal user status: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetPortalUserStatusGuarded disables or enables one account while atomically
// refusing to disable the last active administrator. It reports applied=false
// when that guard blocked the change, so concurrent requests cannot leave the
// platform without an administrator.
func (d *DB) SetPortalUserStatusGuarded(ctx context.Context, id, status string, now time.Time) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("store: portal user id is required")
	}
	if status != PortalUserActive && status != PortalUserDisabled {
		return false, fmt.Errorf("store: invalid portal user status %q", status)
	}
	result, err := d.sql.ExecContext(ctx, `UPDATE portal_users SET status = ?, updated_at = ?
		WHERE id = ?
		  AND (? <> ? OR role <> ? OR status <> ?
		       OR (SELECT COUNT(*) FROM portal_users WHERE role = ? AND status = ?) > 1)`,
		status, formatTime(now), id,
		status, PortalUserDisabled,
		PortalRoleAdmin, PortalUserActive,
		PortalRoleAdmin, PortalUserActive)
	if err != nil {
		return false, fmt.Errorf("store: set portal user status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: set portal user status: %w", err)
	}
	if affected > 0 {
		return true, nil
	}
	// The guard and a missing account both leave the row untouched; keep the
	// existing not-found behaviour for callers.
	var exists int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM portal_users WHERE id = ?`, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("store: check portal user: %w", err)
	}
	if exists == 0 {
		return false, sql.ErrNoRows
	}
	return false, nil
}

// TouchPortalUserLogin records a successful login.
func (d *DB) TouchPortalUserLogin(ctx context.Context, id string, now time.Time) error {
	if _, err := d.sql.ExecContext(ctx, `UPDATE portal_users SET last_login_at = ? WHERE id = ?`,
		formatTime(now), id); err != nil {
		return fmt.Errorf("store: touch portal user login: %w", err)
	}
	return nil
}

// PortalUsers lists accounts for the admin console, newest first. An empty
// status means every status; query matches the username.
func (d *DB) PortalUsers(ctx context.Context, query, status string, limit, offset int) ([]PortalUser, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.sql.QueryContext(ctx, selectPortalUser+`
		WHERE (? = '' OR status = ?) AND (? = '' OR username LIKE '%' || ? || '%')
		ORDER BY created_at DESC, id LIMIT ? OFFSET ?`,
		status, status, query, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: list portal users: %w", err)
	}
	defer rows.Close()
	var users []PortalUser
	for rows.Next() {
		user, err := scanPortalUser(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan portal user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list portal users: %w", err)
	}
	return users, nil
}

// CountPortalUsers counts accounts matching the same filters as PortalUsers.
func (d *DB) CountPortalUsers(ctx context.Context, query, status string) (int, error) {
	var count int
	err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM portal_users
		WHERE (? = '' OR status = ?) AND (? = '' OR username LIKE '%' || ? || '%')`,
		status, status, query, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count portal users: %w", err)
	}
	return count, nil
}

// CountPortalUsersByStatus returns account counts keyed by status.
func (d *DB) CountPortalUsersByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT status, COUNT(*) FROM portal_users GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("store: count portal users: %w", err)
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var (
			status string
			count  int
		)
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("store: scan portal user count: %w", err)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: count portal users: %w", err)
	}
	return counts, nil
}

// CountActiveAdmins counts enabled administrator accounts. It protects the
// last administrator from being disabled.
func (d *DB) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM portal_users WHERE role = ? AND status = ?`,
		PortalRoleAdmin, PortalUserActive).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count active admins: %w", err)
	}
	return count, nil
}

// DeletePortalSessionsByUser revokes every session of one account.
func (d *DB) DeletePortalSessionsByUser(ctx context.Context, userID string) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM portal_sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: delete portal sessions: %w", err)
	}
	return nil
}

// CreatePortalSession stores a login session.
func (d *DB) CreatePortalSession(ctx context.Context, session PortalSession) error {
	if session.TokenHash == "" || session.UserID == "" {
		return fmt.Errorf("store: portal session token and user are required")
	}
	_, err := d.sql.ExecContext(ctx, `INSERT INTO portal_sessions
		(token_hash, user_id, created_at, expires_at, last_seen_at, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)`,
		session.TokenHash, session.UserID, formatTime(session.CreatedAt), formatTime(session.ExpiresAt),
		formatTime(session.LastSeenAt), session.UserAgent)
	if err != nil {
		return fmt.Errorf("store: create portal session: %w", err)
	}
	return nil
}

// PortalSessionByTokenHash returns the session for a token hash. Expiry is
// checked by the caller so it can report a clear reason.
func (d *DB) PortalSessionByTokenHash(ctx context.Context, hash string) (PortalSession, error) {
	var (
		session                        PortalSession
		createdAt, expiresAt, lastSeen string
	)
	err := d.sql.QueryRowContext(ctx, `SELECT token_hash, user_id, created_at, expires_at, last_seen_at, user_agent
		FROM portal_sessions WHERE token_hash = ?`, hash).
		Scan(&session.TokenHash, &session.UserID, &createdAt, &expiresAt, &lastSeen, &session.UserAgent)
	if err != nil {
		return PortalSession{}, err
	}
	if session.CreatedAt, err = parseTime(createdAt); err != nil {
		return PortalSession{}, fmt.Errorf("store: decode portal session time: %w", err)
	}
	if session.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return PortalSession{}, fmt.Errorf("store: decode portal session time: %w", err)
	}
	if session.LastSeenAt, err = parseTime(lastSeen); err != nil {
		return PortalSession{}, fmt.Errorf("store: decode portal session time: %w", err)
	}
	return session, nil
}

// TouchPortalSession records the last time a session was used.
func (d *DB) TouchPortalSession(ctx context.Context, hash string, seenAt time.Time) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE portal_sessions SET last_seen_at = ? WHERE token_hash = ?`,
		formatTime(seenAt), hash)
	if err != nil {
		return fmt.Errorf("store: touch portal session: %w", err)
	}
	return nil
}

// DeletePortalSession removes one session; logging out twice is not an error.
func (d *DB) DeletePortalSession(ctx context.Context, hash string) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM portal_sessions WHERE token_hash = ?`, hash); err != nil {
		return fmt.Errorf("store: delete portal session: %w", err)
	}
	return nil
}

// DeleteExpiredPortalSessions removes sessions that can no longer be used.
func (d *DB) DeleteExpiredPortalSessions(ctx context.Context, now time.Time) (int64, error) {
	result, err := d.sql.ExecContext(ctx, `DELETE FROM portal_sessions WHERE expires_at < ?`, formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("store: delete expired portal sessions: %w", err)
	}
	return result.RowsAffected()
}

// CreatePortalDevice inserts a device record.
func (d *DB) CreatePortalDevice(ctx context.Context, device PortalDevice) error {
	return insertPortalDevice(ctx, d.sql, device)
}

func insertPortalDevice(ctx context.Context, q execer, device PortalDevice) error {
	if device.ID == "" || device.UserID == "" || device.MAC == "" {
		return fmt.Errorf("store: portal device id, user and mac are required")
	}
	_, err := q.ExecContext(ctx, `INSERT INTO portal_devices
		(id, user_id, kind, mac, client_id, linx_agent_id, agent_name, name, state, step, error, credential_path,
		 created_at, updated_at, registered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		device.ID, device.UserID, device.Kind, device.MAC, device.ClientID, device.LinxAgentID,
		device.AgentName, device.Name, device.State, device.Step, device.Error, device.CredentialPath,
		formatTime(device.CreatedAt), formatTime(device.UpdatedAt), formatTimePtr(device.RegisteredAt))
	if err != nil {
		return fmt.Errorf("store: create portal device: %w", err)
	}
	return nil
}

// DeletePortalDevice removes one device record. Callers decide whether its
// applications, voice runtime and credential file may go.
func (d *DB) DeletePortalDevice(ctx context.Context, id string) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM portal_devices WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete portal device: %w", err)
	}
	return nil
}

// PortalDeviceByID returns one device regardless of owner. Callers must check
// ownership themselves; the HTTP layer always scopes by the session user.
func (d *DB) PortalDeviceByID(ctx context.Context, id string) (PortalDevice, error) {
	return scanPortalDevice(d.sql.QueryRowContext(ctx, selectPortalDevice+` WHERE id = ?`, id))
}

// PortalDevicesByUser lists a user's devices, newest first.
func (d *DB) PortalDevicesByUser(ctx context.Context, userID string) ([]PortalDevice, error) {
	rows, err := d.sql.QueryContext(ctx, selectPortalDevice+` WHERE user_id = ? ORDER BY created_at DESC, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list portal devices: %w", err)
	}
	defer rows.Close()
	var devices []PortalDevice
	for rows.Next() {
		device, err := scanPortalDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan portal device: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list portal devices: %w", err)
	}
	return devices, nil
}

// PortalDevicesByState lists devices in one state across all users.
func (d *DB) PortalDevicesByState(ctx context.Context, state string) ([]PortalDevice, error) {
	rows, err := d.sql.QueryContext(ctx, selectPortalDevice+` WHERE state = ? ORDER BY created_at, id`, state)
	if err != nil {
		return nil, fmt.Errorf("store: list portal devices: %w", err)
	}
	defer rows.Close()
	var devices []PortalDevice
	for rows.Next() {
		device, err := scanPortalDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan portal device: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list portal devices: %w", err)
	}
	return devices, nil
}

// PortalDevicesAll lists every device, newest first, for the admin console.
func (d *DB) PortalDevicesAll(ctx context.Context, state, query string, limit int) ([]PortalDevice, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := d.sql.QueryContext(ctx, selectPortalDevice+`
		WHERE (? = '' OR state = ?) AND (? = '' OR mac LIKE '%' || ? || '%' OR agent_name LIKE '%' || ? || '%' OR linx_agent_id LIKE '%' || ? || '%' OR user_id LIKE '%' || ? || '%')
		ORDER BY created_at DESC, id LIMIT ?`,
		state, state, query, query, query, query, query, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list portal devices: %w", err)
	}
	defer rows.Close()
	var devices []PortalDevice
	for rows.Next() {
		device, err := scanPortalDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan portal device: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list portal devices: %w", err)
	}
	return devices, nil
}

// PortalDeviceByIdentity resolves the device behind an MCP identity. The
// agent ID must match, so a MAC alone cannot claim another user's agent.
func (d *DB) PortalDeviceByIdentity(ctx context.Context, mac, agentID string) (PortalDevice, error) {
	return scanPortalDevice(d.sql.QueryRowContext(ctx, selectPortalDevice+`
		WHERE mac = ? AND linx_agent_id = ?`, mac, agentID))
}

// CountPortalDevicesByState returns device counts keyed by provisioning state.
func (d *DB) CountPortalDevicesByState(ctx context.Context) (map[string]int, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT state, COUNT(*) FROM portal_devices GROUP BY state`)
	if err != nil {
		return nil, fmt.Errorf("store: count portal devices: %w", err)
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var (
			state string
			count int
		)
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("store: scan portal device count: %w", err)
		}
		counts[state] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: count portal devices: %w", err)
	}
	return counts, nil
}

// UpdatePortalDevice persists a full device record.
func (d *DB) UpdatePortalDevice(ctx context.Context, device PortalDevice) error {
	if device.ID == "" {
		return fmt.Errorf("store: portal device id is required")
	}
	_, err := d.sql.ExecContext(ctx, `UPDATE portal_devices SET
		kind = ?, mac = ?, client_id = ?, linx_agent_id = ?, agent_name = ?, name = ?, state = ?, step = ?,
		error = ?, credential_path = ?, updated_at = ?, registered_at = ?
		WHERE id = ?`,
		device.Kind, device.MAC, device.ClientID, device.LinxAgentID, device.AgentName, device.Name,
		device.State, device.Step, device.Error, device.CredentialPath,
		formatTime(device.UpdatedAt), formatTimePtr(device.RegisteredAt), device.ID)
	if err != nil {
		return fmt.Errorf("store: update portal device: %w", err)
	}
	return nil
}

const selectPortalDevice = `SELECT id, user_id, kind, mac, client_id, linx_agent_id, agent_name, name,
	state, step, error, credential_path, created_at, updated_at, registered_at FROM portal_devices`

func scanPortalDevice(row rowScanner) (PortalDevice, error) {
	var (
		device               PortalDevice
		createdAt, updatedAt string
		registeredAt         sql.NullString
	)
	if err := row.Scan(&device.ID, &device.UserID, &device.Kind, &device.MAC, &device.ClientID,
		&device.LinxAgentID, &device.AgentName, &device.Name, &device.State, &device.Step, &device.Error,
		&device.CredentialPath, &createdAt, &updatedAt, &registeredAt); err != nil {
		return PortalDevice{}, err
	}
	var err error
	if device.CreatedAt, err = parseTime(createdAt); err != nil {
		return PortalDevice{}, fmt.Errorf("store: decode portal device time: %w", err)
	}
	if device.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return PortalDevice{}, fmt.Errorf("store: decode portal device time: %w", err)
	}
	if registeredAt.Valid {
		parsed, err := parseTime(registeredAt.String)
		if err != nil {
			return PortalDevice{}, fmt.Errorf("store: decode portal device registered time: %w", err)
		}
		device.RegisteredAt = &parsed
	}
	return device, nil
}

// PortalSetting reads one key/value setting. A missing key returns
// sql.ErrNoRows.
func (d *DB) PortalSetting(ctx context.Context, key string) (string, error) {
	var value string
	if err := d.sql.QueryRowContext(ctx, `SELECT value FROM portal_settings WHERE key = ?`, key).Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

// SetPortalSetting inserts or replaces one setting.
func (d *DB) SetPortalSetting(ctx context.Context, key, value string) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO portal_settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("store: set portal setting %s: %w", key, err)
	}
	return nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatTimePtr(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
