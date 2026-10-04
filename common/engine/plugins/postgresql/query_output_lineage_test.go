package postgresql

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pgquery "github.com/pganalyze/pg_query_go/v6"
)

func TestResolvePostgresSelectOutputLineageComposesServiceSubquery(t *testing.T) {
	statement := parsePostgresLineageSelect(t, `
		SELECT addp_source.id, addp_source.contact
		FROM (SELECT id, phone AS contact FROM public.people) AS addp_source
		ORDER BY addp_source.id ASC LIMIT 3`)
	sources := []plugin.QueryOutputSource{{
		Path: plugin.TabularItemPath(7, plugin.EngineCatalogTermSchema, "public", "people"),
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "phone", Type: datatype.FieldTypeString},
		},
	}}
	catalog := &fakePostgresReadCatalog{resolved: map[postgresRelationReference]postgresResolvedRelation{
		{Schema: "public", Name: "people"}: {OID: 1, Schema: "public", Name: "people", Relkind: "r"},
	}}
	resolved, err := resolvePostgresSelectOutputLineage(context.Background(), catalog, statement, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].OpaqueOutput || resolved[0].IdentityOutput || len(resolved[0].Bindings) != 2 {
		t.Fatalf("lineage = %#v", resolved)
	}
	assertPostgresOutputBinding(t, resolved[0].Bindings[0], "id", "id", plugin.QueryOutputTransformationDirect)
	assertPostgresOutputBinding(t, resolved[0].Bindings[1], "phone", "contact", plugin.QueryOutputTransformationDirect)
}

func TestResolvePostgresSelectOutputLineagePreservesDerivedServiceOutput(t *testing.T) {
	statement := parsePostgresLineageSelect(t, `
		SELECT left(addp_source.contact, 3) AS prefix
		FROM (SELECT phone AS contact FROM public.people) AS addp_source`)
	sources := []plugin.QueryOutputSource{{
		Path:   plugin.TabularItemPath(7, plugin.EngineCatalogTermSchema, "public", "people"),
		Fields: []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}},
	}}
	catalog := &fakePostgresReadCatalog{resolved: map[postgresRelationReference]postgresResolvedRelation{
		{Schema: "public", Name: "people"}: {OID: 1, Schema: "public", Name: "people", Relkind: "r"},
	}}
	resolved, err := resolvePostgresSelectOutputLineage(context.Background(), catalog, statement, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].OpaqueOutput || len(resolved[0].Bindings) != 1 {
		t.Fatalf("lineage = %#v", resolved)
	}
	assertPostgresOutputBinding(t, resolved[0].Bindings[0], "phone", "prefix", plugin.QueryOutputTransformationDerived)
}

func parsePostgresLineageSelect(t *testing.T, query string) *pgquery.SelectStmt {
	t.Helper()
	parsed, err := pgquery.Parse(query)
	if err != nil || len(parsed.GetStmts()) != 1 {
		t.Fatalf("parse query: %v", err)
	}
	statement := parsed.GetStmts()[0].GetStmt().GetSelectStmt()
	if statement == nil {
		t.Fatal("query did not parse as SELECT")
	}
	return statement
}

func assertPostgresOutputBinding(t *testing.T, binding plugin.QueryOutputBinding, source, output, transformation string) {
	t.Helper()
	if len(binding.SourcePath) != 1 || binding.SourcePath[0] != source || len(binding.OutputPath) != 1 || binding.OutputPath[0] != output || binding.Transformation != transformation {
		t.Fatalf("binding = %#v", binding)
	}
}

const postgresDWDLineageQuery = `WITH member_rows AS (
 SELECT m.person_id, m.activity_id,
 CASE m.member_status WHEN '报名中' THEN 'signup' WHEN '领队' THEN 'leader' WHEN '领队组' THEN 'leader_group' END AS member_status
 FROM outdoor.members m JOIN outdoor.activities a USING (activity_id)
 WHERE a.is_effective_activity AND m.member_status IN ('报名中','领队','领队组')
 AND NULLIF(BTRIM(m.person_id), '') IS NOT NULL
), combined AS (
 SELECT person_id, activity_id, member_status FROM member_rows
 UNION ALL SELECT a.current_leader_person_id, a.activity_id, 'leader'
 FROM outdoor.activities a WHERE a.is_effective_activity AND a.current_leader_person_id IS NOT NULL
), result AS (
 SELECT DISTINCT ON (c.person_id,c.activity_id) c.person_id,c.activity_id,
 CASE WHEN c.person_id=a.current_leader_person_id THEN 'leader' ELSE c.member_status END AS member_status,
 a.activity_date,a.activity_intensity,c.person_id=a.current_leader_person_id AS is_current_leader,p.person_nickname
 FROM combined c JOIN outdoor.activities a USING(activity_id)
 LEFT JOIN outdoor.persons p USING(person_id)
 ORDER BY c.person_id,c.activity_id,CASE WHEN c.member_status='leader' THEN 0 ELSE 1 END
) SELECT person_id,activity_id,member_status,activity_date,activity_intensity,is_current_leader,person_nickname FROM result`

