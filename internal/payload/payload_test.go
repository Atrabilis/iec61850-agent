package payload

import (
	"encoding/json"
	"testing"

	"github.com/atrabilis/iec61850-agent/internal/bridge"
	"github.com/atrabilis/iec61850-agent/internal/config"
)

// Trimmed reader output from a NOJA RC (firmware 1.27).
const snapshot = `{"domains":{
 "LD0":{"MMXU1$MX":{"Hz":{"mag":{"f":50},"q":"0000000000000","t":"1970-03-16T00:31:29.887Z"}},
        "XCBR1$ST":{"Pos":{"stVal":"10","q":"0000000000000","t":"1970-02-26T04:40:23.467Z"}},
        "LLN0$DC":{"NamPlt":{"swRev":"1.27.0.0"}}},
 "UVLD":{"PTUV1$ST":{"Op":{"general":false}}}},
 "meta":{"host":"h","port":102,"taken_at":"2026-01-01T00:00:00Z","ied":"MyIED","fc":"ST,MX",
         "nameplate":{"swRev":"1.27.0.0","serNum":"123"},"reads":4,"failures":0}}`

func TestBuildSplitsByGroup(t *testing.T) {
	s, err := bridge.Decode([]byte(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	d := config.Device{Name: "noja", Groups: []config.Group{
		{Name: "mx", FC: []string{"MX"}},
		{Name: "st", FC: []string{"ST"}},
		{Name: "st_ld0", FC: []string{"ST"}, LogicalDevices: []string{"LD0"}},
		{Name: "breaker", FC: []string{"ST"}, LogicalNodes: []string{"XCBR1"}},
	}}
	rows := Build(d, s, "test")
	want := map[string]int{"mx": 1, "st": 2, "st_ld0": 1, "breaker": 1}
	for _, r := range rows {
		if r.Items != want[r.Group] {
			t.Errorf("group %s: %d items, want %d", r.Group, r.Items, want[r.Group])
		}
		if r.Payload["ied"] != "MyIED" || r.Payload["series_key"] != r.Group {
			t.Errorf("group %s: bad metadata %v", r.Group, r.Payload)
		}
		// DC must never leak into a group that did not ask for it.
		if containsKey(r.Payload, "LLN0$DC") {
			t.Errorf("group %s: unexpected DC item", r.Group)
		}
	}
}

func containsKey(p map[string]any, key string) bool {
	fields, _ := p["fields"].(map[string]map[string]json.RawMessage)
	for _, items := range fields {
		if _, ok := items[key]; ok {
			return true
		}
	}
	return false
}
