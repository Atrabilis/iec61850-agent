// Package storage describes where the agent sends data. Connection values come from environment variables
// named in the YAML, never from the YAML itself.
package storage

import (
	"fmt"
	"sort"
	"strings"
)

// KindPostgresShadow writes one JSONB payload per row (plain PostgreSQL; no TimescaleDB needed).
const KindPostgresShadow = "postgres_shadow"

// Config is the `storage:` block.
type Config struct {
	Outputs []OutputConfig `yaml:"outputs"`
}

// OutputConfig is one destination.
type OutputConfig struct {
	Name           string         `yaml:"name"`
	Type           string         `yaml:"type"`
	Enabled        *bool          `yaml:"enabled,omitempty"`
	PostgresShadow PostgresConfig `yaml:"postgres_shadow,omitempty"`
}

// PostgresConfig is the connection (by environment variable name) and target table.
type PostgresConfig struct {
	HostEnv     string `yaml:"host_env"`
	PortEnv     string `yaml:"port_env"`
	UserEnv     string `yaml:"user_env"`
	PasswordEnv string `yaml:"password_env"`
	DatabaseEnv string `yaml:"database_env"`
	Schema      string `yaml:"schema"`
	Table       string `yaml:"table"`
}

// IsEnabled defaults to true when `enabled` is omitted.
func (o OutputConfig) IsEnabled() bool { return o.Enabled == nil || *o.Enabled }

// Resolve returns the output kind and its settings.
func (o OutputConfig) Resolve() (string, PostgresConfig, error) {
	kind := strings.ToLower(strings.TrimSpace(o.Type))
	if kind != KindPostgresShadow {
		return "", PostgresConfig{}, fmt.Errorf("storage output %q: unsupported type %q (supported: %s)", o.Name, o.Type, KindPostgresShadow)
	}
	return kind, o.PostgresShadow, nil
}

// Validate checks every output; unknown types are rejected at load time so -checkConfig catches them.
func Validate(c Config) error {
	if len(c.Outputs) == 0 {
		return fmt.Errorf("storage.outputs is empty")
	}
	for i, o := range c.Outputs {
		if o.Name == "" {
			return fmt.Errorf("storage.outputs[%d].name is empty", i)
		}
		_, cfg, err := o.Resolve()
		if err != nil {
			return fmt.Errorf("storage.outputs[%d]: %w", i, err)
		}
		missing := []string{}
		for field, v := range map[string]string{
			"host_env": cfg.HostEnv, "port_env": cfg.PortEnv, "user_env": cfg.UserEnv,
			"password_env": cfg.PasswordEnv, "database_env": cfg.DatabaseEnv, "schema": cfg.Schema, "table": cfg.Table,
		} {
			if strings.TrimSpace(v) == "" {
				missing = append(missing, field)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			return fmt.Errorf("storage output %q: empty %s", o.Name, strings.Join(missing, ", "))
		}
	}
	return nil
}