func postgresDWDLineageFixture() ([]plugin.QueryOutputSource, *fakePostgresReadCatalog) {
	sources := []plugin.QueryOutputSource{}
	catalog := &fakePostgresReadCatalog{resolved: map[postgresRelationReference]postgresResolvedRelation{}}
	for _, table := range []struct {
		name   string
		fields []string
	}{
		{"members", []string{"person_id", "activity_id", "member_status"}},
		{"activities", []string{"activity_id", "current_leader_person_id", "is_effective_activity", "activity_date", "activity_intensity"}},
		{"persons", []string{"person_id", "person_nickname"}},
	} {
		fields := []datatype.FieldInfo{}
		for _, name := range table.fields {
			fields = append(fields, datatype.FieldInfo{Name: name, Type: datatype.FieldTypeString})
		}
		sources = append(sources, plugin.QueryOutputSource{Path: plugin.TabularItemPath(7, plugin.EngineCatalogTermSchema, "outdoor", table.name), Fields: fields})
		catalog.resolved[postgresRelationReference{Schema: "outdoor", Name: table.name}] = postgresResolvedRelation{OID: int64(len(sources)), Schema: "outdoor", Name: table.name, Relkind: "r"}
	}
	return sources, catalog
}

func TestResolvePostgresSelectOutputLineageDWD(t *testing.T) {
	sources, catalog := postgresDWDLineageFixture()
	resolved, err := resolvePostgresSelectOutputLineage(t.Context(), catalog, parsePostgresLineageSelect(t, postgresDWDLineageQuery), sources)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, source := range resolved {
		if source.OpaqueOutput {
			t.Fatalf("DWD source opaque: %+v", source.Path)
		}
		table := source.Path.Segments[len(source.Path.Segments)-1].Name
		for _, binding := range source.Bindings {
			got[fmt.Sprintf("%s.%s->%s", table, binding.SourcePath[0], binding.OutputPath[0])] = binding.Transformation
		}
	}
	want := map[string]string{
		"members.person_id->person_id": "derived", "activities.current_leader_person_id->person_id": "derived",
		"members.activity_id->activity_id": "derived", "activities.activity_id->activity_id": "derived",
		"members.member_status->member_status": "derived", "members.person_id->member_status": "derived", "activities.current_leader_person_id->member_status": "derived",
		"members.person_id->is_current_leader": "derived", "activities.current_leader_person_id->is_current_leader": "derived",
		"activities.activity_date->activity_date": "direct", "activities.activity_intensity->activity_intensity": "direct", "persons.person_nickname->person_nickname": "direct",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DWD bindings = %#v; want %#v", got, want)
	}
}

