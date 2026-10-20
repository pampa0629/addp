package execution

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

func metaScanTestFacts() MetaScanPlanFacts {
	header := false
	return MetaScanPlanFacts{SchemaVersion: MetaScanPlanFactsSchemaV1, ExecutionID: "23dd4f40-95b5-4cc1-a294-ded9f78d10a2", ExecutionPlanDigest: strings.Repeat("a", 64), EngineID: 9, EngineVersion: "2", ScanDepth: "deep", SourceScope: &MetaScanSourceScope{Mode: "exact", Anchors: []plugin.EngineCatalogPath{plugin.TabularItemPath(9, "schema", "public", "people")}}, ReadOptions: MetaScanReadOptions{HasHeader: &header}, BasisState: authorizationfacts.AuthorizationBasisComplete}
}

func TestMetaScanPlanFactsStrictWire(t *testing.T) {
	facts := metaScanTestFacts()
	raw, err := json.Marshal(facts)
	if err != nil || facts.Validate() != nil {
		t.Fatal(err, facts.Validate())
	}
	var decoded MetaScanPlanFacts
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.ReadOptions.HasHeader == nil || *decoded.ReadOptions.HasHeader {
		t.Fatal("explicit false lost", err)
	}
	cases := map[string]string{
		"unknown":             strings.Replace(string(raw), `"read_options":{`, `"token":"secret","read_options":{`, 1),
		"duplicate nested":    strings.Replace(string(raw), `"has_header":false`, `"has_header":false,"has_header":false`, 1),
		"unknown nested":      strings.Replace(string(raw), `"has_header":false`, `"has_header":false,"actor":"secret"`, 1),
		"null optional":       strings.Replace(string(raw), `"has_header":false`, `"has_header":null`, 1),
		"null required":       strings.Replace(string(raw), `"read_options":{"has_header":false}`, `"read_options":null`, 1),
		"missing required":    strings.Replace(string(raw), `"task_id":null,`, ``, 1),
		"unknown path":        strings.Replace(string(raw), `"segments":[`, `"claim":true,"segments":[`, 1),
		"null name":           strings.Replace(string(raw), `"name":"people"`, `"name":null`, 1),
		"numeric engine":      strings.Replace(string(raw), `"engine_id":"9"`, `"engine_id":9`, 1),
		"noncanonical engine": strings.Replace(string(raw), `"engine_id":"9"`, `"engine_id":"09"`, 1),
		"bad UTF8":            string(append(raw, 0xff)),
		"multiple values":     string(raw) + `{}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			original := metaScanTestFacts()
			original.ExecutionID = "unchanged"
			if err := json.Unmarshal([]byte(input), &original); err == nil {
				t.Fatal("ambiguous facts accepted")
			}
			if original.ExecutionID != "unchanged" {
				t.Fatal("failed decode mutated receiver")
			}
		})
	}
	// encoding/json removes leading whitespace before calling UnmarshalJSON.
	// Exercise the DTO's raw-byte entry directly; HTTP separately caps the
	// complete wire response before invoking encoding/json.
	if err := decoded.UnmarshalJSON(append([]byte(strings.Repeat(" ", MaxExecutionConfigBytes)), raw...)); err == nil {
		t.Fatal("raw DTO budget was not enforced")
	}
}

func TestMetaScanPlanFactsIdentityAndBasis(t *testing.T) {
	mutations := map[string]func(*MetaScanPlanFacts){
		"UUID spelling":           func(f *MetaScanPlanFacts) { f.ExecutionID = strings.ToUpper(f.ExecutionID) },
		"nil UUID":                func(f *MetaScanPlanFacts) { f.ExecutionID = "00000000-0000-0000-0000-000000000000" },
		"version zero":            func(f *MetaScanPlanFacts) { f.EngineVersion = "0" },
		"version leading zero":    func(f *MetaScanPlanFacts) { f.EngineVersion = "02" },
		"version too large":       func(f *MetaScanPlanFacts) { f.EngineVersion = "9223372036854775808" },
		"unknown depth":           func(f *MetaScanPlanFacts) { f.ScanDepth = "auto" },
		"task missing digest":     func(f *MetaScanPlanFacts) { id := "42"; f.TaskID = &id },
		"ad-hoc with task digest": func(f *MetaScanPlanFacts) { d := f.ExecutionPlanDigest; f.TaskDefinitionDigest = &d },
		"complete without scope":  func(f *MetaScanPlanFacts) { f.SourceScope = nil },
		"unresolved with scope":   func(f *MetaScanPlanFacts) { f.BasisState = authorizationfacts.AuthorizationBasisIncomplete },
		"wrong engine":            func(f *MetaScanPlanFacts) { f.SourceScope.Anchors[0].EngineID = 8 },
		"exact directory":         func(f *MetaScanPlanFacts) { f.SourceScope.Anchors[0] = plugin.FileDirectoryPath(9, "data") },
		"exact root":              func(f *MetaScanPlanFacts) { f.SourceScope.Anchors[0] = plugin.ObjectRootPath(9) },
		"noncanonical root":       func(f *MetaScanPlanFacts) { f.SourceScope.Anchors[0].Segments[0].Name = "server" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f := metaScanTestFacts()
			mutate(&f)
			if f.Validate() == nil {
				t.Fatal("invalid identity or basis accepted")
			}
		})
	}
	for _, state := range []authorizationfacts.AuthorizationBasisState{authorizationfacts.AuthorizationBasisIncomplete, authorizationfacts.AuthorizationBasisNeedsRefresh, authorizationfacts.AuthorizationBasisUnsupported} {
		f := metaScanTestFacts()
		f.BasisState = state
		f.SourceScope = nil
		if f.Validate() != nil {
			t.Fatal("unresolved facts rejected", f.Validate())
		}
	}
	f := metaScanTestFacts()
	id, digest := "42", strings.Repeat("b", 64)
	f.TaskID, f.TaskDefinitionDigest = &id, &digest
	if f.Validate() != nil {
		t.Fatal(f.Validate())
	}
	copy := f.Clone()
	*copy.TaskID = "43"
	*copy.TaskDefinitionDigest = strings.Repeat("c", 64)
	*copy.ReadOptions.HasHeader = true
	copy.SourceScope.Anchors[0].Segments[2].Name = "other"
	if *f.TaskID != "42" || *f.TaskDefinitionDigest != digest || *f.ReadOptions.HasHeader || f.SourceScope.Anchors[0].Segments[2].Name != "people" {
		t.Fatal("clone aliases original")
	}
}

func TestMetaScanScopeCanonicalBoundedAnchors(t *testing.T) {
	f := metaScanTestFacts()
	f.SourceScope.Mode = "full"
	f.SourceScope.Anchors = []plugin.EngineCatalogPath{plugin.FileDirectoryPath(9, "future"), plugin.FileRootPath(9)}
	sort.Slice(f.SourceScope.Anchors, func(i, j int) bool {
		a, _ := json.Marshal(f.SourceScope.Anchors[i])
		b, _ := json.Marshal(f.SourceScope.Anchors[j])
		return string(a) < string(b)
	})
	if f.Validate() != nil {
		t.Fatal(f.Validate())
	}
	f.SourceScope.Anchors[0], f.SourceScope.Anchors[1] = f.SourceScope.Anchors[1], f.SourceScope.Anchors[0]
	if f.Validate() == nil {
		t.Fatal("unsorted anchors accepted")
	}
	f.SourceScope.Anchors = []plugin.EngineCatalogPath{plugin.FileRootPath(9), plugin.FileRootPath(9)}
	if f.Validate() == nil {
		t.Fatal("duplicate anchors accepted")
	}
	f.SourceScope.Anchors = make([]plugin.EngineCatalogPath, 201)
	if f.Validate() == nil {
		t.Fatal("unbounded anchors accepted")
	}
}

func TestMetaScanPlanDigestPreservesRawNumbersAndRejectsAmbiguity(t *testing.T) {
	for _, compute := range []func(json.RawMessage) (string, error){ComputeMetaScanExecutionPlanDigest, ComputeMetaScanTaskDefinitionDigest} {
		a, err := compute(json.RawMessage(`{"engine_id":9007199254740993,"options":{"header":false}}`))
		if err != nil {
			t.Fatal(err)
		}
		b, err := compute(json.RawMessage(`{ "options": {"header":false}, "engine_id":9007199254740993 }`))
		if err != nil || a != b {
			t.Fatal("object ordering changed digest", err)
		}
		c, _ := compute(json.RawMessage(`{"engine_id":9007199254740992,"options":{"header":false}}`))
		if a == c {
			t.Fatal("number precision lost")
		}
		for _, raw := range []string{`{"a":{"x":1,"x":2}}`, `null`, `[]`, `{} {}`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})} {
			if _, err := compute(json.RawMessage(raw)); err == nil {
				t.Fatal("invalid raw digest accepted")
			}
		}
	}
}
