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
			name: "document collection",
			node: plugin.CatalogNode{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, IsItem: true},
			want: "collection",
		},
		{
			name: "graph label",
			node: plugin.CatalogNode{Term: plugin.CatalogTermLabel, Kind: plugin.CatalogKindLabel, IsItem: true},
			want: "label",
		},
		{
			name: "graph relationship",
			node: plugin.CatalogNode{Term: plugin.CatalogTermRelationship, Kind: plugin.CatalogKindRelationship, IsItem: true},
			want: "relationship",
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
