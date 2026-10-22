package service

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/jsonmap"
)

func TestScanExecutionMatchesTaskSelectors(t *testing.T) {
	for _, tc := range []struct {
		name, scope, params, config, trigger string
		want                                 bool
	}{
		{"default depth", `{}`, `{}`, `{"engine_id":9,"scan_depth":"deep","force":false}`, "manual", true},
		{"null depth not default", `{}`, `{"scan_depth":null}`, `{"engine_id":9,"scan_depth":"deep"}`, "manual", false},
		{"wrong bool not default", `{}`, `{"force":"false"}`, `{"engine_id":9,"scan_depth":"deep","force":false}`, "manual", false},
		{"read options retained", `{}`, `{"encoding":"GBK","has_header":false}`, `{"engine_id":9,"scan_depth":"deep","encoding":"GBK","has_header":false}`, "manual", true},
		{"read option dropped", `{}`, `{"encoding":"GBK"}`, `{"engine_id":9,"scan_depth":"deep"}`, "manual", false},
		{"same paths reordered", `{"type":"catalog_path","catalog_paths":["schema/a","schema/b"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":["schema/b","schema/a"]}`, "manual", true},
		{"scheduled subtraction", `{"type":"catalog_path","catalog_paths":["schema/a","schema/b"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":["schema/b"]}`, "scheduled", true},
		{"manual cannot change plan", `{"type":"catalog_path","catalog_paths":["schema/a","schema/b"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":["schema/b"]}`, "manual", false},
		{"cannot omit paths to scan whole", `{"type":"catalog_path","catalog_paths":["schema/a"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep"}`, "scheduled", false},
		{"cannot empty paths to scan whole", `{"type":"catalog_path","catalog_paths":["schema/a"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":[]}`, "scheduled", false},
		{"cannot add path", `{"type":"catalog_path","catalog_paths":["schema/a"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":["schema/a","schema/b"]}`, "scheduled", false},
		{"prefix not membership", `{"type":"catalog_path","catalog_paths":["schema/a"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":["schema/ab"]}`, "scheduled", false},
		{"no parent inference", `{"type":"catalog_path","catalog_paths":["schema/a"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":["schema"]}`, "scheduled", false},
		{"malformed paths", `{"type":"catalog_path","catalog_paths":["schema/a"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":[9]}`, "scheduled", false},
		{"duplicate paths", `{"type":"catalog_path","catalog_paths":["schema/a"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":["schema/a","schema/a"]}`, "scheduled", false},
		{"unknown scope", `{"type":"unknown"}`, `{}`, `{"engine_id":9,"scan_depth":"deep"}`, "manual", false},
		{"mixed engine scope", `{"type":"engine","catalog_paths":["schema/a"]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","catalog_paths":["schema/a"]}`, "manual", false},
		{"same ref group", `{"type":"ref_group","ref_groups":[{"refs":["a.ifc"]}]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","ref_groups":[{"refs":["a.ifc"]}]}`, "manual", true},
		{"ref group expanded", `{"type":"ref_group","ref_groups":[{"refs":["a.ifc"]}]}`, `{}`, `{"engine_id":9,"scan_depth":"deep","ref_groups":[{"refs":["a.ifc","b.ifc"]}]}`, "manual", false},
		{"ref group missing", `{"type":"ref_group","ref_groups":[{"refs":["a.ifc"]}]}`, `{}`, `{"engine_id":9,"scan_depth":"deep"}`, "scheduled", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := json.RawMessage(`{"engine_id":9,"scope":` + tc.scope + `,"parameters":` + tc.params + `}`)
			config, err := jsonmap.DecodeObjectStrict([]byte(tc.config), 8192, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := scanExecutionMatchesTask(definition, config, tc.trigger); got != tc.want {
				t.Fatalf("matches=%t want=%t", got, tc.want)
			}
		})
	}
}
