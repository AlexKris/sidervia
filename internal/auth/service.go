package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/securefile"
)

var (
	ErrInvalidCredentials = errors.New("authentication failed")
	ErrRateLimited        = errors.New("authentication temporarily limited")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidCSRF        = errors.New("invalid CSRF token")
	ErrTOTPNotPending     = errors.New("TOTP setup is not pending")
	ErrTOTPReplay         = errors.New("TOTP code was already used")
)

const (
	sessionIdle       = 12 * time.Hour
	sessionAbsolute   = 7 * 24 * time.Hour
	passwordWorkLimit = 2
)

type Service struct {
	db           *sql.DB
	cipher       *cryptox.Cipher
	clock        clock.Clock
	ids          identifier.Generator
	passwords    PasswordHasher
	bindingKey   []byte
	accountLabel string
	limiter      *attemptLimiter
	passwordWork chan struct{}
}

type Session struct {
	PublicID          string
	Token             string
	CSRFToken         string
	CreatedAt         time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	TOTPEnabled       bool
}

type TOTPSetup struct {
	Secret    string    `json:"secret"`
	URI       string    `json:"uri"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewService(db *sql.DB, cipher *cryptox.Cipher, c clock.Clock, ids identifier.Generator, passwords PasswordHasher, masterKey []byte, accountLabel string) *Service {
	if c == nil {
		c = clock.Real{}
	}
	if accountLabel == "" {
		accountLabel = "admin"
	}
	return &Service{
		db:           db,
		cipher:       cipher,
		clock:        c,
		ids:          ids,
		passwords:    passwords,
		bindingKey:   cryptox.Derive(masterKey, "session-binding"),
		accountLabel: accountLabel,
		limiter:      newAttemptLimiter(),
		passwordWork: make(chan struct{}, passwordWorkLimit),
	}
}

func (s *Service) AdminExists(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM admin_user WHERE id = 1").Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) BootstrapFromFile(ctx context.Context, path string) (bool, error) {
	exists, err := s.AdminExists(ctx)
	if err != nil || exists {
		return false, err
	}
	if path == "" {
		return false, errors.New("SIDERVIA_BOOTSTRAP_PASSWORD_FILE is required for an empty database")
	}
	raw, err := securefile.Read(path, 4096)
	if err != nil {
		return false, fmt.Errorf("read bootstrap password: %w", err)
	}
	password, err := passwordFromFile(raw)
	if err != nil {
		return false, err
	}
	phc, err := s.passwords.Hash(password)
	if err != nil {
		return false, fmt.Errorf("hash bootstrap password: %w", err)
	}
	now := s.clock.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_user(
        id, password_phc, session_version, failed_login_count, created_at_ms, updated_at_ms
    ) VALUES(1, ?, 1, 0, ?, ?)`, phc, now, now)
	if err != nil {
		return false, fmt.Errorf("create administrator: %w", err)
	}
	if err := s.audit(ctx, tx, "admin.bootstrapped", "local_admin", "", map[string]any{"totp_enabled": false}); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) Login(ctx context.Context, password, code, clientIP, userAgent string) (Session, error) {
	now := s.clock.Now()
	if !s.acquirePasswordWork() {
		return Session{}, ErrRateLimited
	}
	defer s.releasePasswordWork()
	if !s.limiter.Allow(clientIP, now) {
		return Session{}, ErrRateLimited
	}
	admin, err := s.loadAdmin(ctx)
	if err != nil {
		return Session{}, ErrInvalidCredentials
	}
	if admin.LockedUntil.Valid && now.UnixMilli() < admin.LockedUntil.Int64 {
		return Session{}, ErrRateLimited
	}
	passwordOK, verifyErr := s.passwords.Verify(password, admin.PasswordPHC)
	if verifyErr != nil || !passwordOK {
		if err := s.recordFailure(ctx, now); err != nil {
			return Session{}, err
		}
		return Session{}, ErrInvalidCredentials
	}

	var acceptedStep *int64
	if admin.TOTPEnabled {
		secret, err := s.cipher.Open(admin.TOTPSecret, cryptox.AAD("admin_user", "1", "totp_secret_enc"))
		if err != nil {
			return Session{}, ErrInvalidCredentials
		}
		step, ok := ValidateTOTP(string(secret), code, now)
		if !ok {
			if err := s.recordFailure(ctx, now); err != nil {
				return Session{}, err
			}
			return Session{}, ErrInvalidCredentials
		}
		acceptedStep = &step
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	if acceptedStep != nil {
		result, err := tx.ExecContext(ctx, `UPDATE admin_user SET totp_last_used_step = ?, updated_at_ms = ?
            WHERE id = 1 AND (totp_last_used_step IS NULL OR totp_last_used_step < ?)`, *acceptedStep, now.UnixMilli(), *acceptedStep)
		if err != nil {
			return Session{}, err
		}
		rows, _ := result.RowsAffected()
		if rows != 1 {
			return Session{}, ErrTOTPReplay
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE admin_user SET failed_login_count = 0, locked_until_ms = NULL, updated_at_ms = ? WHERE id = 1", now.UnixMilli()); err != nil {
		return Session{}, err
	}
	session, err := s.insertSession(ctx, tx, admin.SessionVersion, admin.TOTPEnabled, clientIP, userAgent, now)
	if err != nil {
		return Session{}, err
	}
	if err := s.audit(ctx, tx, "admin.login_succeeded", "admin_session", session.PublicID, map[string]any{"totp_enabled": admin.TOTPEnabled}); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrUnauthorized
	}
	verifier := identifier.Verifier(token)
	var row struct {
		PublicID, CSRFEnc                               string
		SessionVersion, AdminVersion                    int64
		Created, LastSeen, IdleExpires, AbsoluteExpires int64
		Revoked                                         sql.NullInt64
		TOTPEnabled                                     bool
	}
	var csrf []byte
	err := s.db.QueryRowContext(ctx, `SELECT s.public_id, s.csrf_token_enc, s.session_version,
        s.created_at_ms, s.last_seen_at_ms, s.idle_expires_at_ms, s.absolute_expires_at_ms,
        s.revoked_at_ms, a.session_version, a.totp_enabled
        FROM admin_sessions s JOIN admin_user a ON a.id = 1 WHERE s.token_verifier = ?`, verifier).Scan(
		&row.PublicID, &csrf, &row.SessionVersion, &row.Created, &row.LastSeen, &row.IdleExpires,
		&row.AbsoluteExpires, &row.Revoked, &row.AdminVersion, &row.TOTPEnabled)
	if err != nil || row.Revoked.Valid || row.SessionVersion != row.AdminVersion {
		return Session{}, ErrUnauthorized
	}
	now := s.clock.Now()
	if now.UnixMilli() >= row.IdleExpires || now.UnixMilli() >= row.AbsoluteExpires {
		_, _ = s.db.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at_ms = ? WHERE public_id = ? AND revoked_at_ms IS NULL", now.UnixMilli(), row.PublicID)
		return Session{}, ErrUnauthorized
	}
	csrfPlain, err := s.cipher.Open(csrf, cryptox.AAD("admin_sessions", row.PublicID, "csrf_token_enc"))
	if err != nil {
		return Session{}, ErrUnauthorized
	}
	if now.UnixMilli()-row.LastSeen >= int64(5*time.Minute/time.Millisecond) {
		idle := minTime(now.Add(sessionIdle), time.UnixMilli(row.AbsoluteExpires))
		_, _ = s.db.ExecContext(ctx, `UPDATE admin_sessions SET last_seen_at_ms = ?, idle_expires_at_ms = ?
            WHERE public_id = ? AND revoked_at_ms IS NULL`, now.UnixMilli(), idle.UnixMilli(), row.PublicID)
		row.IdleExpires = idle.UnixMilli()
	}
	return Session{
		PublicID:          row.PublicID,
		Token:             token,
		CSRFToken:         string(csrfPlain),
		CreatedAt:         time.UnixMilli(row.Created),
		IdleExpiresAt:     time.UnixMilli(row.IdleExpires),
		AbsoluteExpiresAt: time.UnixMilli(row.AbsoluteExpires),
		TOTPEnabled:       row.TOTPEnabled,
	}, nil
}

func (s *Service) ValidateCSRF(session Session, value string) error {
	if value == "" || subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(value)) != 1 {
		return ErrInvalidCSRF
	}
	return nil
}

