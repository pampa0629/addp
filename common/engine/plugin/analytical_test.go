package plugin_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/resourcetree"
)

// An external implementation uses only public contracts and its own syntax.
// It proves the interface seam, not production support or owner integration.
type independentCompiler struct{ version string }

type invalidDiagnosticCompiler struct {
	independentCompiler
	diagnostic plugin.SupportDiagnostic
}

func (c invalidDiagnosticCompiler) Check(plugin.CompileRequest) (plugin.SupportReport, error) {
	return plugin.SupportReport{Diagnostics: []plugin.SupportDiagnostic{c.diagnostic}}, nil
}

func (c independentCompiler) Identity() plugin.CompilerIdentity {
	return plugin.CompilerIdentity{ID: "external.analytics", Version: c.version}
}
func (c independentCompiler) Check(r plugin.CompileRequest) (plugin.SupportReport, error) {
	if err := r.Validate(); err != nil {
		return plugin.SupportReport{}, err
	}
	for _, s := range r.Sources {
		if len(s.Path.Segments) != 4 {
			return plugin.SupportReport{Diagnostics: []plugin.SupportDiagnostic{{Code: "catalog_path_unsupported"}}}, nil
		}
		expected := []string{"server", "catalog", "schema", "table"}
		for i, part := range s.Path.Segments {
			if part.Term != expected[i] {
				return plugin.SupportReport{Diagnostics: []plugin.SupportDiagnostic{{Code: "catalog_path_unsupported"}}}, nil
			}
		}
	}
	return plugin.SupportReport{Supported: true}, nil
}
func (c independentCompiler) Compile(r plugin.CompileRequest) (plugin.CompiledQuery, error) {
	report, err := c.Check(r)
	if err != nil {
		return plugin.CompiledQuery{}, err
	}
	if !report.Supported {
		return plugin.CompiledQuery{}, plugin.ErrAnalyticalUnsupported
	}
	native, err := json.Marshal(r.Sources[0].Path.Segments)
	if err != nil {
		return plugin.CompiledQuery{}, err
	}
	return plugin.NewCompiledQuery(r, c.Identity(), "test_algebra", "READ "+string(native), nil)
}

type independentEngine struct{ plugin.QueryRuntimeProvider }

func (independentEngine) Type() string { return "analytical-contract-test" }
func (independentEngine) AnalyticalCompiler() plugin.AnalyticalCompiler {
	return independentCompiler{"1.0"}
}

var _ plugin.AnalyticalCompilerProvider = independentEngine{}

func analyticalFixture() plugin.CompileRequest {
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeString}, {Name: "amount", Type: datatype.FieldTypeInt, Nullable: true}}
	return plugin.CompileRequest{
		Plan:     plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile, Root: "source", Nodes: []plan.Node{{ID: "source", Op: "scan", Scan: &plan.Scan{Source: "items", Fields: fields}}}, Output: plan.OutputContract{Fields: fields, StableKey: []string{"id"}}},
		Sources:  []plugin.SourceBinding{{Source: "items", Path: plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 42, Segments: []plugin.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "catalog", Kind: "namespace", Name: "lake"}, {Term: "schema", Kind: "namespace", Name: "sales"}, {Term: "table", Kind: "table", Name: "Order \" detail"}}}, Columns: []plugin.ColumnBinding{{Column: "id", Field: datatype.FieldInfo{Name: "ID", Path: []string{"ID"}, Type: datatype.FieldTypeString, NativeType: "text"}}, {Column: "amount", Field: datatype.FieldInfo{Name: "Amount", Path: []string{"Amount"}, Type: datatype.FieldTypeInt, NativeType: "integer", Nullable: true}}}}},
		Instance: plugin.AnalyticalInstance{EngineID: 42, Capability: plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{plan.SchemaVersion}, SemanticProfiles: []string{plan.SemanticProfile}}},
	}
}
func clonePackage(p plugin.AnalyticalPlanPackage) plugin.AnalyticalPlanPackage {
	data, _ := json.Marshal(p)
	var out plugin.AnalyticalPlanPackage
	_ = json.Unmarshal(data, &out)
	return out
}

