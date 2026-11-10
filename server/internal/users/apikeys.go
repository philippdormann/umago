package users

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/ids"
)

// Scopes for full-access API keys.
const (
	ScopeReadWrite = "read,write"
	ScopeRead      = "read"
)

// Prefix every minted API key starts with.
const KeyPrefix = "umago_key_"

// APIKey is a full-access key (never carries the secret after creation).
type APIKey struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	Scopes     string `json:"scopes"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	RotatedAt  string `json:"rotated_at,omitempty"`
}

// KeyStore persists API keys.
type KeyStore struct {
	db *database.DB
	mu sync.Mutex
	// touched throttles last_used_at writes to once a minute per key.
	touched map[string]time.Time
}

// NewKeyStore returns a KeyStore.
func NewKeyStore(db *database.DB) *KeyStore {
	return &KeyStore{db: db, touched: map[string]time.Time{}}
}

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Create mints a key and returns it with the raw secret.
func (s *KeyStore) Create(ctx context.Context, name, scopes string) (APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "API key"
	}
	if len(name) > 60 {
		return APIKey{}, "", fmt.Errorf("%w: name must be 60 characters or fewer", ErrInvalid)
	}
	if scopes != ScopeRead {
		scopes = ScopeReadWrite
	}
	raw := KeyPrefix + ids.Random(20)
	k := APIKey{ID: ids.New("key"), Name: name, Prefix: raw[:len(KeyPrefix)+6], Scopes: scopes, CreatedAt: ids.Now()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_keys (id, name, key_hash, prefix, scopes, created_at) VALUES (?,?,?,?,?,?)`,
		k.ID, k.Name, hashKey(raw), k.Prefix, k.Scopes, k.CreatedAt)
	if err != nil {
		return APIKey{}, "", err
	}
	return k, raw, nil
}

// List returns every key, newest first.
func (s *KeyStore) List(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, scopes, expires_at, created_at, last_used_at, rotated_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.Scopes, &k.ExpiresAt, &k.CreatedAt, &k.LastUsedAt, &k.RotatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Delete revokes a key.
func (s *KeyStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Rotate replaces a key's secret, keeping its id, name and scopes. The old
// secret stops working immediately; the new one is returned once.
func (s *KeyStore) Rotate(ctx context.Context, id string) (APIKey, string, error) {
	var k APIKey
	err := s.db.QueryRowContext(ctx, `SELECT id, name, prefix, scopes, expires_at, created_at, last_used_at, rotated_at FROM api_keys WHERE id = ?`, id).
		Scan(&k.ID, &k.Name, &k.Prefix, &k.Scopes, &k.ExpiresAt, &k.CreatedAt, &k.LastUsedAt, &k.RotatedAt)
	if err != nil {
		return APIKey{}, "", ErrNotFound
	}
	raw := KeyPrefix + ids.Random(20)
	k.Prefix = raw[:len(KeyPrefix)+6]
	k.RotatedAt = ids.Now()
	if _, err := s.db.ExecContext(ctx, `UPDATE api_keys SET key_hash = ?, prefix = ?, rotated_at = ? WHERE id = ?`, hashKey(raw), k.Prefix, k.RotatedAt, id); err != nil {
		return APIKey{}, "", err
	}
	return k, raw, nil
}

// Authenticate resolves a raw key, recording its use. Expired keys fail.
func (s *KeyStore) Authenticate(ctx context.Context, raw string) (APIKey, bool) {
	var k APIKey
	if !strings.HasPrefix(raw, KeyPrefix) {
		return k, false
	}
	err := s.db.QueryRowContext(ctx, `SELECT id, name, prefix, scopes, expires_at, created_at, last_used_at, rotated_at FROM api_keys WHERE key_hash = ?`, hashKey(raw)).
		Scan(&k.ID, &k.Name, &k.Prefix, &k.Scopes, &k.ExpiresAt, &k.CreatedAt, &k.LastUsedAt, &k.RotatedAt)
	if err != nil {
		return APIKey{}, false
	}
	if k.ExpiresAt != "" {
		if t, perr := ids.Parse(k.ExpiresAt); perr == nil && !t.After(time.Now()) {
			return APIKey{}, false
		}
	}
	s.mu.Lock()
	last, seen := s.touched[k.ID]
	now := time.Now()
	if !seen || now.Sub(last) > time.Minute {
		s.touched[k.ID] = now
		s.mu.Unlock()
		_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, ids.Format(now), k.ID)
	} else {
		s.mu.Unlock()
	}
	return k, true
}
