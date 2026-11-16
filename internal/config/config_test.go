package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validYAML = `
plant: p1
devices:
  - device:
      name: noja
      host: 192.0.2.10
      groups:
        - name: mx
          fc: [mx]
        - name: st
          fc: [ST]
storage:
  outputs:
    - name: local
      type: postgres_shadow
      postgres_shadow:
        host_env: H
        port_env: P
        user_env: U
        password_env: W
        database_env: D
        schema: edge
        table: t
`

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaultsAndNormalization(t *testing.T) {
	f, err := Load(write(t, validYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	d := f.Devices[0].Device
	if d.Port != DefaultPort {
		t.Errorf("port = %d, want %d", d.Port, DefaultPort)
	}
	if got := d.Groups[0].FC[0]; got != "MX" {
		t.Errorf("fc not upper-cased: %q", got)
	}
	if got := strings.Join(d.FCUnion(), ","); got != "MX,ST" {
		t.Errorf("FCUnion = %q", got)
	}
	if r, _ := f.Resolution(); r != time.Minute {
		t.Errorf("resolution = %v, want 1m", r)
	}
}

func TestRejects(t *testing.T) {
	cases := map[string]struct{ from, to, want string }{
		"controls":        {"fc: [ST]", "fc: [CO]", "CO"},
		"bad fc":          {"fc: [ST]", "fc: [STATUS]", "two-letter"},
		"no plant":        {"plant: p1", "plant: ''", "plant"},
		"repeated group":  {"name: st", "name: mx", "repeated"},
		"unknown output":  {"type: postgres_shadow", "type: influx", "unsupported"},
		"missing env":     {"host_env: H", "host_env: ''", "host_env"},
		"bad resolution":  {"plant: p1", "plant: p1\ntimestamp_resolution: -1m", "negative"},
		"bad run timeout": {"plant: p1", "plant: p1\nreader:\n  run_timeout: soon", "run_timeout"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, strings.Replace(validYAML, c.from, c.to, 1)))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestExampleConfigIsValid(t *testing.T) {
	if _, err := Load(filepath.Join("..", "..", "configs", "example.yml")); err != nil {
		t.Fatalf("configs/example.yml: %v", err)
	}
}
