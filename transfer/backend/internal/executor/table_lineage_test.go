package executor

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	plugin "github.com/addp/common/engine/plugin"
)

func TestQueryTableTransferCapturesOriginalNestedFieldsAndDerivedIndex(t *testing.T) {
	path := plugin.EngineCatalogBranchLeafPath(plugin.DynamicSchemaCatalogModel(), 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	readSet, err := plugin.NewQueryReadSet(path)
	if err != nil {
		t.Fatal(err)
	}
	fields := []datatype.FieldInfo{
		{Name: "_id", Path: []string{"_id"}, Type: datatype.FieldTypeString},
		{Name: "userInfo.nickName", Path: []string{"userInfo", "nickName"}, Type: datatype.FieldTypeString},
		{Name: "members", Path: []string{"members"}, Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON},
	}
	outputs := []datatype.FieldInfo{{Name: "_id", Type: datatype.FieldTypeString}, {Name: "nick", Type: datatype.FieldTypeString}, {Name: "index", Type: datatype.FieldTypeBigInt}}
	targets := []datatype.FieldInfo{{Name: "person_id", Type: datatype.FieldTypeString}, {Name: "person_nickname", Type: datatype.FieldTypeString}, {Name: "member_index", Type: datatype.FieldTypeBigInt}}
	prepared := &lineageTestPreparedQuery{fakePreparedQuery: fakePreparedQuery{readSet: readSet}, lineage: &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, Bindings: []plugin.QueryOutputBinding{
		{SourcePath: []string{"_id"}, OutputPath: []string{"_id"}, Transformation: "direct"},
		{SourcePath: []string{"userInfo", "nickName"}, OutputPath: []string{"nick"}, Transformation: "direct"},
		{SourcePath: []string{"members"}, OutputPath: []string{"index"}, Transformation: "derived"},
	}}}}}
	provider := &lineageTestQueryProvider{fakeQueryReadSessionProvider: fakeQueryReadSessionProvider{prepared: prepared}, batch: &plugin.BatchData{Fields: outputs, Rows: []map[string]interface{}{{"_id": "p1", "nick": "nickname", "index": int64(0)}}}}
	e := &TableTransferExecutor{SourceQuerySessionProvider: provider, TargetNativeWriter: &fakeBatchWriter{}, TargetNativePreparer: &fakeTableWritePreparer{}, TargetCatalogFacts: &lineageTestCatalog{fields: targets}}
	plan := TableTransferPlan{
		Source:     TableSourcePlan{Kind: TableEndpointQuery, RuntimeQuery: &plugin.QueryRequest{Language: "mql", Query: "provider-owned"}, ExpectedQueryReadSet: readSet, QueryInputs: []TableQueryInput{{Port: "source", Path: path}}, TableInfo: &datatype.TableInfo{Fields: outputs}},
		Target:     TableTargetPlan{Kind: TableEndpointNative, Path: plugin.TabularItemPath(2, plugin.EngineCatalogTermSchema, "outdoor", "ods")},
		Transforms: []TableTransformPlan{{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Source: "_id", Target: "person_id"}, {Source: "nick", Target: "person_nickname"}, {Source: "index", Target: "member_index"}}}}},
	}
	metrics, err := e.Execute(t.Context(), plan)
	if err != nil || metrics.FieldLineage == nil || metrics.RecordsWritten != 1 {
		t.Fatalf("query execution missing field evidence: %+v, %v", metrics, err)
	}
	lineage := metrics.FieldLineage
	if !lineage.Sources["source"].HasField("userInfo.nickName") || lineage.Sources["source"].HasField("nick") || len(lineage.Mappings) != 3 {
		t.Fatalf("query aliases replaced source schema: %+v", lineage)
	}
	for _, mapping := range lineage.Mappings {
		if mapping.TargetField == "person_nickname" && (mapping.SourceField != "userInfo.nickName" || mapping.Transformation != "direct") {
			t.Fatalf("nested alias mapping: %+v", mapping)
		}
		if mapping.TargetField == "member_index" && (mapping.SourceField != "members" || mapping.Transformation != "derived") {
			t.Fatalf("array index mapping: %+v", mapping)
		}
	}
}

type lineageTestPreparedQuery struct {
	fakePreparedQuery
	lineage *plugin.QueryOutputLineage
}

func (q *lineageTestPreparedQuery) OutputLineage(context.Context) (*plugin.QueryOutputLineage, error) {
	return q.lineage.Clone(), nil
}

type lineageTestQueryProvider struct {
	fakeQueryReadSessionProvider
	batch *plugin.BatchData
}

func (p *lineageTestQueryProvider) OpenQueryReadSession(context.Context, plugin.PreparedQuery) (plugin.QueryReadSession, error) {
	return &fakeQueryReadSession{batches: []*plugin.BatchData{p.batch}}, nil
}