func TestResolvePostgresSelectOutputLineageScopesAndUncertainty(t *testing.T) {
	for _, test := range []struct {
		name, query string
		opaque      bool
		bindings    map[string]string
	}{
		{name: "qualified JOIN ignores predicate", query: `SELECT m.person_id FROM outdoor.members m JOIN outdoor.activities a ON m.activity_id=a.activity_id WHERE a.is_effective_activity ORDER BY a.activity_id`, bindings: map[string]string{"members.person_id->person_id": "direct"}},
		{name: "USING left value", query: `SELECT activity_id FROM outdoor.members m LEFT JOIN outdoor.activities a USING(activity_id)`, bindings: map[string]string{"members.activity_id->activity_id": "derived"}},
		{name: "USING right value", query: `SELECT activity_id FROM outdoor.members m RIGHT JOIN outdoor.activities a USING(activity_id)`, bindings: map[string]string{"activities.activity_id->activity_id": "derived"}},
		{name: "USING full coalesces", query: `SELECT activity_id FROM outdoor.members m FULL JOIN outdoor.activities a USING(activity_id)`, bindings: map[string]string{"members.activity_id->activity_id": "derived", "activities.activity_id->activity_id": "derived"}},
		{name: "CTE renamed wildcard", query: `WITH x(who,what,status) AS (SELECT * FROM outdoor.members) SELECT x.who AS person FROM x`, bindings: map[string]string{"members.person_id->person": "direct"}},
		{name: "derived survives layers", query: `WITH x AS (SELECT upper(person_nickname) AS nick FROM outdoor.persons) SELECT nick AS name FROM x`, bindings: map[string]string{"persons.person_nickname->name": "derived"}},
		{name: "generated survives layers", query: `WITH x AS (SELECT 'fixed' AS nick FROM outdoor.persons) SELECT nick AS name FROM x`, bindings: map[string]string{}},
		{name: "union generated branch", query: `SELECT person_id AS person FROM outdoor.members UNION ALL SELECT 'anonymous'`, bindings: map[string]string{"members.person_id->person": "derived"}},
		{name: "aggregate FILTER is a row dependency", query: `SELECT count(m.person_id) FILTER (WHERE a.is_effective_activity) AS n FROM outdoor.members m JOIN outdoor.activities a ON m.activity_id=a.activity_id`, bindings: map[string]string{"members.person_id->n": "derived"}},
		{name: "count star FILTER has no field values", query: `SELECT count(*) FILTER (WHERE is_effective_activity) AS n FROM outdoor.activities`, bindings: map[string]string{}},
		{name: "ordered set aggregate", query: `SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY activity_intensity) AS n FROM outdoor.activities`, opaque: true},
		{name: "ambiguous unqualified", query: `SELECT activity_id FROM outdoor.members m JOIN outdoor.activities a ON true`, opaque: true},
		{name: "duplicate output", query: `SELECT m.activity_id,a.activity_id FROM outdoor.members m JOIN outdoor.activities a ON true`, opaque: true},
		{name: "unknown column", query: `SELECT missing FROM outdoor.members`, opaque: true},
		{name: "recursive CTE", query: `WITH RECURSIVE x AS (SELECT person_id FROM outdoor.members) SELECT * FROM x`, opaque: true},
		{name: "scalar subquery", query: `SELECT (SELECT person_nickname FROM outdoor.persons LIMIT 1) AS nick FROM outdoor.members`, opaque: true},
		{name: "window scope", query: `SELECT row_number() OVER (ORDER BY person_id) AS n FROM outdoor.members`, opaque: true},
		{name: "window named scope", query: `SELECT row_number() OVER w AS n FROM outdoor.members WINDOW w AS (ORDER BY person_id)`, opaque: true},
		{name: "anonymous cast name", query: `SELECT 1::integer FROM outdoor.members`, opaque: true},
		{name: "natural JOIN", query: `SELECT person_id FROM outdoor.members NATURAL JOIN outdoor.persons`, opaque: true},
		{name: "CTE must not shadow qualified table", query: `WITH persons AS (SELECT person_id FROM outdoor.members) SELECT person_nickname FROM outdoor.persons`, bindings: map[string]string{"persons.person_nickname->person_nickname": "direct"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources, catalog := postgresDWDLineageFixture()
			resolved, err := resolvePostgresSelectOutputLineage(t.Context(), catalog, parsePostgresLineageSelect(t, test.query), sources)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, source := range resolved {
				if source.OpaqueOutput != test.opaque {
					t.Fatalf("opaque=%v; want %v", source.OpaqueOutput, test.opaque)
				}
				if test.opaque {
					if source.IdentityOutput || len(source.Bindings) > 0 {
						t.Fatal("opaque source leaked mappings")
					}
					continue
				}
				table := source.Path.Segments[len(source.Path.Segments)-1].Name
				for _, binding := range source.Bindings {
					got[fmt.Sprintf("%s.%s->%s", table, binding.SourcePath[0], binding.OutputPath[0])] = binding.Transformation
				}
			}
			if !test.opaque && !reflect.DeepEqual(got, test.bindings) {
				t.Fatalf("bindings=%#v; want %#v", got, test.bindings)
			}
			if err := plugin.ValidateQueryOutputLineage(&plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{sources[0].Path, sources[1].Path, sources[2].Path}}, &plugin.QueryOutputLineage{Sources: resolved}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
