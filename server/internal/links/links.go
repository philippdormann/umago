// Package links manages short links and tracking pixels. A hit on a slug
// records a click event and redirects (links) or serves a 1px gif (pixels).
package links

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/ids"
)

// Kinds.
const (
	KindLink  = "link"
	KindPixel = "pixel"
)

// ErrNotFound is returned when a slug does not exist.
var ErrNotFound = errors.New("link not found")

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid link")

var slugRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// Link is one short link or pixel.
type Link struct {
	ID        string `json:"id"`
	SiteID    string `json:"site_id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	Clicks    int    `json:"clicks"`
}

// Store persists links.
type Store struct{ db *database.DB }

// New returns a Store.
func New(db *database.DB) *Store { return &Store{db: db} }

// Create adds a link or pixel, recording who created it for the audit trail.
func (s *Store) Create(ctx context.Context, siteID, slug, name, rawURL, kind, createdBy string) (Link, error) {
	slug = strings.TrimSpace(slug)
	if !slugRe.MatchString(slug) {
		return Link{}, fmt.Errorf("%w: slug must be 1-100 chars of letters, numbers, - or _", ErrInvalid)
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || len(rawURL) > 500 || (!strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://")) {
		return Link{}, fmt.Errorf("%w: url must be an absolute http(s) URL", ErrInvalid)
	}
	if kind != KindPixel {
		kind = KindLink
	}
	name = strings.TrimSpace(name)
	if len(name) > 100 {
		name = name[:100]
	}
	l := Link{ID: ids.New("lnk"), SiteID: siteID, Slug: slug, Name: name, URL: rawURL, Kind: kind, CreatedAt: ids.Now(), CreatedBy: strings.TrimSpace(createdBy)}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO links (id, site_id, slug, name, url, kind, created_at, created_by) VALUES (?,?,?,?,?,?,?,?)`,
		l.ID, l.SiteID, l.Slug, l.Name, l.URL, l.Kind, l.CreatedAt, l.CreatedBy); err != nil {
		if database.IsUniqueViolation(err) {
			return Link{}, fmt.Errorf("%w: slug is already taken", ErrInvalid)
		}
		return Link{}, err
	}
	return l, nil
}

// List returns a site's links with click counts.
func (s *Store) List(ctx context.Context, siteID string) ([]Link, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.id, l.site_id, l.slug, l.name, l.url, l.kind, l.created_at, l.created_by,
		(SELECT COUNT(*) FROM events e WHERE e.site_id = l.site_id AND e.name = l.slug AND (e.kind = 'link' OR e.kind = 'pixel'))
		FROM links l WHERE l.site_id = ? ORDER BY l.created_at DESC`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Link{}
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.SiteID, &l.Slug, &l.Name, &l.URL, &l.Kind, &l.CreatedAt, &l.CreatedBy, &l.Clicks); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// BySlug resolves a slug to its link.
func (s *Store) BySlug(ctx context.Context, slug string) (Link, error) {
	var l Link
	err := s.db.QueryRowContext(ctx, `SELECT id, site_id, slug, name, url, kind, created_at, created_by FROM links WHERE slug = ?`, slug).
		Scan(&l.ID, &l.SiteID, &l.Slug, &l.Name, &l.URL, &l.Kind, &l.CreatedAt, &l.CreatedBy)
	if err != nil {
		return Link{}, ErrNotFound
	}
	return l, nil
}

// Delete removes a link; its recorded clicks stay in events.
func (s *Store) Delete(ctx context.Context, siteID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM links WHERE id = ? AND site_id = ?`, id, siteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
