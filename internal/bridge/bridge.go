// Package bridge runs the C reader (iec61850-read) as a separate process and decodes its JSON.
//
// The reader is the only part linked to libiec61850; the agent talks to it through command-line
// arguments and stdout only.
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Snapshot is the reader output.
type Snapshot struct {
	// Domains maps LD instance (IED prefix removed) -> "LN$FC" -> value tree.
	Domains map[string]map[string]json.RawMessage `json:"domains"`
	Meta    Meta                                  `json:"meta"`
}

// Meta describes the read.
type Meta struct {
	Host      string          `json:"host"`
	Port      int             `json:"port"`
	TakenAt   string          `json:"taken_at"`
	IED       string          `json:"ied"`
	FC        string          `json:"fc"`
	Nameplate json.RawMessage `json:"nameplate,omitempty"`
	Reads     int             `json:"reads"`
	Failures  int             `json:"failures"`
}

// Options for one reader run.
type Options struct {
	ReaderPath string
	Host       string
	Port       int
	TimeoutMs  int
	DelayMs    int
	FC         []string
	RunTimeout time.Duration
}

// ResolveReaderPath returns the configured path, else iec61850-read next to the agent binary, else $PATH.
func ResolveReaderPath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "iec61850-read")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return exec.LookPath("iec61850-read")
}

// Read runs the reader once against one device. Exit status 3 (some reads failed) still returns the
// snapshot, with Meta.Failures set; any other failure returns an error.
func Read(ctx context.Context, o Options) (*Snapshot, error) {
	args := []string{"-h", o.Host, "-p", strconv.Itoa(o.Port)}
	if o.TimeoutMs > 0 {
		args = append(args, "-t", strconv.Itoa(o.TimeoutMs))
	}
	if o.DelayMs >= 0 {
		args = append(args, "-d", strconv.Itoa(o.DelayMs))
	}
	if len(o.FC) > 0 {
		args = append(args, "-f", strings.Join(o.FC, ","))
	}
	if o.RunTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.RunTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, o.ReaderPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	partial := errors.As(err, &exitErr) && exitErr.ExitCode() == 3
	if err != nil && !partial {
		return nil, fmt.Errorf("running %s: %w: %s", o.ReaderPath, err, strings.TrimSpace(stderr.String()))
	}
	return Decode(stdout.Bytes())
}

// Decode parses reader output.
func Decode(data []byte) (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decoding reader output: %w", err)
	}
	if s.Domains == nil {
		return nil, fmt.Errorf("reader output has no domains")
	}
	return &s, nil
}
