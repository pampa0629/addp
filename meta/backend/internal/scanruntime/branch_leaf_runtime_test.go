package scanruntime

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestCatalogLeafItemType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node plugin.CatalogEntry
		want string
	}{
		{
			name: "dynamic schema collection",
			node: plugin.CatalogEntry{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, Role: plugin.CatalogRoleLeaf},
			want: "collection",
		},
		{
			name: "graph",
			node: plugin.CatalogEntry{Term: plugin.CatalogTermGraph, Kind: plugin.CatalogKindGraph, Role: plugin.CatalogRoleLeaf},
			want: "graph",
		},
		{
			name: "container is not item",
			node: plugin.CatalogEntry{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Role: plugin.CatalogRoleBranch},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := catalogLeafItemType(tt.node); got != tt.want {
				t.Fatalf("catalogLeafItemType() = %q, want %q", got, tt.want)
			}
		})
	}
}