func TestAnalyticalRegistryOpenInterface(t *testing.T) {
	engine := independentEngine{}
	plugin.Register(engine)
	t.Cleanup(func() { plugin.Unregister(engine.Type()) })
	compiler, err := plugin.ResolveAnalyticalCompiler(engine.Type())
	if err != nil {
		t.Fatal(err)
	}
	r := analyticalFixture()
	model := plugin.EngineCatalogModelSpec{PathVersion: plugin.EngineCatalogPathVersion, RootTerm: "server", Levels: []plugin.EngineCatalogLevelSpec{
		{Term: "catalog", Kinds: []string{"namespace"}, Role: plugin.EngineCatalogRoleBranch},
		{Term: "schema", Kinds: []string{"namespace"}, Role: plugin.EngineCatalogRoleBranch},
		{Term: "table", Kinds: []string{"table"}, Role: plugin.EngineCatalogRoleLeaf},
	}}
	r.Sources[0].Path, err = resourcetree.EngineCatalogPathFromLocator(model, &resourcetree.ResourceLocator{EngineID: r.Instance.EngineID, Type: resourcetree.TypeTable, Path: []string{"lake", "sales", "Order detail"}})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(r)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Language() != "test_algebra" || !strings.HasPrefix(compiled.Template(), "READ ") || !strings.Contains(compiled.Template(), "lake") || !strings.Contains(compiled.Template(), "sales") {
		t.Fatal("compiler lost its native syntax or catalog levels")
	}
	if compiled.Compiler() != compiler.Identity() || len(compiled.Fingerprint()) != 64 {
		t.Fatal("missing compilation identity")
	}
	output := compiled.Output()
	output.Fields[0].Name = "changed"
	output.StableKey[0] = "changed"
	if compiled.Output().Fields[0].Name != "id" || compiled.Output().StableKey[0] != "id" {
		t.Fatal("mutable compiled output")
	}
	if _, err := plugin.ResolveAnalyticalCompiler("no-such-analytical-engine"); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatalf("missing compiler: %v", err)
	}
	r.Sources[0].Path.Segments = append(r.Sources[0].Path.Segments[:1], r.Sources[0].Path.Segments[2:]...)
	if _, err := compiler.Compile(r); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatalf("native catalog check not enforced: %v", err)
	}
}

func TestAnalyticalPackageFingerprintAndIsolation(t *testing.T) {
	r := analyticalFixture()
	before, _ := json.Marshal(r)
	compiler := independentCompiler{"1.0"}
	frozen, err := plugin.NewAnalyticalPlanPackage(r, compiler)
	if err != nil {
		t.Fatal(err)
	}
	if err := frozen.Verify(r.Instance, compiler); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(r)
	if string(before) != string(after) {
		t.Fatal("freeze mutated caller")
	}
	r.Sources[0].Columns[0], r.Sources[0].Columns[1] = r.Sources[0].Columns[1], r.Sources[0].Columns[0]
	reordered, err := plugin.NewAnalyticalPlanPackage(r, compiler)
	if err != nil || reordered.PackageHash != frozen.PackageHash {
		t.Fatalf("binding order affects hash: %v", err)
	}
	r.Sources[0].Path.Segments[1].Name = "mutated"
	r.Plan.Nodes[0].Scan.Fields[0].Name = "mutated"
	if frozen.Sources[0].Path.Segments[1].Name != "lake" || frozen.Plan.Nodes[0].Scan.Fields[0].Name != "id" {
		t.Fatal("freeze retained owner aliases")
	}
	if err := frozen.Verify(analyticalFixture().Instance, compiler); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*plugin.AnalyticalPlanPackage){
		"physical name": func(p *plugin.AnalyticalPlanPackage) { p.Sources[0].Path.Segments[1].Name = "another" },
		"column path": func(p *plugin.AnalyticalPlanPackage) {
			p.Sources[0].Columns[0].Field.Path = []string{"nested", p.Sources[0].Columns[0].Field.Name}
		},
		"native type": func(p *plugin.AnalyticalPlanPackage) { p.Sources[0].Columns[0].Field.NativeType = "different" },
		"stable key":  func(p *plugin.AnalyticalPlanPackage) { p.Plan.Output.StableKey = []string{"amount"} },
		"hash":        func(p *plugin.AnalyticalPlanPackage) { p.PackageHash = strings.Repeat("0", 64) },
		"compiler":    func(p *plugin.AnalyticalPlanPackage) { p.Compiler.Version = "2.0" },
	} {
		t.Run(name, func(t *testing.T) {
			p := clonePackage(frozen)
			change(&p)
			if p.Verify(analyticalFixture().Instance, compiler) == nil {
				t.Fatal("tampering accepted")
			}
		})
	}
	if err := frozen.Verify(analyticalFixture().Instance, independentCompiler{"2.0"}); !errors.Is(err, plugin.ErrAnalyticalPlanChanged) {
		t.Fatalf("compiler upgrade silently accepted: %v", err)
	}
	instance := analyticalFixture().Instance
	instance.Capability = plugin.AnalyticalCapability{}
	if err := frozen.Verify(instance, compiler); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatalf("revoked capability accepted: %v", err)
	}
}

