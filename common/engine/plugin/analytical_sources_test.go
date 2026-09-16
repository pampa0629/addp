package plugin

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/query/plan"
	"testing"
)

func TestCompiledSourceBindingsArePrivate(t *testing.T) {
	r := resultCompileRequest()
	r.Plan.Assertions = nil
	r.Plan.Nodes = []plan.Node{{ID: "rows", Op: "scan", Scan: &plan.Scan{Source: "source", Fields: r.Plan.Output.Fields}}}
	r.Sources = []SourceBinding{{Source: "source", Path: TabularItemPath(7, EngineCatalogTermSchema, "public", "source"), Columns: []ColumnBinding{{Column: "value", Field: datatype.FieldInfo{Name: "value", Path: []string{"value"}, Type: datatype.FieldTypeBigInt, NativeType: "bigint"}}}}}
	q, err := NewCompiledQuery(r, CompilerIdentity{ID: "test", Version: "1"}, "sql", "SELECT value FROM source", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Sources[0].Path.Segments[1].Name = "changed"
	r.Sources[0].Columns[0].Field.Path[0] = "changed"
	a := q.sourceBindings()
	a[0].Path.Segments[1].Name = "changed again"
	a[0].Columns[0].Field.Path[0] = "changed again"
	b := q.sourceBindings()
	if b[0].Path.Segments[1].Name != "public" || b[0].Columns[0].Field.Path[0] != "value" {
		t.Fatalf("mutable frozen source: %#v", b)
	}
}
