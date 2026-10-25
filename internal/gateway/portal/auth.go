package portal

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

const (
	// SessionCookie is the portal's session cookie.
	SessionCookie = "oa_portal"

	passwordIterations = 600_000
	passwordSaltBytes  = 16
	passwordKeyBytes   = 32

	maxLoginFailures = 5
	loginWindow      = 10 * time.Minute
	maxLoginBlock    = 10 * time.Minute

	registerPerIPLimit      = 5
	registerPerIPDailyLimit = 20
	registerGlobalLimit     = 100
	registerWindow          = time.Hour
	registerDailyWindow     = 24 * time.Hour
	maxHashConcurrency      = 4
)

// AuthConfig configures account and session handling.
type AuthConfig struct {
	SessionTTL   time.Duration
	CookieSecure bool
	Now          func() time.Time
}

// Auth owns login and cookie sessions.
type Auth struct {
	db    *store.DB
	cfg   AuthConfig
	now   func() time.Time
	limit *loginLimiter
	// Registration is limited separately: per address (hourly and daily) and
	// globally.
	registerIP      *windowLimiter
	registerIPDaily *windowLimiter
	registerGlobal  *windowLimiter
	hashSem         chan struct{}
	logf            func(string, ...any)
}

// NewAuth returns the portal authenticator.
func NewAuth(db *store.DB, cfg AuthConfig) *Auth {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Auth{
		db:              db,
		cfg:             cfg,
		now:             cfg.Now,
		limit:           newLoginLimiter(cfg.Now),
		registerIP:      newWindowLimiter(cfg.Now, registerWindow, registerPerIPLimit),
		registerIPDaily: newWindowLimiter(cfg.Now, registerDailyWindow, registerPerIPDailyLimit),
		registerGlobal:  newWindowLimiter(cfg.Now, registerWindow, registerGlobalLimit),
		hashSem:         make(chan struct{}, maxHashConcurrency),
		logf:            log.Printf,
	}
}

// Credentials are the login request body.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// EnsureAdmin creates or promotes the account declared in gateway
// configuration. That account is the deployment's bootstrap administrator: it
// is activated even if an earlier release left it disabled, and a changed
// configured password rotates the stored hash and revokes its sessions.
func (a *Auth) EnsureAdmin(ctx context.Context, in Credentials) (store.PortalUser, error) {
	username, err := normalizeUsername(in.Username)
	if err != nil {
		return store.PortalUser{}, fmt.Errorf("portal: configured username: %w", err)
	}
	if err := validatePassword(in.Password); err != nil {
		return store.PortalUser{}, fmt.Errorf("portal: configured password: %w", err)
	}
	user, err := a.db.PortalUserByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		hash, err := hashPassword(in.Password)
		if err != nil {
			return store.PortalUser{}, fmt.Errorf("portal: hash password: %w", err)
		}
		now := a.now()
		user = store.PortalUser{
			ID:           "usr_" + rand.Text(),
			Username:     username,
			PasswordHash: hash,
			Role:         store.PortalRoleAdmin,
			Status:       store.PortalUserActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := a.db.CreatePortalUser(ctx, user); err != nil {
			return store.PortalUser{}, fmt.Errorf("portal: create configured user: %w", err)
		}
		return user, nil
	}
	if err != nil {
		return store.PortalUser{}, fmt.Errorf("portal: load configured user: %w", err)
	}
	changedPassword := !verifyPassword(user.PasswordHash, in.Password)
	changedRole := user.Role != store.PortalRoleAdmin
	changedStatus := user.Status != store.PortalUserActive
	if !changedPassword && !changedRole && !changedStatus {
		return user, nil
	}
	if changedPassword {
		hash, err := hashPassword(in.Password)
		if err != nil {
			return store.PortalUser{}, fmt.Errorf("portal: hash password: %w", err)
		}
		// Revoke existing sessions first: once the new password is stored, a
		// failure here would leave old sessions alive until the next restart.
		if err := a.db.DeletePortalSessionsByUser(ctx, user.ID); err != nil {
			return store.PortalUser{}, fmt.Errorf("portal: revoke sessions after password change: %w", err)
		}
		user.PasswordHash = hash
	}
	user.Role = store.PortalRoleAdmin
	user.Status = store.PortalUserActive
	user.UpdatedAt = a.now()
	if err := a.db.UpdatePortalUser(ctx, user); err != nil {
		return store.PortalUser{}, fmt.Errorf("portal: update configured user: %w", err)
	}
	return user, nil
}

// Registration is the result of a successful sign-up: the account, its virtual
// Linx device, and a session token, so the user is signed in immediately.
type Registration struct {
	User   store.PortalUser
	Device store.PortalDevice
	Token  string
}

