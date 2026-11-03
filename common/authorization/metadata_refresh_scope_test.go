package authorization

import (
	"reflect"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
)

func TestMetadataRefreshClipsCurrentRangesWithoutParentEnumeration(t *testing.T) {
	target := plugin.TabularNamespacePath(9, "schema", " exact.schema/ ")
	first := factstest.Binding(plugin.TabularItemPath(9, "schema", target.Segments[1].Name, "first"))
	second := factstest.Binding(plugin.TabularItemPath(9, "schema", target.Segments[1].Name, "second"))
	other := factstest.Binding(plugin.TabularItemPath(9, "schema", "other", "hidden"))
	ranges, err := IntersectMetadataRefreshScope(target, []MetadataRefreshRange{{Item: &first}, {Item: &second}, {Item: &other}, {Item: &first}})
	if err != nil || len(ranges) != 2 {
		t.Fatal("precise permission set drift", ranges, err)
	}
	for _, r := range ranges {
		if r.Node != nil || r.Item == nil {
			t.Fatal("precise items were expanded into an enumeration")
		}
	}
	if !MetadataRefreshScopeStillCovered(ranges, ranges, 9) || MetadataRefreshScopeStillCovered(ranges, ranges[:1], 9) {
		t.Fatal("loss of one source did not stop the frozen plan")
	}
	root := SourceNodeScope{Target: plugin.FileRootPath(9), Recursive: true}
	if _, err := IntersectMetadataRefreshScope(root.Target, []MetadataRefreshRange{{Node: &root}}); err == nil {
		t.Fatal("Manager engine root was admitted")
	}
}

func TestMetadataRefreshNFSNodeClippingAndFrozenUpperBound(t *testing.T) {
	target := plugin.FileDirectoryPath(9, "data/ a.b ")
	root := SourceNodeScope{Target: plugin.FileRootPath(9), Recursive: true}
	clipped, err := IntersectMetadataRefreshScope(target, []MetadataRefreshRange{{Node: &root}})
	if err != nil || len(clipped) != 1 || !reflect.DeepEqual(clipped[0].Node, &SourceNodeScope{Target: target, Recursive: true}) {
		t.Fatal("ancestor scope was not clipped", clipped, err)
	}
	direct := SourceNodeScope{Target: target}
	if MetadataRefreshScopeStillCovered(clipped, []MetadataRefreshRange{{Node: &direct}}, 9) {
		t.Fatal("nonrecursive replacement enlarged current rights")
	}
	if !MetadataRefreshScopeStillCovered([]MetadataRefreshRange{{Node: &direct}}, clipped, 9) ||
		!reflect.DeepEqual(clipped[0].Node.Target, target) {
		t.Fatal("a new grant changed the frozen plan")
	}
	child := SourceNodeScope{Target: plugin.FileDirectoryPath(9, "data/ a.b /child")}
	sibling := SourceNodeScope{Target: plugin.FileDirectoryPath(9, "data/ a.b suffix")}
	mixed, err := IntersectMetadataRefreshScope(target, []MetadataRefreshRange{{Node: &child}, {Node: &sibling}})
	if err != nil || len(mixed) != 1 || !reflect.DeepEqual(mixed[0].Node, &child) {
		t.Fatal("typed child intersection drift", mixed, err)
	}
	// Mutating the original input must not rewrite a saved execution maximum.
	child.Target.Segments[2].Name = "changed"
	if mixed[0].Node.Target.Segments[3].Name != "child" || mixed[0].Node.Target.Segments[2].Name != " a.b " {
		t.Fatal("plan aliases caller-owned segments")
	}
	if MetadataRefreshScopeStillCovered(clipped, nil, 9) || MetadataRefreshScopeStillCovered(nil, clipped, 9) {
		t.Fatal("empty current rights or empty execution admitted")
	}
}

func TestMetadataRefreshDoesNotUseStaleBoundaryOrForeignEngine(t *testing.T) {
	target := plugin.TabularNamespacePath(9, "schema", "public")
	item := factstest.Binding(plugin.TabularItemPath(9, "schema", "public", "table_a"))
	foreign := factstest.Binding(plugin.TabularItemPath(10, "schema", "public", "table_a"))
	if _, err := IntersectMetadataRefreshScope(target, []MetadataRefreshRange{{Item: &foreign}}); err == nil {
		t.Fatal("foreign engine binding admitted")
	}
	changed := item
	changed.BoundaryHash = "invalid"
	if MetadataRefreshScopeStillCovered([]MetadataRefreshRange{{Item: &item}}, []MetadataRefreshRange{{Item: &changed}}, 9) {
		t.Fatal("unproven boundary admitted")
	}
	if _, err := IntersectMetadataRefreshScope(target, []MetadataRefreshRange{{}}); err == nil {
		t.Fatal("ambiguous range admitted")
	}
	var _ authorizationfacts.Binding = item
}
