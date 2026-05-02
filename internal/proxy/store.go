package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"godeploy-platform/internal/platform/iox"
)

// Store persists domain -> target (ip:port) routes in SQLite.
// Hot reload uses a version counter in a meta table, incremented
// whenever routes change.
type Store struct {
	db *sql.DB
}

// NewStore wraps db for route persistence. The caller must use a SQLite driver such as modernc.org/sqlite.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("db cannot be nil")
	}
	return &Store{db: db}, nil
}

// EnsureSchema creates proxy_routes and proxy_meta tables if they are missing.
func (s *Store) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS proxy_routes (
			domain TEXT PRIMARY KEY,
			target TEXT NOT NULL,
			updated_at_unix INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS proxy_meta (
			key TEXT PRIMARY KEY,
			value INTEGER NOT NULL
		);`,
		`INSERT INTO proxy_meta(key, value)
		 VALUES ('routes_version', 0)
		 ON CONFLICT(key) DO NOTHING;`,
	}

	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("failed to create proxy schema: %w", err)
		}
	}
	return nil
}

func normalizeDomain(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	// Strip :port if present.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	// net.SplitHostPort fails on "example.com" (no port), which is fine.
	// It also fails in some IPv6-without-brackets cases; we keep this simple.
	host = strings.TrimSuffix(host, ".")
	return strings.ToLower(host)
}

func validateTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("target cannot be empty")
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return fmt.Errorf("invalid target (expected ip:port): %w", err)
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("invalid target: empty host")
	}
	if p := strings.TrimSpace(port); p == "" || p == "0" {
		return errors.New("invalid target: empty/zero port")
	}
	return nil
}

// UpsertRoute creates or updates the route and bumps the version in one transaction.
func (s *Store) UpsertRoute(ctx context.Context, domain, target string) error {
	domain = normalizeDomain(domain)
	if domain == "" {
		return errors.New("domain cannot be empty")
	}
	if err := validateTarget(target); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback() //nolint:errcheck // noop after Commit; sql.ErrTxDone otherwise
	}()

	now := time.Now().UTC().Unix()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO proxy_routes(domain, target, updated_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET
			target = excluded.target,
			updated_at_unix = excluded.updated_at_unix
	`, domain, strings.TrimSpace(target), now)
	if err != nil {
		return fmt.Errorf("failed to upsert route: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE proxy_meta SET value = value + 1 WHERE key = 'routes_version'`); err != nil {
		return fmt.Errorf("failed to increment routes_version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// DeleteRoute removes a domain mapping and bumps the routes version counter.
func (s *Store) DeleteRoute(ctx context.Context, domain string) error {
	domain = normalizeDomain(domain)
	if domain == "" {
		return errors.New("domain cannot be empty")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback() //nolint:errcheck // noop after Commit; sql.ErrTxDone otherwise
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM proxy_routes WHERE domain = ?`, domain); err != nil {
		return fmt.Errorf("failed to delete route: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE proxy_meta SET value = value + 1 WHERE key = 'routes_version'`); err != nil {
		return fmt.Errorf("failed to increment routes_version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// RoutesVersion returns the monotonic counter used for hot-reload change detection.
func (s *Store) RoutesVersion(ctx context.Context) (int64, error) {
	var v int64
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM proxy_meta WHERE key = 'routes_version'`).Scan(&v); err != nil {
		return 0, fmt.Errorf("failed to read routes_version: %w", err)
	}
	return v, nil
}

// LoadAll returns every route plus the current routes version.
func (s *Store) LoadAll(ctx context.Context) (routes map[string]string, version int64, err error) {
	version, err = s.RoutesVersion(ctx)
	if err != nil {
		return nil, 0, err
	}

	rows, qerr := s.db.QueryContext(ctx, `SELECT domain, target FROM proxy_routes`) //nolint:sqlclosecheck // rows closed in defer below
	if qerr != nil {
		return nil, 0, fmt.Errorf("failed to list routes: %w", qerr)
	}
	defer iox.Close(rows)

	routes = make(map[string]string)
	for rows.Next() {
		var domain, target string
		if scanErr := rows.Scan(&domain, &target); scanErr != nil {
			return nil, 0, fmt.Errorf("failed to scan route: %w", scanErr)
		}
		domain = normalizeDomain(domain)
		if domain == "" {
			continue
		}
		routes[domain] = strings.TrimSpace(target)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("failed to iterate routes: %w", err)
	}

	return routes, version, nil
}

// GetRoute returns the current target for a domain. If missing, ok is false.
func (s *Store) GetRoute(ctx context.Context, domain string) (target string, ok bool, err error) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return "", false, errors.New("domain cannot be empty")
	}

	var t string
	row := s.db.QueryRowContext(ctx, `SELECT target FROM proxy_routes WHERE domain = ?`, domain)
	if scanErr := row.Scan(&t); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read route %q: %w", domain, scanErr)
	}
	return strings.TrimSpace(t), true, nil
}
