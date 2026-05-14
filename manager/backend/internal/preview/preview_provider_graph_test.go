package preview

import (
	"reflect"
	"testing"
)

func TestGraphPreviewKindFallsBackToNodeType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  *PreviewRequest
		want string
	}{
		{
			name: "item type wins",
			req:  &PreviewRequest{ItemType: "label", NodeType: "relationship"},
			want: "label",
		},
		{
			name: "label node type fallback",
			req:  &PreviewRequest{NodeType: "label"},
			want: "label",
		},
		{
			name: "relationship node type fallback",
			req:  &PreviewRequest{NodeType: "relationship"},
			want: "relationship",
		},
		{
			name: "unsupported type",
			req:  &PreviewRequest{ItemType: "collection", NodeType: "collection"},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := graphPreviewKind(tt.req); got != tt.want {
				t.Fatalf("graphPreviewKind() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFlattenGraphEntityRowsIncludesEntityFields(t *testing.T) {
	t.Parallel()

	source := []map[string]interface{}{
		{
			"r": map[string]interface{}{
				"id":         "rel-1",
				"type":       "WORKS_AT",
				"properties": map[string]interface{}{},
			},
		},
	}

	columns, rows := flattenGraphEntityRows(source, "r")
	wantColumns := []string{"id", "type"}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("columns = %v, want %v", columns, wantColumns)
	}
	if len(rows) != 1 || rows[0]["id"] != "rel-1" || rows[0]["type"] != "WORKS_AT" {
		t.Fatalf("rows = %v, want relationship identity fields", rows)
	}
}
