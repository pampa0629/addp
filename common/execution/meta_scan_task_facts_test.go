package execution

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMetaScanTaskFactsStrictWire(t *testing.T) {
	header := false
	facts := MetaScanTaskFacts{SchemaVersion: MetaScanTaskFactsSchemaV1, TaskID: "42", TaskDefinitionDigest: strings.Repeat("a", 64),
		EngineID: 9, ScopeKind: "engine", ScanDepth: "basic", ReadOptions: MetaScanReadOptions{HasHeader: &header}}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MetaScanTaskFacts
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.ReadOptions.HasHeader == nil || *decoded.ReadOptions.HasHeader {
		t.Fatalf("facts=%+v err=%v", decoded, err)
	}
	for name, input := range map[string]string{
		"wrong schema":        strings.Replace(string(raw), MetaScanTaskFactsSchemaV1, "meta.scan_plan_facts/v1", 1),
		"noncanonical task":   strings.Replace(string(raw), `"task_id":"42"`, `"task_id":"042"`, 1),
		"numeric engine":      strings.Replace(string(raw), `"engine_id":"9"`, `"engine_id":9`, 1),
		"noncanonical engine": strings.Replace(string(raw), `"engine_id":"9"`, `"engine_id":"09"`, 1),
		"unsupported scope":   strings.Replace(string(raw), `"scope_kind":"engine"`, `"scope_kind":"catalog_path"`, 1),
		"duplicate":           strings.Replace(string(raw), `"force":false`, `"force":false,"force":false`, 1),
		"null bool":           strings.Replace(string(raw), `"force":false`, `"force":null`, 1),
		"wrong bool":          strings.Replace(string(raw), `"force":false`, `"force":"false"`, 1),
		"missing bool":        strings.Replace(string(raw), `"force":false,`, ``, 1),
		"extra scope":         strings.Replace(string(raw), `"scope_kind":`, `"catalog_paths":["secret"],"scope_kind":`, 1),
		"extra credential":    strings.Replace(string(raw), `"read_options":{`, `"read_options":{"token":"secret",`, 1),
		"null header":         strings.Replace(string(raw), `"has_header":false`, `"has_header":null`, 1),
		"duplicate header":    strings.Replace(string(raw), `"has_header":false`, `"has_header":false,"has_header":true`, 1),
		"trailing":            string(raw) + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			original := decoded
			original.TaskID = "unchanged"
			if err := json.Unmarshal([]byte(input), &original); err == nil || original.TaskID != "unchanged" {
				t.Fatal("ambiguous facts accepted or receiver changed", err)
			}
		})
	}
}