func (s *Service) Logout(ctx context.Context, session Session) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at_ms = ? WHERE public_id = ? AND revoked_at_ms IS NULL", s.clock.Now().UnixMilli(), session.PublicID); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, "admin.logout", "admin_session", session.PublicID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) SetupTOTP(ctx context.Context, session Session, password string) (TOTPSetup, error) {
	admin, err := s.loadAdmin(ctx)
	if err != nil {
		return TOTPSetup{}, ErrUnauthorized
	}
	ok, err := s.verifyPassword(password, admin.PasswordPHC)
	if errors.Is(err, ErrRateLimited) {
		return TOTPSetup{}, err
	}
	if err != nil || !ok {
		return TOTPSetup{}, ErrInvalidCredentials
	}
	secret, err := GenerateTOTPSecret(nil)
	if err != nil {
		return TOTPSetup{}, err
	}
	sealed, err := s.cipher.Seal([]byte(secret), cryptox.AAD("admin_user", "1", "totp_pending_secret_enc"))
	if err != nil {
		return TOTPSetup{}, err
	}
	expires := s.clock.Now().Add(10 * time.Minute)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TOTPSetup{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE admin_user SET totp_pending_secret_enc = ?,
        totp_pending_expires_at_ms = ?, updated_at_ms = ? WHERE id = 1`, sealed, expires.UnixMilli(), s.clock.Now().UnixMilli())
	if err != nil {
		return TOTPSetup{}, err
	}
	if err := s.audit(ctx, tx, "admin.totp_setup_started", "admin_session", session.PublicID, nil); err != nil {
		return TOTPSetup{}, err
	}
	if err := tx.Commit(); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: secret, URI: TOTPURI(secret, s.accountLabel), ExpiresAt: expires}, nil
}

func (s *Service) ConfirmTOTP(ctx context.Context, session Session, code, clientIP, userAgent string) (Session, error) {
	admin, err := s.loadAdmin(ctx)
	if err != nil || len(admin.TOTPPendingSecret) == 0 || !admin.TOTPPendingExpiry.Valid || s.clock.Now().UnixMilli() >= admin.TOTPPendingExpiry.Int64 {
		return Session{}, ErrTOTPNotPending
	}
	secret, err := s.cipher.Open(admin.TOTPPendingSecret, cryptox.AAD("admin_user", "1", "totp_pending_secret_enc"))
	if err != nil {
		return Session{}, ErrTOTPNotPending
	}
	step, ok := ValidateTOTP(string(secret), code, s.clock.Now())
	if !ok {
		return Session{}, ErrInvalidCredentials
	}
	sealed, err := s.cipher.Seal(secret, cryptox.AAD("admin_user", "1", "totp_secret_enc"))
	if err != nil {
		return Session{}, err
	}
	return s.updateSecurityAndRotate(ctx, session, clientIP, userAgent, "admin.totp_enabled", func(tx *sql.Tx, newVersion int64, now int64) error {
		result, err := tx.ExecContext(ctx, `UPDATE admin_user SET totp_secret_enc = ?, totp_pending_secret_enc = NULL,
            totp_pending_expires_at_ms = NULL, totp_enabled = 1, totp_last_used_step = ?,
			session_version = ?, updated_at_ms = ? WHERE id = 1 AND totp_pending_secret_enc = ?
			AND totp_pending_expires_at_ms > ? AND session_version = ?`, sealed, step, newVersion, now,
			admin.TOTPPendingSecret, now, newVersion-1)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrTOTPNotPending
		}
		return nil
	})
}

func (s *Service) DisableTOTP(ctx context.Context, session Session, password, code, clientIP, userAgent string) (Session, error) {
	admin, err := s.loadAdmin(ctx)
	if err != nil || !admin.TOTPEnabled {
		return Session{}, ErrInvalidCredentials
	}
	ok, err := s.verifyPassword(password, admin.PasswordPHC)
	if errors.Is(err, ErrRateLimited) {
		return Session{}, err
	}
	if err != nil || !ok {
		return Session{}, ErrInvalidCredentials
	}
	secret, err := s.cipher.Open(admin.TOTPSecret, cryptox.AAD("admin_user", "1", "totp_secret_enc"))
	if err != nil {
		return Session{}, ErrInvalidCredentials
	}
	step, ok := ValidateTOTP(string(secret), code, s.clock.Now())
	if !ok || (admin.TOTPLastStep.Valid && step <= admin.TOTPLastStep.Int64) {
		return Session{}, ErrInvalidCredentials
	}
	return s.updateSecurityAndRotate(ctx, session, clientIP, userAgent, "admin.totp_disabled", func(tx *sql.Tx, newVersion int64, now int64) error {
		result, err := tx.ExecContext(ctx, `UPDATE admin_user SET totp_secret_enc = NULL, totp_pending_secret_enc = NULL,
            totp_pending_expires_at_ms = NULL, totp_enabled = 0, totp_last_used_step = NULL,
			session_version = ?, updated_at_ms = ? WHERE id = 1 AND totp_enabled = 1
			AND totp_secret_enc = ? AND session_version = ?`, newVersion, now, admin.TOTPSecret, newVersion-1)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrTOTPReplay
		}
		return nil
	})
}

func (s *Service) ChangePassword(ctx context.Context, session Session, current, next, clientIP, userAgent string) (Session, error) {
	admin, err := s.loadAdmin(ctx)
	if err != nil {
		return Session{}, ErrUnauthorized
	}
	if !s.acquirePasswordWork() {
		return Session{}, ErrRateLimited
	}
	defer s.releasePasswordWork()
	ok, err := s.passwords.Verify(current, admin.PasswordPHC)
	if err != nil || !ok {
		return Session{}, ErrInvalidCredentials
	}
	phc, err := s.passwords.Hash(next)
	if err != nil {
		return Session{}, err
	}
	return s.updateSecurityAndRotate(ctx, session, clientIP, userAgent, "admin.password_changed", func(tx *sql.Tx, newVersion int64, now int64) error {
		result, err := tx.ExecContext(ctx, `UPDATE admin_user SET password_phc = ?, session_version = ?, updated_at_ms = ?
			WHERE id = 1 AND password_phc = ? AND session_version = ?`, phc, newVersion, now, admin.PasswordPHC, newVersion-1)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrInvalidCredentials
		}
		return nil
	})
}

func (s *Service) ResetPasswordFromFile(ctx context.Context, path string, disableTOTP bool) error {
	raw, err := securefile.Read(path, 4096)
	if err != nil {
		return err
	}
	password, err := passwordFromFile(raw)
	if err != nil {
		return err
	}
	phc, err := s.passwords.Hash(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.clock.Now().UnixMilli()
	var result sql.Result
	if disableTOTP {
		result, err = tx.ExecContext(ctx, `UPDATE admin_user SET password_phc = ?, totp_secret_enc = NULL,
            totp_pending_secret_enc = NULL, totp_pending_expires_at_ms = NULL, totp_enabled = 0,
            totp_last_used_step = NULL, session_version = session_version + 1, failed_login_count = 0,
            locked_until_ms = NULL, updated_at_ms = ? WHERE id = 1`, phc, now)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE admin_user SET password_phc = ?, session_version = session_version + 1,
            failed_login_count = 0, locked_until_ms = NULL, updated_at_ms = ? WHERE id = 1`, phc, now)
	}
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("administrator is not configured")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at_ms = ? WHERE revoked_at_ms IS NULL", now); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, "admin.password_reset", "local_admin", "", map[string]any{"totp_disabled": disableTOTP}); err != nil {
		return err
	}
	return tx.Commit()
}

