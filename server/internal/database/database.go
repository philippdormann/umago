// Package database opens the configured database (SQLite or Postgres),
// applies migrations, and rebinds query placeholders per dialect.
//
// Callers use *DB exactly like *sql.DB: every query method rebinds `?`
// placeholders to $1, $2, ... when talking to Postgres, so the rest of
// the codebase stays on one placeholder style.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/philippdormann/umago/server/migrations"
)

// Dialect is the database backend in use.
type Dialect int

const (
	// SQLite is the default backend: one file, zero setup.
	SQLite Dialect = iota
	// Postgres is used when UMAGO_DATABASE_URL is set.
	Postgres
)

// String names the dialect for logs.
func (d Dialect) String() string {
	if d == Postgres {
		return "postgres"
	}
	return "sqlite"
}

// DB wraps *sql.DB with its dialect. All query methods accept `?`
// placeholders and rebind them for Postgres.
type DB struct {
	*sql.DB
	Dialect Dialect
}

// Wrap attaches a dialect to an existing connection (e.g. in tests).
func Wrap(db *sql.DB, d Dialect) *DB { return &DB{DB: db, Dialect: d} }

// Rebind rewrites `?` placeholders to $1, $2, ... for Postgres and leaves
// the query untouched for SQLite. Placeholders inside string literals,
// quoted identifiers and comments are left alone.
func Rebind(query string, d Dialect) string {
	if d != Postgres {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	const (
		code = iota
		single
		double
		lineComment
		blockComment
	)
	state := code
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch state {
		case code:
			switch {
			case c == '\'':
				state = single
				b.WriteByte(c)
			case c == '"':
				state = double
				b.WriteByte(c)
			case c == '-' && i+1 < len(query) && query[i+1] == '-':
				state = lineComment
				b.WriteByte(c)
			case c == '/' && i+1 < len(query) && query[i+1] == '*':
				state = blockComment
				b.WriteByte(c)
			case c == '?':
				n++
				fmt.Fprintf(&b, "$%d", n)
			default:
				b.WriteByte(c)
			}
		case single:
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					i++
					b.WriteByte('\'')
				} else {
					state = code
				}
			}
		case double:
			b.WriteByte(c)
			if c == '"' {
				state = code
			}
		case lineComment:
			b.WriteByte(c)
			if c == '\n' {
				state = code
			}
		case blockComment:
			b.WriteByte(c)
			if c == '*' && i+1 < len(query) && query[i+1] == '/' {
				i++
				b.WriteByte('/')
				state = code
			}
		}
	}
	return b.String()
}

// ExecContext runs q with args, rebinding placeholders for Postgres.
func (db *DB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return db.DB.ExecContext(ctx, Rebind(q, db.Dialect), args...)
}

// QueryContext runs q with args, rebinding placeholders for Postgres.
func (db *DB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return db.DB.QueryContext(ctx, Rebind(q, db.Dialect), args...)
}

// QueryRowContext runs q with args, rebinding placeholders for Postgres.
func (db *DB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return db.DB.QueryRowContext(ctx, Rebind(q, db.Dialect), args...)
}

// Exec runs q with args, rebinding placeholders for Postgres.
func (db *DB) Exec(q string, args ...any) (sql.Result, error) {
	return db.DB.Exec(Rebind(q, db.Dialect), args...)
}

// Query runs q with args, rebinding placeholders for Postgres.
func (db *DB) Query(q string, args ...any) (*sql.Rows, error) {
	return db.DB.Query(Rebind(q, db.Dialect), args...)
}

// QueryRow runs q with args, rebinding placeholders for Postgres.
func (db *DB) QueryRow(q string, args ...any) *sql.Row {
	return db.DB.QueryRow(Rebind(q, db.Dialect), args...)
}

// BeginTx starts a transaction whose query methods rebind like the DB's.
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := db.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, dialect: db.Dialect}, nil
}

// Tx wraps *sql.Tx with placeholder rebinding.
type Tx struct {
	*sql.Tx
	dialect Dialect
}

// ExecContext runs q with args, rebinding placeholders for Postgres.
func (tx *Tx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, Rebind(q, tx.dialect), args...)
}

// QueryContext runs q with args, rebinding placeholders for Postgres.
func (tx *Tx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return tx.Tx.QueryContext(ctx, Rebind(q, tx.dialect), args...)
}

// QueryRowContext runs q with args, rebinding placeholders for Postgres.
func (tx *Tx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return tx.Tx.QueryRowContext(ctx, Rebind(q, tx.dialect), args...)
}

// Exec runs q with args, rebinding placeholders for Postgres.
func (tx *Tx) Exec(q string, args ...any) (sql.Result, error) {
	return tx.Tx.Exec(Rebind(q, tx.dialect), args...)
}

// Query runs q with args, rebinding placeholders for Postgres.
func (tx *Tx) Query(q string, args ...any) (*sql.Rows, error) {
	return tx.Tx.Query(Rebind(q, tx.dialect), args...)
}

// QueryRow runs q with args, rebinding placeholders for Postgres.
func (tx *Tx) QueryRow(q string, args ...any) *sql.Row {
	return tx.Tx.QueryRow(Rebind(q, tx.dialect), args...)
}

// PrepareContext prepares q, rebinding placeholders for Postgres.
func (tx *Tx) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return tx.Tx.PrepareContext(ctx, Rebind(q, tx.dialect))
}

// OpenSQLite opens (creating if needed) the SQLite database at path,
// enables WAL mode and foreign keys, and applies pending migrations.
func OpenSQLite(path string) (*DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)"
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_pragma=foreign_keys(ON)"
	}
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite handles one writer at a time; a single connection avoids
	// SQLITE_BUSY churn and keeps the in-memory test database coherent.
	conn.SetMaxOpenConns(1)
	db := &DB{DB: conn, Dialect: SQLite}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if err := Migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Open is OpenSQLite, kept for existing callers (notably tests).
func Open(path string) (*DB, error) { return OpenSQLite(path) }

// OpenPostgres connects to Postgres (e.g.
// postgres://user:pass@host:5432/umago?sslmode=disable) and applies
// pending migrations.
func OpenPostgres(url string) (*DB, error) {
	conn, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(30 * time.Minute)
	db := &DB{DB: conn, Dialect: Postgres}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := Migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies every embedded migration for the DB's dialect not yet
// recorded in schema_migrations. Migration names are bare filenames, so
// existing SQLite databases keep working.
func Migrate(ctx context.Context, db *DB) error {
	dir := "sqlite"
	if db.Dialect == Postgres {
		dir = "postgres"
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations.FS, dir+"/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		short := strings.TrimPrefix(name, dir+"/")
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, short).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, short, time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// IsUniqueViolation reports whether err is a UNIQUE constraint failure,
// on either backend.
func IsUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "duplicate key value violates unique constraint"))
}
