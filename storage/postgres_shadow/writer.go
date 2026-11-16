// Package postgres_shadow writes one JSONB row per device group and run (table layout in
// sql/postgres_shadow.sql).
package postgres_shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atrabilis/iec61850-agent/storage"
)

var ident = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Writer is one postgres_shadow output.
type Writer struct {
	name string
	fqn  string
	pool *pgxpool.Pool
}

// New connects using the environment variables named in cfg. It does not create tables.
func New(ctx context.Context, name string, cfg storage.PostgresConfig) (*Writer, error) {
	env := func(key string) (string, error) {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			return "", fmt.Errorf("output %q: environment variable %s is empty", name, key)
		}
		return v, nil
	}
	host, err := env(cfg.HostEnv)
	if err != nil {
		return nil, err
	}
	port, err := env(cfg.PortEnv)
	if err != nil {
		return nil, err
	}
	user, err := env(cfg.UserEnv)
	if err != nil {
		return nil, err
	}
	database, err := env(cfg.DatabaseEnv)
	if err != nil {
		return nil, err
	}
	password := os.Getenv(cfg.PasswordEnv)
	if password == "" {
		return nil, fmt.Errorf("output %q: environment variable %s is empty", name, cfg.PasswordEnv)
	}
	schema, table := strings.ToLower(cfg.Schema), strings.ToLower(cfg.Table)
	if !ident.MatchString(schema) || !ident.MatchString(table) {
		return nil, fmt.Errorf("output %q: schema and table must be plain lower-case identifiers", name)
	}

	conn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		url.QueryEscape(user), url.QueryEscape(password), host, port, url.QueryEscape(database))
	pool, err := pgxpool.New(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("output %q: %w", name, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("output %q: database not reachable: %w", name, err)
	}
	return &Writer{name: name, fqn: schema + "." + table, pool: pool}, nil
}

// Name of the output.
func (w *Writer) Name() string { return w.name }

// Write upserts one row: a later run in the same timestamp step overwrites the row.
func (w *Writer) Write(ctx context.Context, plant string, ts time.Time, device, group string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("output %q: encoding payload: %w", w.name, err)
	}
	query := fmt.Sprintf(`INSERT INTO %s (plant, ts, device_name, slave_name, series_key, payload)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (plant, device_name, slave_name, ts, series_key)
DO UPDATE SET payload = EXCLUDED.payload, ingested_at = now()`, w.fqn)
	// slave_name carries the group; series_key repeats it, so the table can share its layout and
	// primary key with shadow tables written by other collectors.
	_, err = w.pool.Exec(ctx, query, plant, ts.UTC(), device, group, group, json.RawMessage(data))
	if err != nil {
		return fmt.Errorf("output %q: %w", w.name, err)
	}
	return nil
}

// Close releases the pool.
func (w *Writer) Close() { w.pool.Close() }