type lineageTestCatalog struct {
	plugin.EnginePlugin
	fields []datatype.FieldInfo
}

func (p *lineageTestCatalog) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{Fields: p.fields}}, nil
}

func TestTableFieldLineageComposesActualMappingsAndConstants(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeString}, {Name: "name", Type: datatype.FieldTypeString}}
	read := &datatype.TableInfo{Fields: fields}
	target := []datatype.FieldInfo{{Name: "client_id", Type: datatype.FieldTypeString}, {Name: "title", Type: datatype.FieldTypeString}, {Name: "label", Type: datatype.FieldTypeString}}
	plans := []TableTransformPlan{
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Source: "id", Target: "temp"}, {Source: "name", Target: "title", Default: "unknown"}, {Target: "label", Default: "customer"}}}},
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModePassthrough, Fields: []FieldMappingFieldPlan{{Source: "temp", Target: "client_id"}}}},
	}
	lineage := buildTableFieldLineage(fields, target, read, &datatype.TableInfo{Fields: target}, plans)
	if lineage == nil || len(lineage.Mappings) != 3 {
		t.Fatalf("lineage: %+v", lineage)
	}
	byTarget := map[string]int{}
	for index, mapping := range lineage.Mappings {
		byTarget[mapping.TargetField] = index
	}
	if mapping := lineage.Mappings[byTarget["client_id"]]; mapping.SourceField != "id" || mapping.Transformation != "direct" {
		t.Fatalf("rename: %+v", mapping)
	}
	if mapping := lineage.Mappings[byTarget["title"]]; mapping.SourceField != "name" || mapping.Transformation != "derived" {
		t.Fatalf("default: %+v", mapping)
	}
	if mapping := lineage.Mappings[byTarget["label"]]; mapping.SourceField != "" || mapping.InputPort != "" || mapping.Transformation != "generated" {
		t.Fatalf("constant: %+v", mapping)
	}
}

func TestTableFieldLineageDoesNotGuessAbsentFields(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeString}}
	info := &datatype.TableInfo{Fields: fields}
	if result := buildTableFieldLineage(fields, nil, info, info, nil); result != nil {
		t.Fatal("missing target schema claimed complete")
	}
	if result := buildTableFieldLineage(fields, fields, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "absent", Type: datatype.FieldTypeString}}}, info, nil); result != nil {
		t.Fatal("missing source field claimed complete")
	}
}

func TestQueryFieldLineageRejectsIncompleteOrAmbiguousProof(t *testing.T) {
	path := plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "public", "persons")
	inputs := []TableQueryInput{{Port: "source", Path: path}}
	fields := []datatype.FieldInfo{{Name: "userInfo.nickName", Path: []string{"userInfo", "nickName"}, Type: datatype.FieldTypeString}}
	target := []datatype.FieldInfo{{Name: "nick", Type: datatype.FieldTypeString}}
	info := &datatype.TableInfo{Fields: target}
	binding := plugin.QueryOutputBinding{SourcePath: []string{"userInfo", "nickName"}, OutputPath: []string{"nick"}, Transformation: "direct"}
	for _, entry := range []struct {
		name   string
		source plugin.QueryOutputSource
	}{
		{"opaque", plugin.QueryOutputSource{Path: path, Fields: fields, OpaqueOutput: true}},
		{"missing", plugin.QueryOutputSource{Path: path, Fields: fields}},
		{"no exact output", plugin.QueryOutputSource{Path: path, Fields: fields, Bindings: []plugin.QueryOutputBinding{{SourcePath: binding.SourcePath, Transformation: "derived"}}}},
		{"missing physical field", plugin.QueryOutputSource{Path: path, Fields: fields, Bindings: []plugin.QueryOutputBinding{{SourcePath: []string{"absent"}, OutputPath: binding.OutputPath, Transformation: "direct"}}}},
		{"ambiguous source name", plugin.QueryOutputSource{Path: path, Fields: append(append([]datatype.FieldInfo{}, fields...), datatype.FieldInfo{Name: "userInfo.nickName", Path: []string{"userInfo.nickName"}, Type: datatype.FieldTypeString}), Bindings: []plugin.QueryOutputBinding{binding}}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			if got := buildQueryTableFieldLineage(&plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{entry.source}}, inputs, target, info, info, nil, nil); got != nil {
				t.Fatalf("unproven query declared complete: %+v", got)
			}
		})
	}
	source := plugin.QueryOutputSource{Path: path, Fields: fields, Bindings: []plugin.QueryOutputBinding{binding}}
	if got := buildQueryTableFieldLineage(&plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{source, source}}, inputs, target, info, info, nil, nil); got != nil {
		t.Fatal("multiple sources declared complete")
	}
	lineage := buildQueryTableFieldLineage(&plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{source}}, inputs, target, info, info, nil, []string{"nick"})
	if lineage == nil || len(lineage.Mappings) != 1 || lineage.Mappings[0].Transformation != "derived" || lineage.Mappings[0].SourceField != fields[0].Name {
		t.Fatalf("masked query alias lost physical origin: %+v", lineage)
	}
	// Suppression removes the query column before field mapping; it cannot create an origin.
	if got := buildQueryTableFieldLineage(&plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{source}}, inputs, target, &datatype.TableInfo{}, info, nil, nil); got != nil {
		t.Fatal("suppressed field acquired an origin")
	}
}

