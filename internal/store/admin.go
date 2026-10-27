package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---------- the admin ----------

type Admin struct {
	ID           int64
	Username     string
	PasswordHash string
	UpdatedAt    time.Time
}

// SetAdmin creates the admin, or renames the one there is and gives it a new password.
// There is only ever one admin.
func (s *Store) SetAdmin(ctx context.Context, username, hash string) (created bool, err error) {
	tag, err := s.db.Exec(ctx, `UPDATE admins SET username = $1, password_hash = $2, updated_at = now()
		WHERE id = (SELECT min(id) FROM admins)`, username, hash)
	if err != nil || tag.RowsAffected() > 0 {
		return false, err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO admins (username, password_hash) VALUES ($1, $2)`, username, hash)
	return err == nil, err
}

func (s *Store) AdminByUsername(ctx context.Context, username string) (Admin, error) {
	return s.admin(ctx, `SELECT id, username, password_hash, updated_at FROM admins WHERE username = $1`, username)
}

func (s *Store) AdminByID(ctx context.Context, id int64) (Admin, error) {
	return s.admin(ctx, `SELECT id, username, password_hash, updated_at FROM admins WHERE id = $1`, id)
}

func (s *Store) admin(ctx context.Context, q string, arg any) (Admin, error) {
	var a Admin
	err := s.db.QueryRow(ctx, q, arg).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// HasAdmin tells whether anyone can sign in yet.
func (s *Store) HasAdmin(ctx context.Context) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM admins)`).Scan(&ok)
	return ok, err
}

func (s *Store) UpdateAdmin(ctx context.Context, id int64, username, hash string) error {
	_, err := s.db.Exec(ctx, `UPDATE admins SET username = $2, password_hash = $3, updated_at = now() WHERE id = $1`, id, username, hash)
	return taken(err)
}

// ---------- sessions ----------

type Session struct {
	TokenHash string
	AdminID   int64
	CSRF      string
	UserAgent string
	IP        string
	CreatedAt time.Time
	SeenAt    time.Time
	ExpiresAt time.Time
}

func (s *Store) CreateSession(ctx context.Context, ses Session) error {
	_, err := s.db.Exec(ctx, `INSERT INTO sessions (token_hash, admin_id, csrf, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, ses.TokenHash, ses.AdminID, ses.CSRF, ses.UserAgent, ses.IP, ses.ExpiresAt)
	return err
}

// SessionByToken finds a live session by the hash of its token.
func (s *Store) SessionByToken(ctx context.Context, tokenHash string) (Session, error) {
	var ses Session
	err := s.db.QueryRow(ctx, `SELECT token_hash, admin_id, csrf, user_agent, ip, created_at, seen_at, expires_at
		FROM sessions WHERE token_hash = $1 AND expires_at > now()`, tokenHash).
		Scan(&ses.TokenHash, &ses.AdminID, &ses.CSRF, &ses.UserAgent, &ses.IP, &ses.CreatedAt, &ses.SeenAt, &ses.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ses, ErrNotFound
	}
	return ses, err
}

// TouchSession moves a session's expiry forward, so an admin who keeps using it stays signed in.
func (s *Store) TouchSession(ctx context.Context, tokenHash string, expires time.Time) error {
	_, err := s.db.Exec(ctx, `UPDATE sessions SET seen_at = now(), expires_at = $2 WHERE token_hash = $1`, tokenHash, expires)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteOtherSessions signs the admin out everywhere but here. It gives how many it ended.
func (s *Store) DeleteOtherSessions(ctx context.Context, adminID int64, keep string) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE admin_id = $1 AND token_hash <> $2`, adminID, keep)
	return tag.RowsAffected(), err
}

func (s *Store) Sessions(ctx context.Context, adminID int64) ([]Session, error) {
	rows, err := s.db.Query(ctx, `SELECT token_hash, admin_id, csrf, user_agent, ip, created_at, seen_at, expires_at
		FROM sessions WHERE admin_id = $1 AND expires_at > now() ORDER BY seen_at DESC`, adminID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) {
		var ses Session
		err := r.Scan(&ses.TokenHash, &ses.AdminID, &ses.CSRF, &ses.UserAgent, &ses.IP, &ses.CreatedAt, &ses.SeenAt, &ses.ExpiresAt)
		return ses, err
	})
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	return err
}