func TestAnalyticalBindingRejections(t *testing.T) {
	for name, change := range map[string]func(*plugin.CompileRequest){
		"cross engine":                     func(r *plugin.CompileRequest) { r.Sources[0].Path.EngineID++ },
		"missing root":                     func(r *plugin.CompileRequest) { r.Sources[0].Path.Segments = r.Sources[0].Path.Segments[1:] },
		"named structural root":            func(r *plugin.CompileRequest) { r.Sources[0].Path.Segments[0].Name = "default" },
		"missing source":                   func(r *plugin.CompileRequest) { r.Sources = nil },
		"extra source":                     func(r *plugin.CompileRequest) { r.Sources = append(r.Sources, r.Sources[0]) },
		"wrong source":                     func(r *plugin.CompileRequest) { r.Sources[0].Source = "other" },
		"missing column":                   func(r *plugin.CompileRequest) { r.Sources[0].Columns = r.Sources[0].Columns[:1] },
		"duplicate column":                 func(r *plugin.CompileRequest) { r.Sources[0].Columns[1] = r.Sources[0].Columns[0] },
		"nullable to required":             func(r *plugin.CompileRequest) { r.Sources[0].Columns[0].Field.Nullable = true },
		"missing full column path":         func(r *plugin.CompileRequest) { r.Sources[0].Columns[0].Field.Path = nil },
		"column name and path differ":      func(r *plugin.CompileRequest) { r.Sources[0].Columns[0].Field.Path = []string{"different"} },
		"native expression":                func(r *plugin.CompileRequest) { r.Sources[0].Columns[0].Field.DefaultExpression = "unsafe()" },
		"native type absent":               func(r *plugin.CompileRequest) { r.Sources[0].Columns[0].Field.NativeType = "" },
		"logical and physical type differ": func(r *plugin.CompileRequest) { r.Sources[0].Columns[0].Field.Type = datatype.FieldTypeDate },
		"unknown schema capability":        func(r *plugin.CompileRequest) { r.Instance.Capability.PlanVersions = []string{"next"} },
		"unknown semantic capability":      func(r *plugin.CompileRequest) { r.Instance.Capability.SemanticProfiles = nil },
	} {
		t.Run(name, func(t *testing.T) {
			r := analyticalFixture()
			change(&r)
			if r.Validate() == nil {
				t.Fatal("invalid binding or capability accepted")
			}
		})
	}
}

func TestAnalyticalSupportDiagnosticReferences(t *testing.T) {
	for name, diagnostic := range map[string]plugin.SupportDiagnostic{
		"unknown source":        {Code: "source_unsupported", SourceID: "other"},
		"unknown column":        {Code: "column_unsupported", SourceID: "items", ColumnID: "other"},
		"column without source": {Code: "column_unsupported", ColumnID: "id"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := plugin.NewAnalyticalPlanPackage(analyticalFixture(), invalidDiagnosticCompiler{independentCompiler: independentCompiler{"1.0"}, diagnostic: diagnostic})
			if !errors.Is(err, plugin.ErrAnalyticalInvalid) {
				t.Fatalf("invalid diagnostic accepted: %v", err)
			}
		})
	}
}

func TestAnalyticalInputBoundary(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var inspect func(reflect.Type)
	inspect = func(typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice:
			inspect(typ.Elem())
		case reflect.Interface, reflect.Map:
			t.Fatalf("untyped extension in compiler input: %v", typ)
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				for _, name := range []string{"ConnectionInfo", "Password", "DSN", "MetricID", "TenantID", "SQLQuery", "ParameterValues"} {
					if f.Name == name {
						t.Fatalf("owner or runtime state entered compiler input: %s", name)
					}
				}
				inspect(f.Type)
			}
		}
	}
	inspect(reflect.TypeOf(plugin.CompileRequest{}))
}
