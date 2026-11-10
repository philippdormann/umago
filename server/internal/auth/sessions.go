package auth

import (
	"context"
	"database/sql"
	"errors"
	"github.com/philippdormann/umago/server/internal/database"
	"time"

	"github.com/philippdormann/umago/server/internal/ids"
)

// SessionStore persists admin sessions so a restart does not log the admin out.
type SessionStore struct{ db *database.DB }

// NewSessionStore returns a SessionStore over db.
func NewSessionStore(db *database.DB) *SessionStore { return &SessionStore{db: db} }

// Save records a session token hash bound to the password it was created
// with. userID is "" for the env admin.
func (s *SessionStore) Save(ctx context.Context, tokenHash, passwordHash string, expires time.Time, userID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, password_hash, expires_at, created_at, user_id) VALUES (?,?,?,?,?)`,
		tokenHash, passwordHash, ids.Format(expires), ids.Now(), userID)
	return err
}

// Lookup returns the password hash, expiry and user id for a token hash.
func (s *SessionStore) Lookup(ctx context.Context, tokenHash string) (passwordHash, userID string, expires time.Time, ok bool, err error) {
	var exp string
	err = s.db.QueryRowContext(ctx, `SELECT password_hash, user_id, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&passwordHash, &userID, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", time.Time{}, false, nil
	}
	if err != nil {
		return "", "", time.Time{}, false, err
	}
	expires, _ = ids.Parse(exp)
	return passwordHash, userID, expires, true, nil
}

// Delete removes a session.
func (s *SessionStore) Delete(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// Prune removes expired sessions.
func (s *SessionStore) Prune(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, ids.Format(now))
	return err
}
