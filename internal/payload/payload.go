// Package payload splits one reader snapshot into the rows of each configured group.
package payload

import (
	"encoding/json"
	"strings"

	"github.com/atrabilis/iec61850-agent/internal/bridge"
	"github.com/atrabilis/iec61850-agent/internal/config"
)

// Row is one shadow row: one group of one device at one timestamp.
type Row struct {
	Group   string
	Payload map[string]any
	Items   int
}

// Build returns one row per group. The payload keeps the reader layout under "fields"
// (LD instance -> "LN$FC" -> value tree, with q and t from the device), plus what is needed to
// interpret it later: IED name, nameplate (firmware, serial, model) and the read stats.
func Build(d config.Device, s *bridge.Snapshot, agentVersion string) []Row {
	rows := make([]Row, 0, len(d.Groups))
	for _, g := range d.Groups {
		fields := map[string]map[string]json.RawMessage{}
		items := 0
		for ld, lnItems := range s.Domains {
			if !match(g.LogicalDevices, ld) {
				continue
			}
			for key, val := range lnItems {
				ln, fc, ok := strings.Cut(key, "$")
				if !ok || !match(g.FC, fc) || !match(g.LogicalNodes, ln) {
					continue
				}
				if fields[ld] == nil {
					fields[ld] = map[string]json.RawMessage{}
				}
				fields[ld][key] = val
				items++
			}
		}
		p := map[string]any{
			"series_key": g.Name,
			"fc":         g.FC,
			"ied":        s.Meta.IED,
			"reader": map[string]any{
				"taken_at": s.Meta.TakenAt,
				"reads":    s.Meta.Reads,
				"failures": s.Meta.Failures,
			},
			"agent_version": agentVersion,
			"fields":        fields,
		}
		if len(s.Meta.Nameplate) > 0 {
			p["nameplate"] = s.Meta.Nameplate
		}
		rows = append(rows, Row{Group: g.Name, Payload: p, Items: items})
	}
	return rows
}

// match is true when the filter is empty or contains v.
func match(filter []string, v string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if f == v {
			return true
		}
	}
	return false
}
