package conformance

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
)

// CompilerSourceDiagnostics checks the same request-level support contract for
// each certified tabular compiler, without opening a database connection.
func CompilerSourceDiagnostics(t *testing.T, c plugin.AnalyticalCompiler, model plugin.EngineCatalogModelSpec, nativeInt string) {
	t.Helper()
	request := func() plugin.CompileRequest {
		field := datatype.FieldInfo{Name: "id", Type: datatype.FieldTypeInt}
		return plugin.CompileRequest{
			Plan: plan.Plan{
				SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile,
				Nodes: []plan.Node{{ID: "scan", Op: "scan", Scan: &plan.Scan{Source: "items", Fields: []datatype.FieldInfo{field}}}},
				Root:  "scan", Output: plan.OutputContract{Fields: []datatype.FieldInfo{field}, StableKey: []string{"id"}},
			},
			Sources: []plugin.SourceBinding{{
				Source:  "items",
				Path:    plugin.EngineCatalogBranchLeafPath(model, 42, model.Levels[0].Term, "business", model.Levels[1].Term, plugin.EngineCatalogKindTable, "items"),
				Columns: []plugin.ColumnBinding{{Column: "id", Field: datatype.FieldInfo{Name: "id", Path: []string{"id"}, Type: datatype.FieldTypeInt, NativeType: nativeInt}}},
			}},
			Instance: plugin.AnalyticalInstance{EngineID: 42, Capability: plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}}},
		}
	}
	valid := request()
	if report, err := c.Check(valid); err != nil || !report.Supported || len(report.Diagnostics) != 0 {
		t.Fatalf("valid source support=%#v err=%v", report, err)
	}
	for _, tc := range []struct {
		name, code, column string
		change             func(*plugin.CompileRequest)
	}{
		{"catalog path", "unsupported_source_path", "", func(r *plugin.CompileRequest) { r.Sources[0].Path.Segments[2].Kind = "view" }},
		{"column path", "unsupported_source_column", "id", func(r *plugin.CompileRequest) { r.Sources[0].Columns[0].Field.Path = []string{"nested", "id"} }},
		{"native type", "unsupported_source_field_type", "id", func(r *plugin.CompileRequest) { r.Sources[0].Columns[0].Field.NativeType = "unproven_native_type" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request()
			tc.change(&r)
			want := plugin.SupportDiagnostic{Code: tc.code, NodeID: "scan", Operation: "scan", SourceID: "items", ColumnID: tc.column}
			report, err := c.Check(r)
			if err != nil || report.Supported || !reflect.DeepEqual(report.Diagnostics, []plugin.SupportDiagnostic{want}) {
				t.Fatalf("support=%#v err=%v, want %#v", report, err, want)
			}
			if _, err := c.Compile(r); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
				t.Fatalf("compile accepted unsupported source: %v", err)
			}
		})
	}
}

type ScanTypeCase struct {
	Native           string
	Type             datatype.FieldType
	Precision, Scale int
	Supported        bool
}

func ScanContracts(t *testing.T, d sqlcompile.ScanDialect, model plugin.EngineCatalogModelSpec, quote func(string) string, cases []ScanTypeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.Native+"/"+string(tc.Type), func(t *testing.T) {
			c := plugin.ColumnBinding{Column: "logical", Field: datatype.FieldInfo{Name: "native'\"`", Path: []string{"native'\"`"}, Type: tc.Type, NativeType: tc.Native, Precision: tc.Precision, Scale: tc.Scale}}
			expr, err := d.Column(c, "scan_source")
			if tc.Supported {
				if err != nil || expr.Type != tc.Type || !strings.Contains(expr.SQL, quote("scan_source")+"."+quote(c.Field.Name)) {
					t.Fatalf("invalid source expression: %#v %v", expr, err)
				}
			} else if !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
				t.Fatalf("unproven native type accepted: %#v %v", expr, err)
			}
		})
	}
	newSource := func() plugin.SourceBinding {
		return plugin.SourceBinding{Source: "source", Path: plugin.EngineCatalogBranchLeafPath(model, 7, model.Levels[0].Term, "business", plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, "table'\"`;")}
	}
	s := newSource()
	got, err := d.Table(s)
	if err != nil || got != quote("business")+"."+quote("table'\"`;") {
		t.Fatalf("catalog names not independently quoted: %s %v", got, err)
	}
	for name, change := range map[string]func(*plugin.SourceBinding){
		"missing root": func(s *plugin.SourceBinding) { s.Path.Segments = s.Path.Segments[1:] },
		"wrong root": func(s *plugin.SourceBinding) {
			s.Path.Segments[0].Term = "service"
			s.Path.Segments[0].Kind = "service"
		},
		"wrong branch": func(s *plugin.SourceBinding) { s.Path.Segments[1].Term = "collection" },
		"wrong kind":   func(s *plugin.SourceBinding) { s.Path.Segments[2].Kind = "view" },
		"nested tail": func(s *plugin.SourceBinding) {
			s.Path.Segments = append(s.Path.Segments, plugin.EngineCatalogSegment{Term: "table", Kind: "table", Name: "tail"})
		},
		"hidden schema": func(s *plugin.SourceBinding) { s.Path.Segments[1].Name = "information_schema" },
	} {
		t.Run(name, func(t *testing.T) {
			s := newSource()
			change(&s)
			if _, err := d.Table(s); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
				t.Fatal(err)
			}
		})
	}
	c := plugin.ColumnBinding{Column: "id", Field: datatype.FieldInfo{Name: "id", Path: []string{"nested", "id"}, Type: datatype.FieldTypeBigInt, NativeType: "bigint"}}
	if _, err := d.Column(c, "scan_source"); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	if err := d.ValidateSchema([]datatype.FieldInfo{{Name: strings.Repeat("x", 65), Type: datatype.FieldTypeInt}}); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
}
