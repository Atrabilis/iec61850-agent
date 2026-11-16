// Command iec61850-agent reads IEC 61850 devices (through the iec61850-read reader) and writes one JSONB
// row per configured group to PostgreSQL shadow tables. One run per invocation, like the other
// polling collectors (a systemd timer or cron schedules it).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"

	dotenv "github.com/joho/godotenv"

	"github.com/atrabilis/iec61850-agent/internal/bridge"
	"github.com/atrabilis/iec61850-agent/internal/config"
	"github.com/atrabilis/iec61850-agent/internal/payload"
	"github.com/atrabilis/iec61850-agent/storage/postgres_shadow"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	configPath := flag.String("configPath", "", "Path to the YAML config file")
	envPath := flag.String("envPath", "/etc/iec61850-agent/iec61850-agent.env", "Path to the dotenv file with the output credentials")
	interrogateOnly := flag.Bool("interrogateOnly", false, "Read the devices and print the rows as JSON, without writing to the outputs")
	checkConfig := flag.Bool("checkConfig", false, "Validate the config file and exit")
	showVersion := flag.Bool("version", false, "Print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *configPath == "" {
		log.Fatal("-configPath is required")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if *checkConfig {
		fmt.Printf("config OK: %d device(s)\n", len(cfg.Devices))
		return
	}
	if err := dotenv.Load(*envPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Fatalf("env file %s: %v", *envPath, err)
	}
	os.Exit(run(cfg, *interrogateOnly))
}

// run reads every device and writes its rows; it returns 1 when any device or output failed, so the
// systemd unit shows the failure.
func run(cfg *config.File, interrogateOnly bool) int {
	ctx := context.Background()
	resolution, _ := cfg.Resolution()
	runTimeout, _ := cfg.RunTimeout()
	ts := time.Now().Truncate(resolution).UTC()
	if resolution == 0 {
		ts = time.Now().UTC()
	}

	readerPath, err := bridge.ResolveReaderPath(cfg.Reader.Path)
	if err != nil {
		log.Printf("reader iec61850-read not found: %v", err)
		return 1
	}

	var writers []*postgres_shadow.Writer
	if !interrogateOnly {
		for _, o := range cfg.Storage.Outputs {
			if !o.IsEnabled() {
				continue
			}
			_, pc, _ := o.Resolve()
			w, err := postgres_shadow.New(ctx, o.Name, pc)
			if err != nil {
				log.Printf("output %q unavailable: %v", o.Name, err)
				continue
			}
			defer w.Close()
			writers = append(writers, w)
		}
		if len(writers) == 0 {
			log.Printf("no output available; nothing will be written")
			return 1
		}
	}

	failed := false
	for _, entry := range cfg.Devices {
		d := entry.Device
		delay := cfg.Reader.DelayMs
		if delay == 0 {
			delay = 20
		}
		snap, err := bridge.Read(ctx, bridge.Options{
			ReaderPath: readerPath, Host: d.Host, Port: d.Port,
			TimeoutMs: cfg.Reader.TimeoutMs, DelayMs: delay,
			FC: d.FCUnion(), RunTimeout: runTimeout,
		})
		if err != nil {
			log.Printf("device %q (%s:%d): %v", d.Name, d.Host, d.Port, err)
			failed = true
			continue
		}
		if snap.Meta.Failures > 0 {
			log.Printf("device %q: %d of %d reads failed; writing what was read", d.Name, snap.Meta.Failures, snap.Meta.Reads)
			failed = true
		}
		for _, row := range payload.Build(d, snap, version) {
			if interrogateOnly {
				out, _ := json.Marshal(map[string]any{
					"plant": cfg.Plant, "ts": ts, "device_name": d.Name, "series_key": row.Group, "payload": row.Payload,
				})
				fmt.Println(string(out))
				continue
			}
			if row.Items == 0 {
				log.Printf("device %q group %q: no items matched; row not written", d.Name, row.Group)
				continue
			}
			for _, w := range writers {
				if err := w.Write(ctx, cfg.Plant, ts, d.Name, row.Group, row.Payload); err != nil {
					log.Printf("device %q group %q: %v", d.Name, row.Group, err)
					failed = true
				}
			}
		}
		log.Printf("device %q: %d reads, IED %q", d.Name, snap.Meta.Reads, snap.Meta.IED)
	}
	if failed {
		return 1
	}
	return 0
}
