package portal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// newAuthDBEnv is newAuthEnv with the database path exposed so a test can
// install triggers or rename tables to force storage failures.
func newAuthDBEnv(t *testing.T) (*store.DB, *Auth, *time.Time, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "portal.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	auth := NewAuth(db, AuthConfig{SessionTTL: time.Hour, Now: func() time.Time { return now }})
	return db, auth, &now, path
}

func execRaw(t *testing.T, path, statement string) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer raw.Close()
	if _, err := raw.Exec(statement); err != nil {
		t.Fatalf("exec %q: %v", statement, err)
	}
}

func TestNewAuthDefaults(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	auth := NewAuth(db, AuthConfig{})
	if auth.cfg.SessionTTL != 30*24*time.Hour {
		t.Fatalf("session TTL = %v", auth.cfg.SessionTTL)
	}
	if auth.now == nil || auth.cfg.Now == nil {
		t.Fatal("NewAuth left the clock nil")
	}
	if auth.hashSem == nil || cap(auth.hashSem) != maxHashConcurrency {
		t.Fatalf("hash semaphore = %v", auth.hashSem)
	}
}

func TestEnsureAdminStorageFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("lookup", func(t *testing.T) {
		db, auth, _, _ := newAuthDBEnv(t)
		if _, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"}); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"}); err == nil ||
			!strings.Contains(err.Error(), "load configured user") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("create", func(t *testing.T) {
		_, auth, _, path := newAuthDBEnv(t)
		execRaw(t, path, `CREATE TRIGGER block_user_insert BEFORE INSERT ON portal_users
			BEGIN SELECT RAISE(ABORT, 'blocked insert'); END`)
		_, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"})
		if err == nil || !strings.Contains(err.Error(), "create configured user") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("revoke sessions", func(t *testing.T) {
		db, auth, _, path := newAuthDBEnv(t)
		user, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.PortalUserByID(ctx, user.ID); err != nil {
			t.Fatal(err)
		}
		execRaw(t, path, `CREATE TRIGGER block_session_delete BEFORE DELETE ON portal_sessions
			BEGIN SELECT RAISE(ABORT, 'blocked delete'); END`)
		_, err = auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "different-password"})
		if err == nil || !strings.Contains(err.Error(), "revoke sessions") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("update", func(t *testing.T) {
		_, auth, _, path := newAuthDBEnv(t)
		if _, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"}); err != nil {
			t.Fatal(err)
		}
		execRaw(t, path, `CREATE TRIGGER block_user_update BEFORE UPDATE ON portal_users
			BEGIN SELECT RAISE(ABORT, 'blocked update'); END`)
		_, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "rotated-password"})
		if err == nil || !strings.Contains(err.Error(), "update configured user") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("promote and reactivate", func(t *testing.T) {
		db, auth, _, _ := newAuthDBEnv(t)
		hash, err := hashPassword("password123")
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		user := store.PortalUser{
			ID: "usr_plain", Username: "alice", PasswordHash: hash,
			Role: store.PortalRoleUser, Status: store.PortalUserDisabled,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := db.CreatePortalUser(ctx, user); err != nil {
			t.Fatal(err)
		}
		updated, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"})
		if err != nil {
			t.Fatal(err)
		}
		if updated.Role != store.PortalRoleAdmin || updated.Status != store.PortalUserActive {
			t.Fatalf("updated = %+v", updated)
		}
		stored, err := db.PortalUserByUsername(ctx, "alice")
		if err != nil || stored.Role != store.PortalRoleAdmin || stored.Status != store.PortalUserActive {
			t.Fatalf("stored = %+v err=%v", stored, err)
		}
	})
}

func TestRegisterStorageAndValidationFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid username", func(t *testing.T) {
		_, auth, _, _ := newAuthDBEnv(t)
		_, err := auth.Register(ctx, Credentials{Username: "bad!name", Password: "password123"}, "agent", "10.0.0.1")
		if apiCode(t, err) != "INVALID_ARGUMENT" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("short password", func(t *testing.T) {
		_, auth, _, _ := newAuthDBEnv(t)
		_, err := auth.Register(ctx, Credentials{Username: "newcomer", Password: "short"}, "agent", "10.0.0.1")
		if apiCode(t, err) != "INVALID_ARGUMENT" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("username taken", func(t *testing.T) {
		_, auth, _, _ := newAuthDBEnv(t)
		if _, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"}); err != nil {
			t.Fatal(err)
		}
		_, err := auth.Register(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1")
		if apiCode(t, err) != "USERNAME_TAKEN" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("lookup", func(t *testing.T) {
		db, auth, _, _ := newAuthDBEnv(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		_, err := auth.Register(ctx, Credentials{Username: "newcomer", Password: "password123"}, "agent", "10.0.0.1")
		if err == nil || !strings.Contains(err.Error(), "lookup username") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("insert", func(t *testing.T) {
		_, auth, _, path := newAuthDBEnv(t)
		execRaw(t, path, `CREATE TRIGGER block_user_insert BEFORE INSERT ON portal_users
			BEGIN SELECT RAISE(ABORT, 'blocked insert'); END`)
		_, err := auth.Register(ctx, Credentials{Username: "newcomer", Password: "password123"}, "agent", "10.0.0.1")
		if err == nil || !strings.Contains(err.Error(), "register user") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("session insert", func(t *testing.T) {
		db, auth, _, path := newAuthDBEnv(t)
		execRaw(t, path, `CREATE TRIGGER block_session_insert BEFORE INSERT ON portal_sessions
			BEGIN SELECT RAISE(ABORT, 'blocked insert'); END`)
		registration, err := auth.Register(ctx, Credentials{Username: "newcomer", Password: "password123"}, "agent", "10.0.0.1")
		if err == nil || !strings.Contains(err.Error(), "create session") {
			t.Fatalf("err = %v", err)
		}
		if registration.User.ID != "" {
			t.Fatalf("a failed session returned a registration: %+v", registration)
		}
		if _, err := db.PortalUserByUsername(ctx, "newcomer"); err != nil {
			t.Fatalf("the account was not created: %v", err)
		}
	})

	t.Run("canceled while waiting for a hash slot", func(t *testing.T) {
		_, auth, _, _ := newAuthDBEnv(t)
		for i := 0; i < maxHashConcurrency; i++ {
			auth.hashSem <- struct{}{}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := auth.hashPasswordFor(ctx, "password123"); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		for i := 0; i < maxHashConcurrency; i++ {
			<-auth.hashSem
		}
	})
}

func TestRegisterRateLimitWindows(t *testing.T) {
	_, auth, now, _ := newAuthDBEnv(t)
	ctx := context.Background()
	auth.registerIP = newWindowLimiter(auth.now, time.Hour, 1)
	if _, err := auth.Register(ctx, Credentials{Username: "first", Password: "password123"}, "agent", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	_, err := auth.Register(ctx, Credentials{Username: "second", Password: "password123"}, "agent", "10.0.0.1")
	if apiCode(t, err) != "REGISTRATION_RATE_LIMITED" {
		t.Fatalf("hourly err = %v", err)
	}
	*now = now.Add(registerWindow + time.Second)
	auth.registerIPDaily = newWindowLimiter(auth.now, registerDailyWindow, 1)
	if _, err := auth.Register(ctx, Credentials{Username: "third", Password: "password123"}, "agent", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	_, err = auth.Register(ctx, Credentials{Username: "fourth", Password: "password123"}, "agent", "10.0.0.1")
	if apiCode(t, err) != "REGISTRATION_RATE_LIMITED" {
		t.Fatalf("daily err = %v", err)
	}
	*now = now.Add(registerDailyWindow + time.Second)
	auth.registerGlobal = newWindowLimiter(auth.now, registerWindow, 1)
	if _, err := auth.Register(ctx, Credentials{Username: "fifth", Password: "password123"}, "agent", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	_, err = auth.Register(ctx, Credentials{Username: "sixth", Password: "password123"}, "agent", "10.0.0.3")
	if apiCode(t, err) != "REGISTRATION_RATE_LIMITED" {
		t.Fatalf("global err = %v", err)
	}
}

func TestLoginStorageFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("lookup", func(t *testing.T) {
		db, auth, _, _ := newAuthDBEnv(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		_, _, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1")
		if err == nil || !strings.Contains(err.Error(), "lookup user") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("touch login", func(t *testing.T) {
		_, auth, _, path := newAuthDBEnv(t)
		if _, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"}); err != nil {
			t.Fatal(err)
		}
		execRaw(t, path, `CREATE TRIGGER block_user_update BEFORE UPDATE ON portal_users
			BEGIN SELECT RAISE(ABORT, 'blocked update'); END`)
		_, _, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1")
		if err == nil || !strings.Contains(err.Error(), "blocked update") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("create session", func(t *testing.T) {
		_, auth, _, path := newAuthDBEnv(t)
		if _, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"}); err != nil {
			t.Fatal(err)
		}
		execRaw(t, path, `CREATE TRIGGER block_session_insert BEFORE INSERT ON portal_sessions
			BEGIN SELECT RAISE(ABORT, 'blocked insert'); END`)
		_, _, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1")
		if err == nil || !strings.Contains(err.Error(), "create session") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestAuthenticateStorageFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("load session", func(t *testing.T) {
		db, auth, _, _ := newAuthDBEnv(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		_, err := auth.Authenticate(ctx, "token")
		if err == nil || !strings.Contains(err.Error(), "load session") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("touch session", func(t *testing.T) {
		db, auth, now, _ := newAuthDBEnv(t)
		user, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"})
		if err != nil {
			t.Fatal(err)
		}
		old := now.Add(-time.Hour)
		if err := db.CreatePortalSession(ctx, store.PortalSession{
			TokenHash:  SessionHash("token"),
			UserID:     user.ID,
			CreatedAt:  old,
			ExpiresAt:  now.Add(time.Hour),
			LastSeenAt: old,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.Authenticate(ctx, "token"); err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		session, err := db.PortalSessionByTokenHash(ctx, SessionHash("token"))
		if err != nil {
			t.Fatal(err)
		}
		if session.LastSeenAt.Before(*now) {
			t.Fatalf("the session was not touched: %+v", session)
		}
	})

	t.Run("missing user", func(t *testing.T) {
		_, auth, now, path := newAuthDBEnv(t)
		// Insert directly with foreign keys disabled: the session points at a
		// user that was removed outside the portal's own cascade.
		raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(0)&_pragma=busy_timeout(5000)")
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		if _, err := raw.Exec(`INSERT INTO portal_sessions
			(token_hash, user_id, created_at, expires_at, last_seen_at, user_agent)
			VALUES (?, 'usr_missing', ?, ?, ?, '')`,
			SessionHash("orphan"), now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.Authenticate(context.Background(), "orphan"); apiCode(t, err) != "UNAUTHORIZED" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("load user", func(t *testing.T) {
		db, auth, now, path := newAuthDBEnv(t)
		user, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.CreatePortalSession(ctx, store.PortalSession{
			TokenHash: SessionHash("token"), UserID: user.ID,
			CreatedAt: *now, ExpiresAt: now.Add(time.Hour), LastSeenAt: *now,
		}); err != nil {
			t.Fatal(err)
		}
		execRaw(t, path, `ALTER TABLE portal_users RENAME TO portal_users_gone`)
		_, err = auth.Authenticate(ctx, "token")
		if err == nil || !strings.Contains(err.Error(), "load user") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestLogoutAndSessionHelpers(t *testing.T) {
	ctx := context.Background()
	db, auth, now, _ := newAuthDBEnv(t)
	if err := auth.Logout(ctx, ""); err != nil {
		t.Fatalf("empty Logout: %v", err)
	}
	user, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreatePortalSession(ctx, store.PortalSession{
		TokenHash: SessionHash("expired"), UserID: user.ID,
		CreatedAt: now.Add(-3 * time.Hour), ExpiresAt: now.Add(-time.Hour), LastSeenAt: now.Add(-3 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	auth.CleanupExpiredSessions(ctx)
	if _, err := db.PortalSessionByTokenHash(ctx, SessionHash("expired")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired session survived cleanup: %v", err)
	}

	var logged string
	auth.logf = func(format string, args ...any) { logged = fmt.Sprintf(format, args...) }
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	auth.CleanupExpiredSessions(ctx)
	if !strings.Contains(logged, "clean expired sessions") {
		t.Fatalf("log = %q", logged)
	}
	if err := auth.Logout(ctx, "token"); err == nil {
		t.Fatal("Logout on a closed database succeeded")
	}
}

func TestClientIPAndInputHelpers(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.7"
	if got := ClientIP(request); got != "203.0.113.7" {
		t.Fatalf("ClientIP = %q", got)
	}
	request.RemoteAddr = "203.0.113.7:4321"
	if got := ClientIP(request); got != "203.0.113.7" {
		t.Fatalf("ClientIP = %q", got)
	}
	request.RemoteAddr = "[2001:db8::1]:443"
	if got := ClientIP(request); got != "2001:db8::1" {
		t.Fatalf("ClientIP = %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("ab", 3); got != "ab" {
		t.Fatalf("truncate = %q", got)
	}
	if _, err := normalizeUsername("bad!name"); apiCode(t, err) != "INVALID_ARGUMENT" {
		t.Fatalf("normalizeUsername err = %v", err)
	}
	if _, err := normalizeUsername("ok.name-1_2"); err != nil {
		t.Fatalf("normalizeUsername rejected a valid name: %v", err)
	}
	if err := validatePassword(strings.Repeat("a", 129)); apiCode(t, err) != "INVALID_ARGUMENT" {
		t.Fatalf("long password err = %v", err)
	}
	if err := validatePassword("12345678"); err != nil {
		t.Fatalf("valid password err = %v", err)
	}

	hash, err := hashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, "password123") {
		t.Fatal("hashPassword/verifyPassword disagree")
	}
	for _, malformed := range []string{
		"", "plain", "pbkdf2-sha256$1$c2FsdA", "scrypt$1$c2FsdA$a2V5",
		"pbkdf2-sha256$abc$c2FsdA$a2V5", "pbkdf2-sha256$0$c2FsdA$a2V5",
		"pbkdf2-sha256$1$%%%$a2V5", "pbkdf2-sha256$1$c2FsdA$%%%",
		"pbkdf2-sha256$1$c2FsdA$",
	} {
		if verifyPassword(malformed, "password123") {
			t.Errorf("verifyPassword accepted %q", malformed)
		}
	}
}

func TestRateLimitedRoundsUpToOneSecond(t *testing.T) {
	err := rateLimited(10 * time.Millisecond)
	if err.Status != http.StatusTooManyRequests || !strings.Contains(err.Message, "1 秒") {
		t.Fatalf("err = %+v", err)
	}
}

func TestLoginLimiterDoublesAndCapsBlocks(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(func() time.Time { return now })
	if wait, blocked := limiter.blocked("missing"); blocked || wait != 0 {
		t.Fatalf("an unseen key was blocked: wait=%v", wait)
	}
	key := "alice|10.0.0.1"
	for attempt := 0; attempt < maxLoginFailures; attempt++ {
		limiter.fail(key)
	}
	if wait, blocked := limiter.blocked(key); !blocked || wait != time.Minute {
		t.Fatalf("first block: blocked=%v wait=%v", blocked, wait)
	}
	// Further failures inside the same window keep doubling the block up to the
	// configured ceiling.
	now = now.Add(time.Minute + time.Second)
	for round, want := range []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, maxLoginBlock, maxLoginBlock} {
		limiter.fail(key)
		wait, blocked := limiter.blocked(key)
		if !blocked || wait != want {
			t.Fatalf("round %d blocked=%v wait=%v, want %v", round, blocked, wait, want)
		}
	}
	now = now.Add(maxLoginBlock + time.Second)
	if _, blocked := limiter.blocked(key); blocked {
		t.Fatal("the block outlived its deadline")
	}
	limiter.succeed(key)
	if _, blocked := limiter.blocked(key); blocked {
		t.Fatal("succeed did not clear the entry")
	}
}

func TestWindowLimiterBoundsItsMapAndResetsWindows(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	limiter := newWindowLimiter(func() time.Time { return now }, time.Hour, 100)
	if _, allowed := limiter.allow(""); !allowed {
		t.Fatal("the empty key was rejected")
	}
	if _, ok := limiter.entries["unknown"]; !ok {
		t.Fatal("the empty key was not canonicalized")
	}
	for i := 0; i < 5000; i++ {
		limiter.allow(fmt.Sprintf("key-%d", i))
	}
	now = now.Add(2 * time.Hour)
	limiter.allow("fresh")
	if len(limiter.entries) > 4096 {
		t.Fatalf("expired entries were not pruned: %d", len(limiter.entries))
	}
	limited := newWindowLimiter(func() time.Time { return now }, time.Minute, 1)
	limited.allow("k")
	if wait, allowed := limited.allow("k"); allowed || wait <= 0 {
		t.Fatalf("limit was not enforced: wait=%v allowed=%v", wait, allowed)
	}
	now = now.Add(2 * time.Minute)
	if _, allowed := limited.allow("k"); !allowed {
		t.Fatal("the window did not reset")
	}
}

func TestSessionHashAndIsUniqueViolation(t *testing.T) {
	if SessionHash("token") != tokenHash("token") || len(SessionHash("token")) != 64 {
		t.Fatalf("SessionHash = %q", SessionHash("token"))
	}
	if !isUniqueViolation(errors.New("UNIQUE constraint failed: portal_users.username")) {
		t.Fatal("a unique violation was not recognized")
	}
	if isUniqueViolation(nil) || isUniqueViolation(errors.New("other error")) {
		t.Fatal("isUniqueViolation accepted a different error")
	}
}

func TestRegisterMapsAUniqueViolationToAConflict(t *testing.T) {
	env := newPortalHTTPEnv(t)
	enablePortalRegistration(t, env)
	createPortalTestUser(t, env, "usr_taken", "taken", "password123", store.PortalRoleUser)
	execRaw(t, env.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_base")
	// The lookup sees no accounts while an insert is forwarded to the real table,
	// so the duplicate is only caught by the unique constraint at insert time.
	execRaw(t, env.dbPath, `CREATE VIEW portal_users AS
		SELECT id, username, password_hash, role, status, created_at, updated_at, last_login_at
		FROM portal_users_base WHERE 0`)
	execRaw(t, env.dbPath, `CREATE TRIGGER portal_users_insert INSTEAD OF INSERT ON portal_users
		BEGIN
			INSERT INTO portal_users_base
				(id, username, password_hash, role, status, created_at, updated_at, last_login_at)
			VALUES (NEW.id, NEW.username, NEW.password_hash, NEW.role, NEW.status,
				NEW.created_at, NEW.updated_at, NEW.last_login_at);
		END`)
	response, payload := env.do(t, http.MethodPost, "/api/portal/register",
		`{"username":"taken","password":"password123"}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "USERNAME_TAKEN" {
		t.Fatalf("duplicate status=%d body=%s", response.StatusCode, payload)
	}
}
