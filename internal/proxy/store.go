package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Store persiste rotas domain -> target (ip:port) em SQLite.
// O hot-reload usa um "version counter" em uma tabela meta, incrementado
// sempre que as rotas são alteradas.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("db nao pode ser nil")
	}
	return &Store{db: db}, nil
}

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
			return fmt.Errorf("falha ao criar schema do proxy: %w", err)
		}
	}
	return nil
}

func normalizeDomain(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	// Remove :port se existir.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else {
		// net.SplitHostPort falha em "example.com" (sem porta), o que é ok.
		// Também falha em alguns casos com IPv6 sem colchetes; mantemos simples.
	}
	host = strings.TrimSuffix(host, ".")
	return strings.ToLower(host)
}

func validateTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("target nao pode ser vazio")
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return fmt.Errorf("target invalido (esperado ip:porta): %w", err)
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("target invalido: host vazio")
	}
	if p := strings.TrimSpace(port); p == "" || p == "0" {
		return errors.New("target invalido: porta vazia/zero")
	}
	return nil
}

// UpsertRoute cria/atualiza a rota e incrementa a versão em uma mesma transação.
func (s *Store) UpsertRoute(ctx context.Context, domain, target string) error {
	domain = normalizeDomain(domain)
	if domain == "" {
		return errors.New("domain nao pode ser vazio")
	}
	if err := validateTarget(target); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("falha ao iniciar transacao: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Unix()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO proxy_routes(domain, target, updated_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET
			target = excluded.target,
			updated_at_unix = excluded.updated_at_unix
	`, domain, strings.TrimSpace(target), now)
	if err != nil {
		return fmt.Errorf("falha ao upsert de rota: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE proxy_meta SET value = value + 1 WHERE key = 'routes_version'`); err != nil {
		return fmt.Errorf("falha ao incrementar routes_version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar transacao: %w", err)
	}
	return nil
}

func (s *Store) DeleteRoute(ctx context.Context, domain string) error {
	domain = normalizeDomain(domain)
	if domain == "" {
		return errors.New("domain nao pode ser vazio")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("falha ao iniciar transacao: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM proxy_routes WHERE domain = ?`, domain); err != nil {
		return fmt.Errorf("falha ao deletar rota: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE proxy_meta SET value = value + 1 WHERE key = 'routes_version'`); err != nil {
		return fmt.Errorf("falha ao incrementar routes_version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("falha ao commitar transacao: %w", err)
	}
	return nil
}

func (s *Store) RoutesVersion(ctx context.Context) (int64, error) {
	var v int64
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM proxy_meta WHERE key = 'routes_version'`).Scan(&v); err != nil {
		return 0, fmt.Errorf("falha ao ler routes_version: %w", err)
	}
	return v, nil
}

func (s *Store) LoadAll(ctx context.Context) (map[string]string, int64, error) {
	v, err := s.RoutesVersion(ctx)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT domain, target FROM proxy_routes`)
	if err != nil {
		return nil, 0, fmt.Errorf("falha ao listar rotas: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var domain, target string
		if err := rows.Scan(&domain, &target); err != nil {
			return nil, 0, fmt.Errorf("falha ao scan de rota: %w", err)
		}
		domain = normalizeDomain(domain)
		if domain == "" {
			continue
		}
		out[domain] = strings.TrimSpace(target)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("falha ao iterar rotas: %w", err)
	}

	return out, v, nil
}

// GetRoute retorna o target atual para um domain. Se não existir, ok=false.
func (s *Store) GetRoute(ctx context.Context, domain string) (target string, ok bool, err error) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return "", false, errors.New("domain nao pode ser vazio")
	}

	var t string
	row := s.db.QueryRowContext(ctx, `SELECT target FROM proxy_routes WHERE domain = ?`, domain)
	if scanErr := row.Scan(&t); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("falha ao ler rota %q: %w", domain, scanErr)
	}
	return strings.TrimSpace(t), true, nil
}

