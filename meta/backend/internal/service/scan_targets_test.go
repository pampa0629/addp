package service

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestScanTargetFromItemUsesCatalogSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		item models.MetaItem
		want []string
	}{
		{
			name: "filesystem file scans parent directory",
			item: models.MetaItem{ItemType: "file", FullName: "container/复杂.zip"},
			want: []string{"container"},
		},
		{
			name: "filesystem root file scans filesystem root",
			item: models.MetaItem{ItemType: "file", FullName: "README.md"},
			want: nil,
		},
		{
			name: "object storage object scans exact object path",
			item: models.MetaItem{ItemType: "object", FullName: "addp/container/复杂.zip"},
			want: []string{"addp/container/复杂.zip"},
		},
		{
			name: "database table scans namespace",
			item: models.MetaItem{ItemType: "table", FullName: "public.users"},
			want: []string{"public"},
		},
		{
			name: "document collection scans database",
			item: models.MetaItem{ItemType: "collection", FullName: "business.orders"},
			want: []string{"business"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := scanTargetFromItem(tt.item); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("scanTargetFromItem() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
