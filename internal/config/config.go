// Package config loads and validates the agent YAML (one file per device type).
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/atrabilis/iec61850-agent/storage"
)

// DefaultPort is the IEC 61850 MMS port.
const DefaultPort = 102

// File is the whole configuration file.
type File struct {
	Plant               string         `yaml:"plant"`
	TimestampResolution string         `yaml:"timestamp_resolution,omitempty"`
	Reader              Reader         `yaml:"reader,omitempty"`
	Devices             []DeviceEntry  `yaml:"devices"`
	Storage             storage.Config `yaml:"storage"`
}

// Reader controls how the C reader (iec61850-read) is run.
type Reader struct {
	// Path to the iec61850-read binary; empty means next to the agent binary, then $PATH.
	Path string `yaml:"path,omitempty"`
	// Per-request MMS timeout in ms (default 10000).
	TimeoutMs int `yaml:"timeout_ms,omitempty"`
	// Pause between reads in ms, to go easy on protection devices (default 20).
	DelayMs int `yaml:"delay_ms,omitempty"`
	// Whole-run limit for one device, as a Go duration (default 2m).
	RunTimeout string `yaml:"run_timeout,omitempty"`
}

// DeviceEntry is one item of `devices:`, written as `- device: {...}`.
type DeviceEntry struct {
	Device Device `yaml:"device"`
}

// Device is one IEC 61850 server (IED).
type Device struct {
	Name   string  `yaml:"name"`
	Host   string  `yaml:"host"`
	Port   int     `yaml:"port,omitempty"`
	Groups []Group `yaml:"groups"`
}

// Group selects part of the device model; each group becomes one shadow row per run (series_key = name).
type Group struct {
	Name string `yaml:"name"`
	// Functional constraints to keep, e.g. [MX] or [ST]. CO (controls) is rejected.
	FC []string `yaml:"fc"`
	// Optional filters on logical device instances (e.g. LD0) and logical nodes (e.g. MMXU1). Empty = all.
	LogicalDevices []string `yaml:"logical_devices,omitempty"`
	LogicalNodes   []string `yaml:"logical_nodes,omitempty"`
}

// Load reads and validates a configuration file.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Validate checks the configuration and fills defaults.
func (f *File) Validate() error {
	if strings.TrimSpace(f.Plant) == "" {
		return fmt.Errorf("plant is empty")
	}
	if _, err := f.Resolution(); err != nil {
		return err
	}
	if _, err := f.RunTimeout(); err != nil {
		return err
	}
	if f.Reader.TimeoutMs < 0 || f.Reader.DelayMs < 0 {
		return fmt.Errorf("reader.timeout_ms and reader.delay_ms must not be negative")
	}
	if len(f.Devices) == 0 {
		return fmt.Errorf("devices is empty")
	}
	names := map[string]bool{}
	for i := range f.Devices {
		d := &f.Devices[i].Device
		if d.Name == "" {
			return fmt.Errorf("devices[%d].device.name is empty", i)
		}
		if names[d.Name] {
			return fmt.Errorf("devices[%d].device.name %q is repeated", i, d.Name)
		}
		names[d.Name] = true
		if d.Host == "" {
			return fmt.Errorf("device %q: host is empty", d.Name)
		}
		if d.Port == 0 {
			d.Port = DefaultPort
		}
		if d.Port < 0 || d.Port > 65535 {
			return fmt.Errorf("device %q: port %d out of range", d.Name, d.Port)
		}
		if len(d.Groups) == 0 {
			return fmt.Errorf("device %q: groups is empty", d.Name)
		}
		groups := map[string]bool{}
		for j := range d.Groups {
			g := &d.Groups[j]
			if g.Name == "" {
				return fmt.Errorf("device %q: groups[%d].name is empty", d.Name, j)
			}
			if groups[g.Name] {
				return fmt.Errorf("device %q: group %q is repeated", d.Name, g.Name)
			}
			groups[g.Name] = true
			if len(g.FC) == 0 {
				return fmt.Errorf("device %q group %q: fc is empty", d.Name, g.Name)
			}
			for k, fc := range g.FC {
				fc = strings.ToUpper(strings.TrimSpace(fc))
				if len(fc) != 2 {
					return fmt.Errorf("device %q group %q: fc %q is not a two-letter functional constraint", d.Name, g.Name, g.FC[k])
				}
				if fc == "CO" {
					return fmt.Errorf("device %q group %q: fc CO (controls) is not allowed; the agent only reads", d.Name, g.Name)
				}
				g.FC[k] = fc
			}
		}
	}
	return storage.Validate(f.Storage)
}

// Resolution is the step the run timestamp is rounded down to (default 1m, "0s" = no rounding).
func (f *File) Resolution() (time.Duration, error) {
	raw := strings.TrimSpace(f.TimestampResolution)
	if raw == "" {
		return time.Minute, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("timestamp_resolution %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("timestamp_resolution %q: must not be negative", raw)
	}
	return d, nil
}

// RunTimeout limits one reader run (default 2m).
func (f *File) RunTimeout() (time.Duration, error) {
	raw := strings.TrimSpace(f.Reader.RunTimeout)
	if raw == "" {
		return 2 * time.Minute, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("reader.run_timeout %q: must be a positive duration", raw)
	}
	return d, nil
}

// FCUnion returns the functional constraints any group of the device needs, in a stable order.
func (d Device) FCUnion() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range d.Groups {
		for _, fc := range g.FC {
			if !seen[fc] {
				seen[fc] = true
				out = append(out, fc)
			}
		}
	}
	return out
}