type adminRow struct {
	PasswordPHC       string
	TOTPEnabled       bool
	TOTPSecret        []byte
	TOTPPendingSecret []byte
	TOTPPendingExpiry sql.NullInt64
	TOTPLastStep      sql.NullInt64
	SessionVersion    int64
	LockedUntil       sql.NullInt64
}

func (s *Service) loadAdmin(ctx context.Context) (adminRow, error) {
	var row adminRow
	err := s.db.QueryRowContext(ctx, `SELECT password_phc, totp_enabled, totp_secret_enc,
        totp_pending_secret_enc, totp_pending_expires_at_ms, totp_last_used_step,
        session_version, locked_until_ms FROM admin_user WHERE id = 1`).Scan(
		&row.PasswordPHC, &row.TOTPEnabled, &row.TOTPSecret, &row.TOTPPendingSecret,
		&row.TOTPPendingExpiry, &row.TOTPLastStep, &row.SessionVersion, &row.LockedUntil)
	return row, err
}

func (s *Service) recordFailure(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var next int
	if err := tx.QueryRowContext(ctx, `UPDATE admin_user
        SET failed_login_count = failed_login_count + 1, locked_until_ms = NULL, updated_at_ms = ?
        WHERE id = 1 RETURNING failed_login_count`, now.UnixMilli()).Scan(&next); err != nil {
		return err
	}
	var lockedUntil *int64
	if next >= 5 {
		shift := min(next-5, 9)
		delay := 30 * time.Second * time.Duration(1<<shift)
		if delay > 15*time.Minute {
			delay = 15 * time.Minute
		}
		value := now.Add(delay).UnixMilli()
		lockedUntil = &value
		if _, err := tx.ExecContext(ctx, "UPDATE admin_user SET locked_until_ms = ? WHERE id = 1", value); err != nil {
			return err
		}
	}
	if err := s.auditOutcome(ctx, tx, "admin.login_failed", "anonymous", "", "failure", map[string]any{"rate_limit_applied": lockedUntil != nil}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) insertSession(ctx context.Context, tx *sql.Tx, sessionVersion int64, totpEnabled bool, clientIP, userAgent string, now time.Time) (Session, error) {
	publicID, err := s.ids.Object("sess")
	if err != nil {
		return Session{}, err
	}
	token, err := s.ids.Token(32)
	if err != nil {
		return Session{}, err
	}
	csrf, err := s.ids.Token(32)
	if err != nil {
		return Session{}, err
	}
	csrfEnc, err := s.cipher.Seal([]byte(csrf), cryptox.AAD("admin_sessions", publicID, "csrf_token_enc"))
	if err != nil {
		return Session{}, err
	}
	idle := now.Add(sessionIdle)
	absolute := now.Add(sessionAbsolute)
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_sessions(public_id, token_verifier, csrf_token_enc,
        session_version, created_at_ms, last_seen_at_ms, idle_expires_at_ms, absolute_expires_at_ms,
        ip_prefix_hmac, user_agent_hmac) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		publicID, identifier.Verifier(token), csrfEnc, sessionVersion, now.UnixMilli(), now.UnixMilli(),
		idle.UnixMilli(), absolute.UnixMilli(), s.bindingHMAC(ipPrefix(clientIP)), s.bindingHMAC(limitString(userAgent, 512)))
	if err != nil {
		return Session{}, err
	}
	return Session{PublicID: publicID, Token: token, CSRFToken: csrf, CreatedAt: now, IdleExpiresAt: idle, AbsoluteExpiresAt: absolute, TOTPEnabled: totpEnabled}, nil
}