// Register creates a normal user and its virtual device atomically. The caller
// is responsible for checking whether registration is open and for starting
// provisioning after the transaction commits.
func (a *Auth) Register(ctx context.Context, in Credentials, userAgent, clientIP string) (Registration, error) {
	if retry, allowed := a.registerIP.allow(clientIP); !allowed {
		return Registration{}, rateLimited(retry)
	}
	if retry, allowed := a.registerIPDaily.allow(clientIP); !allowed {
		return Registration{}, rateLimited(retry)
	}
	if retry, allowed := a.registerGlobal.allow("global"); !allowed {
		return Registration{}, rateLimited(retry)
	}
	username, err := normalizeUsername(in.Username)
	if err != nil {
		return Registration{}, err
	}
	if err := validatePassword(in.Password); err != nil {
		return Registration{}, err
	}
	_, err = a.db.PortalUserByUsername(ctx, username)
	if err == nil {
		return Registration{}, apiError(http.StatusConflict, "USERNAME_TAKEN", "该用户名已被占用")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Registration{}, fmt.Errorf("portal: lookup username: %w", err)
	}
	hash, err := a.hashPasswordFor(ctx, in.Password)
	if err != nil {
		return Registration{}, err
	}
	identity, err := NewDeviceIdentity()
	if err != nil {
		return Registration{}, fmt.Errorf("portal: create device identity: %w", err)
	}
	now := a.now()
	user := store.PortalUser{
		ID:           "usr_" + rand.Text(),
		Username:     username,
		PasswordHash: hash,
		Role:         store.PortalRoleUser,
		Status:       store.PortalUserActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	device := store.PortalDevice{
		ID:        "dev_" + rand.Text(),
		UserID:    user.ID,
		Kind:      store.PortalDeviceVirtual,
		MAC:       identity.MAC,
		ClientID:  identity.ClientID,
		State:     store.PortalDeviceProvisioning,
		Step:      stepCreateAgent,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.db.CreatePortalUserWithDevice(ctx, user, device); err != nil {
		if isUniqueViolation(err) {
			return Registration{}, apiError(http.StatusConflict, "USERNAME_TAKEN", "该用户名已被占用")
		}
		return Registration{}, fmt.Errorf("portal: register user: %w", err)
	}
	token, err := a.createSession(ctx, user.ID, userAgent)
	if err != nil {
		// The account and agent exist; the user can sign in without the cookie.
		return Registration{}, err
	}
	return Registration{User: user, Device: device, Token: token}, nil
}

// hashPasswordFor bounds concurrent PBKDF2 work so a registration burst cannot
// saturate the process.
func (a *Auth) hashPasswordFor(ctx context.Context, password string) (string, error) {
	select {
	case a.hashSem <- struct{}{}:
		defer func() { <-a.hashSem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return hashPassword(password)
}

func rateLimited(wait time.Duration) *APIError {
	seconds := int(wait.Seconds() + 0.5)
	if seconds < 1 {
		seconds = 1
	}
	return apiError(http.StatusTooManyRequests, "REGISTRATION_RATE_LIMITED",
		fmt.Sprintf("注册过于频繁，请在 %d 秒后重试", seconds))
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

// Login verifies credentials and creates a session. Failures are rate limited
// by username and client address and never distinguish unknown users from bad
// passwords.
func (a *Auth) Login(ctx context.Context, in Credentials, userAgent, clientIP string) (store.PortalUser, string, error) {
	username := strings.ToLower(strings.TrimSpace(in.Username))
	key := username + "|" + clientIP
	if wait, blocked := a.limit.blocked(key); blocked {
		return store.PortalUser{}, "", apiError(http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS",
			fmt.Sprintf("尝试次数过多，请在 %d 秒后重试", int(wait.Seconds()+0.5)))
	}
	user, err := a.db.PortalUserByUsername(ctx, username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.PortalUser{}, "", fmt.Errorf("portal: lookup user: %w", err)
	}
	valid := err == nil && verifyPassword(user.PasswordHash, in.Password)
	if !valid {
		a.limit.fail(key)
		return store.PortalUser{}, "", apiError(http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码不正确")
	}
	if user.Status != store.PortalUserActive {
		a.limit.fail(key)
		return store.PortalUser{}, "", apiError(http.StatusForbidden, "ACCOUNT_DISABLED", "该账号已被停用，请联系管理员")
	}
	a.limit.succeed(key)
	now := a.now()
	if err := a.db.TouchPortalUserLogin(ctx, user.ID, now); err != nil {
		return store.PortalUser{}, "", err
	}
	user.LastLoginAt = &now
	token, err := a.createSession(ctx, user.ID, userAgent)
	if err != nil {
		return store.PortalUser{}, "", err
	}
	return user, token, nil
}

// Authenticate resolves a cookie token to a user. Expired sessions are deleted.
func (a *Auth) Authenticate(ctx context.Context, token string) (store.PortalUser, error) {
	if token == "" {
		return store.PortalUser{}, errUnauthorized
	}
	hash := tokenHash(token)
	session, err := a.db.PortalSessionByTokenHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return store.PortalUser{}, errUnauthorized
	}
	if err != nil {
		return store.PortalUser{}, fmt.Errorf("portal: load session: %w", err)
	}
	now := a.now()
	if !session.ExpiresAt.After(now) {
		_ = a.db.DeletePortalSession(ctx, hash)
		return store.PortalUser{}, errUnauthorized
	}
	if now.Sub(session.LastSeenAt) > 5*time.Minute {
		_ = a.db.TouchPortalSession(ctx, hash, now)
	}
	user, err := a.db.PortalUserByID(ctx, session.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		_ = a.db.DeletePortalSession(ctx, hash)
		return store.PortalUser{}, errUnauthorized
	}
	if err != nil {
		return store.PortalUser{}, fmt.Errorf("portal: load user: %w", err)
	}
	if user.Status != store.PortalUserActive {
		// A disabled account loses its sessions immediately, even if an
		// administrator did not revoke them explicitly.
		_ = a.db.DeletePortalSession(ctx, hash)
		return store.PortalUser{}, errUnauthorized
	}
	return user, nil
}

// SessionHash returns the database key of a cookie token.
func SessionHash(token string) string { return tokenHash(token) }

// Logout deletes the session behind token.
func (a *Auth) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return a.db.DeletePortalSession(ctx, tokenHash(token))
}

// SetCookie writes the session cookie.
func (a *Auth) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.cfg.CookieSecure,
		MaxAge:   int(a.cfg.SessionTTL / time.Second),
	})
}

// ClearCookie expires the session cookie.
func (a *Auth) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.cfg.CookieSecure,
		MaxAge:   -1,
	})
}

