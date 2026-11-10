// Package users manages accounts, teams, site access and TOTP two-factor
// auth. The single env-admin from before still works: it is bootstrapped
// into this table on startup.
package users

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"strings"

	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/ids"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// Roles, strongest first.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// Team roles.
const (
	TeamOwner   = "owner"
	TeamManager = "manager"
	TeamMember  = "member"
	TeamViewer  = "viewer"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid")

// User is one account (never carries hashes).
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	TOTPEnabled bool   `json:"totp_enabled"`
	CreatedAt   string `json:"created_at"`
}

// Team groups sites and members.
type Team struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// Member is one team membership.
type Member struct {
	TeamID   string `json:"team_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Store persists users and teams.
type Store struct{ db *database.DB }

// New returns a Store.
func New(db *database.DB) *Store { return &Store{db: db} }

func validRole(r string) bool { return r == RoleAdmin || r == RoleMember || r == RoleViewer }
func validTeamRole(r string) bool {
	return r == TeamOwner || r == TeamManager || r == TeamMember || r == TeamViewer
}

// HashPassword hashes a password for storage.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword compares a password against its hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// Bootstrap ensures the env admin exists as a user row, creating it on
// first start. It never changes an existing row.
func (s *Store) Bootstrap(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	h, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO users (id, username, pass_hash, role, created_at) VALUES (?,?,?,?,?)`,
		ids.New("usr"), username, h, RoleAdmin, ids.Now())
	return err
}

// Count returns the number of users.
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	return n, s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
}

// Create adds a user.
func (s *Store) Create(ctx context.Context, username, password, role string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 60 {
		return User{}, fmt.Errorf("%w: username must be 1-60 chars", ErrInvalid)
	}
	if len(password) < 8 {
		return User{}, fmt.Errorf("%w: password must be at least 8 characters", ErrInvalid)
	}
	if !validRole(role) {
		role = RoleViewer
	}
	h, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	u := User{ID: ids.New("usr"), Username: username, Role: role, CreatedAt: ids.Now()}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO users (id, username, pass_hash, role, created_at) VALUES (?,?,?,?,?)`,
		u.ID, u.Username, h, u.Role, u.CreatedAt); err != nil {
		if database.IsUniqueViolation(err) {
			return User{}, fmt.Errorf("%w: username is taken", ErrInvalid)
		}
		return User{}, err
	}
	return u, nil
}

// Get returns one user.
func (s *Store) Get(ctx context.Context, id string) (User, error) {
	var u User
	var totp int
	err := s.db.QueryRowContext(ctx, `SELECT id, username, role, totp_enabled, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.Role, &totp, &u.CreatedAt)
	if err != nil {
		return User{}, ErrNotFound
	}
	u.TOTPEnabled = totp == 1
	return u, nil
}