func (s *Service) updateSecurityAndRotate(ctx context.Context, current Session, clientIP, userAgent, eventType string, update func(*sql.Tx, int64, int64) error) (Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	var oldVersion int64
	if err := tx.QueryRowContext(ctx, "SELECT session_version FROM admin_user WHERE id = 1").Scan(&oldVersion); err != nil {
		return Session{}, err
	}
	newVersion := oldVersion + 1
	now := s.clock.Now()
	if err := update(tx, newVersion, now.UnixMilli()); err != nil {
		return Session{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at_ms = ? WHERE public_id <> ? AND revoked_at_ms IS NULL", now.UnixMilli(), current.PublicID); err != nil {
		return Session{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at_ms = ? WHERE public_id = ? AND revoked_at_ms IS NULL", now.UnixMilli(), current.PublicID); err != nil {
		return Session{}, err
	}
	rotated, err := s.insertSession(ctx, tx, newVersion, false, clientIP, userAgent, now)
	if err != nil {
		return Session{}, err
	}
	var enabled bool
	if err := tx.QueryRowContext(ctx, "SELECT totp_enabled FROM admin_user WHERE id = 1").Scan(&enabled); err != nil {
		return Session{}, err
	}
	rotated.TOTPEnabled = enabled
	if err := s.audit(ctx, tx, eventType, "admin_session", rotated.PublicID, map[string]any{"sessions_rotated": true}); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	return rotated, nil
}

func (s *Service) audit(ctx context.Context, tx *sql.Tx, eventType, actorKind, actorID string, metadata map[string]any) error {
	return s.auditOutcome(ctx, tx, eventType, actorKind, actorID, "success", metadata)
}

func (s *Service) auditOutcome(ctx context.Context, tx *sql.Tx, eventType, actorKind, actorID, outcome string, metadata map[string]any) error {
	publicID, err := s.ids.Object("audit")
	if err != nil {
		return err
	}
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["schema_version"] = 1
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	if len(body) > 4096 {
		return errors.New("authentication audit metadata is too large")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(public_id, event_type, actor_kind, actor_id,
        target_kind, target_id, outcome, metadata_json, created_at_ms)
		VALUES(?, ?, ?, NULLIF(?, ''), 'admin', '1', ?, ?, ?)`, publicID, eventType,
		actorKind, actorID, outcome, string(body), s.clock.Now().UnixMilli())
	return err
}

func (s *Service) bindingHMAC(value string) string {
	h := hmac.New(sha256.New, s.bindingKey)
	_, _ = h.Write([]byte(value))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (s *Service) verifyPassword(password, phc string) (bool, error) {
	if !s.acquirePasswordWork() {
		return false, ErrRateLimited
	}
	defer s.releasePasswordWork()
	return s.passwords.Verify(password, phc)
}

func (s *Service) acquirePasswordWork() bool {
	select {
	case s.passwordWork <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) releasePasswordWork() {
	<-s.passwordWork
}

func ipPrefix(value string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return "unknown"
	}
	bits := 56
	if addr.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(addr, bits).Masked().String()
}

func passwordFromFile(raw []byte) (string, error) {
	value := string(raw)
	value = strings.TrimSuffix(value, "\n")
	value = strings.TrimSuffix(value, "\r")
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New("password file must contain exactly one line")
	}
	if err := ValidatePassword(value); err != nil {
		return "", err
	}
	return value, nil
}

func limitString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
