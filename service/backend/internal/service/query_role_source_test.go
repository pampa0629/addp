package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/postgresql"
)

type roleSourcePreparedTest struct {
	set      *plugin.QueryReadSet
	lineage  *plugin.QueryOutputLineage
	executed bool
}

func (p *roleSourcePreparedTest) Analysis(context.Context) (*plugin.QueryAnalysis, error) {
	return nil, nil
}
func (p *roleSourcePreparedTest) EngineReadScope(context.Context) (*plugin.QueryEngineReadScope, error) {
	return nil, nil
}
func (p *roleSourcePreparedTest) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return p.set, nil
}
func (p *roleSourcePreparedTest) OutputLineage(context.Context) (*plugin.QueryOutputLineage, error) {
	return p.lineage, nil
}
func (p *roleSourcePreparedTest) Execute(context.Context) (*plugin.QueryResult, error) {
	p.executed = true
	return nil, errors.New("metadata proof must not execute")
}

func TestSQLRoleSourceUsesProviderIdentityAndDirectAliasCopies(t *testing.T) {
	engine, err := plugin.Get("postgresql")
	if err != nil {
		t.Fatal(err)
	}
	fixture := func() *roleSourcePreparedTest {
		path := plugin.TabularItemPath(91, "schema", "public", "persons")
		set, err := plugin.NewQueryReadSet(path)
		if err != nil {
			t.Fatal(err)
		}
		return &roleSourcePreparedTest{set: set, lineage: &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "phone", Type: datatype.FieldTypeString}}, Bindings: []plugin.QueryOutputBinding{{SourcePath: []string{"id"}, OutputPath: []string{"person_id"}, Transformation: "direct"}, {SourcePath: []string{"phone"}, OutputPath: []string{"phone_alias"}, Transformation: "direct"}, {SourcePath: []string{"phone"}, OutputPath: []string{"phone_copy"}, Transformation: "direct"}}}}}}
	}
	prepared := fixture()
	source, err := sqlRoleSource(t.Context(), engine, prepared, 91)
	expected, _ := dataprotection.DataItemTargetFromCatalogPath(engine.(plugin.EngineCatalogModelProvider).EngineCatalogModel(), prepared.set.Paths[0])
	if err != nil || source.target != expected || !source.fields["phone"] || source.fields["phone_alias"] || prepared.executed {
		t.Fatalf("incorrect source identity or alias inference: %+v %v", source, err)
	}
	unused := fixture()
	unused.lineage.Sources[0].Bindings = unused.lineage.Sources[0].Bindings[:1]
	unusedSource, err := sqlRoleSource(t.Context(), engine, unused, 91)
	if err != nil || unusedSource.fields["phone"] || !unusedSource.fields["id"] {
		t.Fatalf("unused source field became a selectable output: %+v %v", unusedSource, err)
	}
	cases := map[string]func(*roleSourcePreparedTest){
		"multiple": func(p *roleSourcePreparedTest) {
			p.set.Paths = append(p.set.Paths, plugin.TabularItemPath(91, "schema", "public", "other"))
		},
		"view":    func(p *roleSourcePreparedTest) { p.set.Paths[0].Segments[1].Kind = "view" },
		"engine":  func(p *roleSourcePreparedTest) { p.set.Paths[0].EngineID = 92 },
		"derived": func(p *roleSourcePreparedTest) { p.lineage.Sources[0].Bindings[1].Transformation = "derived" },
		"opaque": func(p *roleSourcePreparedTest) {
			p.lineage.Sources[0].OpaqueOutput = true
			p.lineage.Sources[0].Bindings = nil
		},
		"missing": func(p *roleSourcePreparedTest) { p.lineage.Sources = nil },
		"foreign source": func(p *roleSourcePreparedTest) {
			p.lineage.Sources[0].Path = plugin.TabularItemPath(91, "schema", "public", "other")
		},
		"duplicate field": func(p *roleSourcePreparedTest) {
			p.lineage.Sources[0].Fields = append(p.lineage.Sources[0].Fields, p.lineage.Sources[0].Fields[0])
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := fixture()
			mutate(p)
			source, err := sqlRoleSource(t.Context(), engine, p, 91)
			if source != nil || !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) || p.executed {
				t.Fatalf("unproven source admitted: %+v %v", source, err)
			}
		})
	}
}