// CookieToken returns the session token from a request.
func CookieToken(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ClientIP is the request's peer address, without the port.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// CleanupExpiredSessions removes sessions that can no longer be used.
func (a *Auth) CleanupExpiredSessions(ctx context.Context) {
	if _, err := a.db.DeleteExpiredPortalSessions(ctx, a.now()); err != nil {
		a.logf("portal: clean expired sessions: %v", err)
	}
}

func (a *Auth) createSession(ctx context.Context, userID, userAgent string) (string, error) {
	token := rand.Text()
	now := a.now()
	session := store.PortalSession{
		TokenHash:  tokenHash(token),
		UserID:     userID,
		CreatedAt:  now,
		ExpiresAt:  now.Add(a.cfg.SessionTTL),
		LastSeenAt: now,
		UserAgent:  truncate(userAgent, 200),
	}
	if err := a.db.CreatePortalSession(ctx, session); err != nil {
		return "", fmt.Errorf("portal: create session: %w", err)
	}
	return token, nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeUsername(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 3 || len(value) > 32 {
		return "", invalidArgument("用户名长度需在 3–32 个字符之间")
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			continue
		}
		switch char {
		case '_', '-', '.':
			continue
		default:
			return "", invalidArgument("用户名只能包含字母、数字、下划线、连字符和点")
		}
	}
	return value, nil
}

func validatePassword(value string) error {
	if len(value) < 8 || len(value) > 128 {
		return invalidArgument("密码长度需在 8–128 个字符之间")
	}
	return nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	var iterations int
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil || iterations < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// loginLimiter blocks a username+IP pair after repeated failures. State is in
// memory: a restart clears it.
type loginLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]*loginEntry
}

type loginEntry struct {
	windowStart time.Time
	failures    int
	blockedTill time.Time
	blockFor    time.Duration
}

func newLoginLimiter(now func() time.Time) *loginLimiter {
	return &loginLimiter{now: now, entries: make(map[string]*loginEntry)}
}

func (l *loginLimiter) blocked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[key]
	if !ok {
		return 0, false
	}
	now := l.now()
	if entry.blockedTill.After(now) {
		return entry.blockedTill.Sub(now), true
	}
	return 0, false
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry, ok := l.entries[key]
	if !ok || now.Sub(entry.windowStart) > loginWindow {
		entry = &loginEntry{windowStart: now}
		l.entries[key] = entry
	}
	entry.failures++
	if entry.failures < maxLoginFailures {
		return
	}
	if entry.blockFor == 0 {
		entry.blockFor = time.Minute
	} else {
		entry.blockFor *= 2
		if entry.blockFor > maxLoginBlock {
			entry.blockFor = maxLoginBlock
		}
	}
	entry.blockedTill = now.Add(entry.blockFor)
}

func (l *loginLimiter) succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// windowLimiter allows at most limit events per key in a fixed window. It is
// used for registration, where a successful sign-up still counts.
type windowLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	window  time.Duration
	limit   int
	entries map[string]*windowEntry
}

type windowEntry struct {
	start time.Time
	count int
}

func newWindowLimiter(now func() time.Time, window time.Duration, limit int) *windowLimiter {
	return &windowLimiter{now: now, window: window, limit: limit, entries: make(map[string]*windowEntry)}
}

func (l *windowLimiter) allow(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if key == "" {
		key = "unknown"
	}
	now := l.now()
	if len(l.entries) > 4096 {
		for candidate, entry := range l.entries {
			if now.Sub(entry.start) >= l.window {
				delete(l.entries, candidate)
			}
		}
	}
	entry := l.entries[key]
	if entry == nil || now.Sub(entry.start) >= l.window {
		entry = &windowEntry{start: now}
		l.entries[key] = entry
	}
	if entry.count >= l.limit {
		return entry.start.Add(l.window).Sub(now), false
	}
	entry.count++
	return 0, true
}