func TestQueryFieldLineageComposesMultipleOriginsByPath(t *testing.T) {
	persons := plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "public", "persons")
	activities := plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "public", "activities")
	filter := plugin.TabularItemPath(11, plugin.EngineCatalogTermSchema, "public", "allowed")
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}, {Name: "name", Type: datatype.FieldTypeString}}
	// Owner order differs from canonical resource order and provider order.
	inputs := []TableQueryInput{{Port: "people", Path: persons}, {Port: "events", Path: activities}, {Port: "scope", Path: filter}}
	lineage := &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{
		{Path: activities, Fields: fields, Bindings: []plugin.QueryOutputBinding{{SourcePath: []string{"name"}, OutputPath: []string{"label"}, Transformation: "derived"}}},
		{Path: filter, Fields: fields}, // Row-only dependence is still a frozen input.
		{Path: persons, Fields: fields, Bindings: []plugin.QueryOutputBinding{
			{SourcePath: []string{"id"}, OutputPath: []string{"id"}, Transformation: "direct"},
			{SourcePath: []string{"name"}, OutputPath: []string{"label"}, Transformation: "direct"},
			{SourcePath: []string{"name"}, OutputPath: []string{"label"}, Transformation: "derived"},
		}},
	}}
	read := &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}, {Name: "label", Type: datatype.FieldTypeString}}}
	target := []datatype.FieldInfo{{Name: "person_id", Type: datatype.FieldTypeBigInt}, {Name: "person_label", Type: datatype.FieldTypeString}, {Name: "category", Type: datatype.FieldTypeString}}
	plans := []TableTransformPlan{
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Source: "id", Target: "person_id"}, {Source: "label", Target: "temp"}, {Target: "category", Default: "outdoor"}}}},
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Source: "person_id", Target: "person_id"}, {Source: "temp", Target: "person_label"}, {Source: "category", Target: "category"}}}},
	}
	got := buildQueryTableFieldLineage(lineage, inputs, target, read, &datatype.TableInfo{Fields: target}, plans, []string{"label"})
	if got == nil || len(got.Sources) != 3 || len(got.Mappings) != 4 {
		t.Fatalf("multi-source proof missing: %+v", got)
	}
	for _, port := range []string{"people", "events", "scope"} {
		if got.Sources[port].Validate() != nil || !got.Sources[port].HasField("name") || got.Sources[port].HasField("label") {
			t.Fatalf("original source snapshot lost at %s: %+v", port, got)
		}
	}
	want := map[string]string{"people/id/person_id": "derived", "people/name/person_label": "derived", "events/name/person_label": "derived", "//category": "generated"}
	for _, mapping := range got.Mappings {
		key := mapping.InputPort + "/" + mapping.SourceField + "/" + mapping.TargetField
		if mapping.OutputPort != "target" || want[key] != mapping.Transformation {
			t.Fatalf("incorrect origin: %+v", mapping)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing origins: %+v", want)
	}
	for _, entry := range []struct {
		name   string
		mutate func(*plugin.QueryOutputLineage, []TableQueryInput)
	}{
		{"opaque input", func(l *plugin.QueryOutputLineage, _ []TableQueryInput) { l.Sources[1].OpaqueOutput = true }},
		{"undeclared source", func(_ *plugin.QueryOutputLineage, i []TableQueryInput) { i[0].Path = filter }},
		{"duplicate port", func(_ *plugin.QueryOutputLineage, i []TableQueryInput) { i[0].Port = i[1].Port }},
		{"unbound result", func(l *plugin.QueryOutputLineage, _ []TableQueryInput) {
			l.Sources[2].Bindings = l.Sources[2].Bindings[1:]
		}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			l, i := lineage.Clone(), append([]TableQueryInput(nil), inputs...)
			entry.mutate(l, i)
			if got := buildQueryTableFieldLineage(l, i, target, read, &datatype.TableInfo{Fields: target}, plans, nil); got != nil {
				t.Fatalf("incomplete proof claimed complete: %+v", got)
			}
		})
	}
}
