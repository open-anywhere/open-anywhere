package portal

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func apiCode(t *testing.T, err error) string {
	t.Helper()
	var target *APIError
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

func newAuthEnv(t *testing.T) (*store.DB, *Auth, *time.Time) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	auth := NewAuth(db, AuthConfig{
		SessionTTL: time.Hour,
		Now:        func() time.Time { return now },
	})
	return db, auth, &now
}

func TestEnsureAdminCreatesAndUpdatesTheConfiguredAccount(t *testing.T) {
	db, auth, _ := newAuthEnv(t)
	ctx := context.Background()

	user, err := auth.EnsureAdmin(ctx, Credentials{Username: "  Alice ", Password: "password123"})
	if err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if user.Username != "alice" || !strings.HasPrefix(user.ID, "usr_") {
		t.Fatalf("user=%+v", user)
	}
	if user.Role != store.PortalRoleAdmin || user.Status != store.PortalUserActive {
		t.Fatalf("the configured account is not an active admin: %+v", user)
	}
	if strings.Contains(user.PasswordHash, "password123") {
		t.Fatal("the password was stored in clear text")
	}
	parts := strings.Split(user.PasswordHash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" {
		t.Fatalf("hash format=%q", user.PasswordHash)
	}
	if !verifyPassword(user.PasswordHash, "password123") || verifyPassword(user.PasswordHash, "wrong-password") {
		t.Fatal("password verification disagrees with the stored hash")
	}

	stored, err := db.PortalUserByUsername(ctx, "alice")
	if err != nil || stored.ID != user.ID {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}

	// A normal restart finds the account and leaves it untouched.
	again, err := auth.EnsureAdmin(ctx, Credentials{Username: "ALICE", Password: "password123"})
	if err != nil || again.ID != user.ID || again.PasswordHash != user.PasswordHash {
		t.Fatalf("second EnsureAdmin=%+v err=%v", again, err)
	}

	for _, username := range []string{"ab", strings.Repeat("a", 33), "张三", "a b", "a@b"} {
		if _, err := auth.EnsureAdmin(ctx, Credentials{Username: username, Password: "password123"}); apiCode(t, err) != "INVALID_ARGUMENT" {
			t.Errorf("username %q err=%v", username, err)
		}
	}
	if _, err := auth.EnsureAdmin(ctx, Credentials{Username: "bob", Password: "short"}); apiCode(t, err) != "INVALID_ARGUMENT" {
		t.Fatalf("short password err=%v", err)
	}

	// Rotating the configured password keeps the account but revokes sessions.
	_, token, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "new-password-456"})
	if err != nil || rotated.ID != user.ID || rotated.PasswordHash == user.PasswordHash {
		t.Fatalf("rotated=%+v err=%v", rotated, err)
	}
	if _, err := auth.Authenticate(ctx, token); apiCode(t, err) != "UNAUTHORIZED" {
		t.Fatalf("a session survived the password change: %v", err)
	}
	if _, _, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1"); apiCode(t, err) != "INVALID_CREDENTIALS" {
		t.Fatalf("the old password still works: %v", err)
	}
	if _, _, err := auth.Login(ctx, Credentials{Username: "alice", Password: "new-password-456"}, "agent", "10.0.0.1"); err != nil {
		t.Fatalf("the new password does not work: %v", err)
	}
}

func TestLoginRejectsBadPasswordsAndHidesWhetherTheUserExists(t *testing.T) {
	_, auth, _ := newAuthEnv(t)
	ctx := context.Background()
	if _, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"}); err != nil {
		t.Fatal(err)
	}

	_, _, wrong := auth.Login(ctx, Credentials{Username: "alice", Password: "wrong-password"}, "agent", "10.0.0.1")
	_, _, missing := auth.Login(ctx, Credentials{Username: "nobody", Password: "wrong-password"}, "agent", "10.0.0.2")
	if apiCode(t, wrong) != "INVALID_CREDENTIALS" || apiCode(t, missing) != "INVALID_CREDENTIALS" {
		t.Fatalf("wrong=%v missing=%v", wrong, missing)
	}
	if wrong.Error() != missing.Error() {
		t.Fatalf("the two failures are distinguishable: %q / %q", wrong, missing)
	}

	user, token, err := auth.Login(ctx, Credentials{Username: "ALICE", Password: "password123"}, "agent", "10.0.0.1")
	if err != nil || user.Username != "alice" || token == "" {
		t.Fatalf("login=%+v token=%q err=%v", user, token, err)
	}
}

func TestLoginIsRateLimitedPerUsernameAndAddress(t *testing.T) {
	_, auth, now := newAuthEnv(t)
	ctx := context.Background()
	for attempt := 0; attempt < maxLoginFailures; attempt++ {
		if _, _, err := auth.Login(ctx, Credentials{Username: "ghost", Password: "wrong-password"}, "agent", "10.0.0.1"); apiCode(t, err) != "INVALID_CREDENTIALS" {
			t.Fatalf("attempt %d err=%v", attempt, err)
		}
	}
	if _, _, err := auth.Login(ctx, Credentials{Username: "ghost", Password: "wrong-password"}, "agent", "10.0.0.1"); apiCode(t, err) != "TOO_MANY_ATTEMPTS" {
		t.Fatalf("the limited address was not blocked: %v", err)
	}
	if _, _, err := auth.Login(ctx, Credentials{Username: "ghost", Password: "wrong-password"}, "agent", "10.0.0.2"); apiCode(t, err) != "INVALID_CREDENTIALS" {
		t.Fatalf("another address inherited the block: %v", err)
	}
	*now = now.Add(loginWindow + time.Second)
	if _, _, err := auth.Login(ctx, Credentials{Username: "ghost", Password: "wrong-password"}, "agent", "10.0.0.1"); apiCode(t, err) != "INVALID_CREDENTIALS" {
		t.Fatalf("the block outlived its window: %v", err)
	}
}

