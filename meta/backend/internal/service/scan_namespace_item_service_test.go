package service

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestNamespaceItemType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node plugin.CatalogNode
		want string
	}{
		{
			name: "dynamic schema collection",
			node: plugin.CatalogNode{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, IsItem: true},
			want: "collection",
		},
		{
			name: "graph",
			node: plugin.CatalogNode{Term: plugin.CatalogTermGraph, Kind: plugin.CatalogKindGraph, IsItem: true},
			want: "graph",
		},
		{
			name: "container is not item",
			node: plugin.CatalogNode{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, IsContainer: true},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := namespaceItemType(tt.node); got != tt.want {
				t.Fatalf("namespaceItemType() = %q, want %q", got, tt.want)
			}
		})
	}
}
