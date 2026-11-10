package users

import (
	"context"
	"regexp"
	"strings"

	"github.com/philippdormann/umago/server/internal/ids"
)

// Share is a public read-only link to a site's dashboard data.
type Share struct {
	ID        string `json:"id"`
	SiteID    string `json:"site_id"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
}

var shareSlugRe = regexp.MustCompile(`^[A-Za-z0-9_-]{4,100}$`)

// ListShares returns a site's shares.
func (s *Store) ListShares(ctx context.Context, siteID string) ([]Share, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, site_id, slug, created_at, created_by FROM shares WHERE site_id = ? ORDER BY created_at`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Share{}
	for rows.Next() {
		var sh Share
		if err := rows.Scan(&sh.ID, &sh.SiteID, &sh.Slug, &sh.CreatedAt, &sh.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// CreateShare mints a share slug for a site, recording who created it.
func (s *Store) CreateShare(ctx context.Context, siteID, createdBy string) (Share, error) {
	sh := Share{ID: ids.New("shr"), SiteID: siteID, Slug: ids.Random(8), CreatedAt: ids.Now(), CreatedBy: strings.TrimSpace(createdBy)}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO shares (id, site_id, slug, created_at, created_by) VALUES (?,?,?,?,?)`,
		sh.ID, sh.SiteID, sh.Slug, sh.CreatedAt, sh.CreatedBy); err != nil {
		return Share{}, err
	}
	return sh, nil
}

// ShareBySlug resolves a public slug.
func (s *Store) ShareBySlug(ctx context.Context, slug string) (Share, error) {
	slug = strings.TrimSpace(slug)
	if !shareSlugRe.MatchString(slug) {
		return Share{}, ErrNotFound
	}
	var sh Share
	err := s.db.QueryRowContext(ctx, `SELECT id, site_id, slug, created_at, created_by FROM shares WHERE slug = ?`, slug).
		Scan(&sh.ID, &sh.SiteID, &sh.Slug, &sh.CreatedAt, &sh.CreatedBy)
	if err != nil {
		return Share{}, ErrNotFound
	}
	return sh, nil
}

// DeleteShare revokes a share.
func (s *Store) DeleteShare(ctx context.Context, siteID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM shares WHERE id = ? AND site_id = ?`, id, siteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
