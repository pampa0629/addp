package execution

import (
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestProductionReadSetRequiresCanonicalBoundedSources(t *testing.T) {
	a := plugin.TabularItemPath(1, "schema", "a.b", "c")
	b := plugin.TabularItemPath(1, "schema", "x", "y")
	valid, _ := plugin.NewQueryReadSet(a, b)
	object := plugin.ObjectItemPath(39, "addp-demo", "tables/订单.parquet")
	objectSet, _ := plugin.NewQueryReadSet(object)
	multipleObjects, _ := plugin.NewQueryReadSet(object, plugin.ObjectItemPath(39, "addp-demo", "other.parquet"))
	mixed, _ := plugin.NewQueryReadSet(a, object)
	for _, tc := range []struct {
		name string
		set  *plugin.QueryReadSet
		ok   bool
	}{
		{"unavailable", nil, true}, {"complete", valid, true},
		{"single object", objectSet, true},
		{"multiple objects", multipleObjects, false},
		{"mixed native and object", mixed, false},
		{"bucket", &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{plugin.ObjectDirectoryPath(39, "addp-demo", "")}}, false},
		{"empty is not proven generation", &plugin.QueryReadSet{}, false},
		{"duplicates", &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{a, a}}, false},
		{"not canonical", &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{b, a}}, false},
		{"wrong namespace", &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{plugin.TabularItemPath(1, "database", "a", "b")}}, false},
		{"oversized", &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{plugin.TabularItemPath(1, "schema", "a", strings.Repeat("x", 128<<10))}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateProductionReadSet(tc.set); (err == nil) != tc.ok {
				t.Fatal(err)
			}
		})
	}
}