func TestRegistrationDailyLimitPerAddress(t *testing.T) {
	db, auth, clock := newAuthEnv(t)
	ctx := context.Background()
	const address = "203.0.113.9"

	// The hourly and global limiters would fire first; relax them so this test
	// exercises only the per-address 24h window.
	auth.registerIP = newWindowLimiter(auth.now, time.Hour, 1_000)
	auth.registerGlobal = newWindowLimiter(auth.now, time.Hour, 1_000)
	for attempt := 0; attempt < registerPerIPDailyLimit; attempt++ {
		if _, allowed := auth.registerIPDaily.allow(address); !allowed {
			t.Fatalf("attempt %d was rejected before reaching the daily limit", attempt)
		}
	}
	wait, allowed := auth.registerIPDaily.allow(address)
	if allowed || wait <= 23*time.Hour {
		t.Fatalf("the daily window did not block: wait=%v allowed=%v", wait, allowed)
	}
	if _, err := auth.Register(ctx, Credentials{Username: "newcomer", Password: "password123"}, "agent", address); apiCode(t, err) != "REGISTRATION_RATE_LIMITED" {
		t.Fatalf("Register err = %v", err)
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
			t.Fatalf("status = %+v", apiErr)
		}
	}
	if _, err := db.PortalUserByUsername(ctx, "newcomer"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a rate-limited registration created an account: %v", err)
	}
	// One address does not consume another address's budget.
	if _, allowed := auth.registerIPDaily.allow("198.51.100.4"); !allowed {
		t.Fatal("one address exhausted the budget of another")
	}
	// Once the 24h window passes the address may register again.
	*clock = clock.Add(registerDailyWindow + time.Second)
	registration, err := auth.Register(ctx, Credentials{Username: "newcomer", Password: "password123"}, "agent", address)
	if err != nil {
		t.Fatalf("registration after the window: %v", err)
	}
	if registration.User.Role != store.PortalRoleUser || registration.Device.Kind != store.PortalDeviceVirtual || registration.Token == "" {
		t.Fatalf("registration = %+v", registration)
	}
}

func TestSessionsExpireAndLogoutRevokesTheToken(t *testing.T) {
	db, auth, now := newAuthEnv(t)
	ctx := context.Background()
	if _, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"}); err != nil {
		t.Fatal(err)
	}
	_, token, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := auth.Authenticate(ctx, "forged-token"); apiCode(t, err) != "UNAUTHORIZED" {
		t.Fatalf("a forged token was accepted: %v", err)
	}

	_, freshToken, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Hour)
	if _, err := auth.Authenticate(ctx, freshToken); apiCode(t, err) != "UNAUTHORIZED" {
		t.Fatalf("an expired session was accepted: %v", err)
	}
	if _, err := db.PortalSessionByTokenHash(ctx, SessionHash(freshToken)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the expired session was not deleted: %v", err)
	}

	// Logout of the still-valid token revokes it.
	if err := auth.Logout(ctx, token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := auth.Authenticate(ctx, token); apiCode(t, err) != "UNAUTHORIZED" {
		t.Fatalf("a logged-out token was accepted: %v", err)
	}
}

func TestDisabledAccountCannotLogInOrKeepSessions(t *testing.T) {
	db, auth, _ := newAuthEnv(t)
	ctx := context.Background()
	user, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPortalUserStatus(ctx, user.ID, store.PortalUserDisabled, time.Now()); err != nil {
		t.Fatalf("SetPortalUserStatus: %v", err)
	}
	if _, _, err := auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.2"); apiCode(t, err) != "ACCOUNT_DISABLED" {
		t.Fatalf("a disabled account logged in: %v", err)
	}
	if _, err := auth.Authenticate(ctx, token); apiCode(t, err) != "UNAUTHORIZED" {
		t.Fatalf("a disabled account kept a session: %v", err)
	}
	// EnsureAdmin is the bootstrap account and restores it on the next start.
	restored, err := auth.EnsureAdmin(ctx, Credentials{Username: "alice", Password: "password123"})
	if err != nil || restored.Status != store.PortalUserActive || restored.Role != store.PortalRoleAdmin {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
}

func TestSessionCookieFlags(t *testing.T) {
	_, auth, now := newAuthEnv(t)
	auth.cfg.SessionTTL = 90 * time.Minute
	auth.cfg.CookieSecure = true
	recorder := httptest.NewRecorder()
	auth.SetCookie(recorder, "token-value")
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookie || cookie.Value != "token-value" || !cookie.HttpOnly ||
		!cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" ||
		cookie.MaxAge != int((90*time.Minute/time.Second)) {
		t.Fatalf("cookie=%+v", cookie)
	}
	if !cookie.Expires.IsZero() && cookie.Expires.Before(*now) {
		t.Fatalf("cookie expiry=%v", cookie.Expires)
	}

	recorder = httptest.NewRecorder()
	auth.ClearCookie(recorder)
	cleared := recorder.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 || cleared[0].Value != "" {
		t.Fatalf("cleared cookie=%+v", cleared)
	}
}
