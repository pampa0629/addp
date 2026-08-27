package scanruntime

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestCatalogLeafItemType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node plugin.EngineCatalogEntry
		want string
	}{
		{
			name: "dynamic schema collection",
			node: plugin.EngineCatalogEntry{Term: plugin.EngineCatalogTermCollection, Kind: plugin.EngineCatalogKindCollection, Role: plugin.EngineCatalogRoleLeaf},
			want: "collection",
		},
		{
			name: "graph",
			node: plugin.EngineCatalogEntry{Term: plugin.EngineCatalogTermGraph, Kind: plugin.EngineCatalogKindGraph, Role: plugin.EngineCatalogRoleLeaf},
			want: "graph",
		},
		{
			name: "container is not item",
			node: plugin.EngineCatalogEntry{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Role: plugin.EngineCatalogRoleBranch},
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