// GetByUsername returns one user by name.
func (s *Store) GetByUsername(ctx context.Context, username string) (User, error) {
	var u User
	var totp int
	err := s.db.QueryRowContext(ctx, `SELECT id, username, role, totp_enabled, created_at FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.Role, &totp, &u.CreatedAt)
	if err != nil {
		return User{}, ErrNotFound
	}
	u.TOTPEnabled = totp == 1
	return u, nil
}

// PassHash returns the stored password hash for verification.
func (s *Store) PassHash(ctx context.Context, id string) (string, error) {
	var h string
	if err := s.db.QueryRowContext(ctx, `SELECT pass_hash FROM users WHERE id = ?`, id).Scan(&h); err != nil {
		return "", ErrNotFound
	}
	return h, nil
}

// List returns every user.
func (s *Store) List(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, role, totp_enabled, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var totp int
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &totp, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.TOTPEnabled = totp == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetRole changes a user's global role.
func (s *Store) SetRole(ctx context.Context, id, role string) error {
	if !validRole(role) {
		return fmt.Errorf("%w: unknown role", ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPassword changes a user's password.
func (s *Store) SetPassword(ctx context.Context, id, password string) error {
	if len(password) < 8 {
		return fmt.Errorf("%w: password must be at least 8 characters", ErrInvalid)
	}
	h, err := HashPassword(password)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET pass_hash = ? WHERE id = ?`, h, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a user, their memberships and backup codes. The last admin
// cannot be deleted.
func (s *Store) Delete(ctx context.Context, id string) error {
	var role string
	if err := s.db.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, id).Scan(&role); err != nil {
		return ErrNotFound
	}
	if role == RoleAdmin {
		var n int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&n)
		if n <= 1 {
			return fmt.Errorf("%w: cannot delete the last admin", ErrInvalid)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM team_members WHERE user_id = ?`,
		`DELETE FROM backup_codes WHERE user_id = ?`,
		`DELETE FROM sessions WHERE user_id = ?`,
		`DELETE FROM users WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- teams ----

// CreateTeam adds a team with the caller as owner.
func (s *Store) CreateTeam(ctx context.Context, name, ownerID string) (Team, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return Team{}, fmt.Errorf("%w: name must be 1-80 chars", ErrInvalid)
	}
	t := Team{ID: ids.New("tem"), Name: name, CreatedAt: ids.Now()}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Team{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO teams (id, name, created_at) VALUES (?,?,?)`, t.ID, t.Name, t.CreatedAt); err != nil {
		return Team{}, err
	}
	if ownerID != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO team_members (team_id, user_id, role) VALUES (?,?,?)`, t.ID, ownerID, TeamOwner); err != nil {
			return Team{}, err
		}
	}
	return t, tx.Commit()
}

// ListTeams returns every team.
func (s *Store) ListTeams(ctx context.Context) ([]Team, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at FROM teams ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTeam returns one team.
func (s *Store) GetTeam(ctx context.Context, id string) (Team, error) {
	var t Team
	if err := s.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM teams WHERE id = ?`, id).Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
		return Team{}, ErrNotFound
	}
	return t, nil
}

// RenameTeam renames a team.
func (s *Store) RenameTeam(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return fmt.Errorf("%w: name must be 1-80 chars", ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE teams SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTeam removes a team, its memberships, and unassigns its sites.
func (s *Store) DeleteTeam(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM team_members WHERE team_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sites SET owner_team_id = '' WHERE owner_team_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// Members lists a team's memberships.
func (s *Store) Members(ctx context.Context, teamID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.team_id, m.user_id, u.username, m.role FROM team_members m
		JOIN users u ON u.id = m.user_id WHERE m.team_id = ? ORDER BY u.username`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Username, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMember adds a user to a team.
func (s *Store) AddMember(ctx context.Context, teamID, userID, role string) error {
	if !validTeamRole(role) {
		role = TeamMember
	}
	if _, err := s.Get(ctx, userID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO team_members (team_id, user_id, role) VALUES (?,?,?)
		ON CONFLICT(team_id, user_id) DO UPDATE SET role = excluded.role`, teamID, userID, role); err != nil {
		return err
	}
	return nil
}

// RemoveMember removes a user from a team. The last owner stays.
func (s *Store) RemoveMember(ctx context.Context, teamID, userID string) error {
	var role string
	err := s.db.QueryRowContext(ctx, `SELECT role FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID).Scan(&role)
	if err != nil {
		return ErrNotFound
	}
	if role == TeamOwner {
		var n int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_members WHERE team_id = ? AND role = 'owner'`, teamID).Scan(&n)
		if n <= 1 {
			return fmt.Errorf("%w: cannot remove the last owner", ErrInvalid)
		}
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TeamRole returns a user's role in a team, or "" when not a member.
func (s *Store) TeamRole(ctx context.Context, teamID, userID string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx, `SELECT role FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID).Scan(&role)
	if err != nil {
		return "", nil
	}
	return role, nil
}

// ---- site access ----

// SiteAccess reports whether a user may read or write a site. Global admins
// bypass teams. Unowned sites are open to every signed-in user.
func (s *Store) SiteAccess(ctx context.Context, userID, globalRole, siteID string) (read, write bool, err error) {
	if globalRole == RoleAdmin {
		return true, true, nil
	}
	var team string
	if err := s.db.QueryRowContext(ctx, `SELECT owner_team_id FROM sites WHERE id = ?`, siteID).Scan(&team); err != nil {
		return false, false, ErrNotFound
	}
	if team == "" {
		return true, globalRole == RoleMember, nil
	}
	role, err := s.TeamRole(ctx, team, userID)
	if err != nil || role == "" {
		return false, false, err
	}
	if role == TeamViewer {
		return true, false, nil
	}
	return true, true, nil
}

// ---- TOTP ----

// TOTPSetup starts enrollment, returning the secret and otpauth URL for the
// QR code. Confirm with a code to enable.
func (s *Store) TOTPSetup(ctx context.Context, userID, username string) (secret, url, qr string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Umago", AccountName: username})
	if err != nil {
		return "", "", "", err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET totp_secret = ? WHERE id = ?`, key.Secret(), userID); err != nil {
		return "", "", "", err
	}
	img, err := key.Image(256, 256)
	if err != nil {
		return "", "", "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", "", "", err
	}
	qr = "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	return key.Secret(), key.URL(), qr, nil
}

// TOTPConfirm enables 2FA after a valid code, minting backup codes.
func (s *Store) TOTPConfirm(ctx context.Context, userID, code string) ([]string, error) {
	var secret string
	if err := s.db.QueryRowContext(ctx, `SELECT totp_secret FROM users WHERE id = ?`, userID).Scan(&secret); err != nil || secret == "" {
		return nil, fmt.Errorf("%w: no 2FA setup in progress", ErrInvalid)
	}
	if !totp.Validate(code, secret) {
		return nil, fmt.Errorf("%w: wrong code", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_enabled = 1 WHERE id = ?`, userID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 0, 10)
	for range 10 {
		var b [5]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		code := hex.EncodeToString(b[:])
		sum := sha256.Sum256([]byte(code))
		if _, err := tx.ExecContext(ctx, `INSERT INTO backup_codes (id, user_id, code_hash, created_at) VALUES (?,?,?,?)`,
			ids.New("bcp"), userID, hex.EncodeToString(sum[:]), ids.Now()); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, tx.Commit()
}

// TOTPDisable turns 2FA off and burns the secret and codes.
func (s *Store) TOTPDisable(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret = '', totp_enabled = 0 WHERE id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// TOTPVerify checks a code or an unused backup code (which it consumes).
func (s *Store) TOTPVerify(ctx context.Context, userID, code string) bool {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if code == "" {
		return false
	}
	var secret string
	var enabled int
	if err := s.db.QueryRowContext(ctx, `SELECT totp_secret, totp_enabled FROM users WHERE id = ?`, userID).Scan(&secret, &enabled); err != nil || enabled != 1 {
		return false
	}
	if secret != "" && totp.Validate(code, secret) {
		return true
	}
	sum := sha256.Sum256([]byte(code))
	// Backup codes compare per-row in Go to stay constant-time; the per-user
	// table is tiny (10 rows).
	rows, err := s.db.QueryContext(ctx, `SELECT id, code_hash FROM backup_codes WHERE user_id = ? AND used = 0`, userID)
	if err != nil {
		return false
	}
	defer rows.Close()
	target := hex.EncodeToString(sum[:])
	matched := ""
	for rows.Next() {
		var rid, hash string
		if err := rows.Scan(&rid, &hash); err != nil {
			continue
		}
		if matched == "" && subtle.ConstantTimeCompare([]byte(hash), []byte(target)) == 1 {
			matched = rid
		}
	}
	rows.Close()
	if matched != "" {
		_, _ = s.db.ExecContext(ctx, `UPDATE backup_codes SET used = 1 WHERE id = ?`, matched)
		return true
	}
	return false
}
