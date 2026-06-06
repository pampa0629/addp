package scanflow

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestModeFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		opts         Options
		catalogPaths []string
		want         Mode
	}{
		{name: "ref groups", opts: Options{RefGroups: []models.ScanRefGroup{{Primary: "a.shp"}}}, want: ModeRefGroups},
		{name: "item wins over ref groups", opts: Options{ItemID: 7, RefGroups: []models.ScanRefGroup{{Primary: "a.shp"}}}, catalogPaths: []string{"bucket/path"}, want: ModeItem},
		{name: "item", opts: Options{ItemID: 7}, want: ModeItem},
		{name: "node", opts: Options{NodeID: 3}, want: ModeNode},
		{name: "targets", opts: Options{Targets: []string{"addp://engine/1"}}, want: ModeTargets},
		{name: "catalog paths", catalogPaths: []string{"bucket/path"}, want: ModeCatalogPaths},
		{name: "engine", want: ModeEngine},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ModeFor(tt.opts, tt.catalogPaths); got != tt.want {
				t.Fatalf("ModeFor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeRefGroups(t *testing.T) {
	t.Parallel()

	got := NormalizeRefGroups([]models.ScanRefGroup{
		{
			Primary: " bucket/path/roads.shp ",
			Refs: []models.ScanRef{
				{Path: " bucket/path/roads.shp ", Role: " main ", Required: true},
				{Path: " ", Role: "sidecar", Required: true},
			},
		},
		{},
	})
	want := []models.ScanRefGroup{
		{
			Primary: "bucket/path/roads.shp",
			Refs: []models.ScanRef{
				{Path: "bucket/path/roads.shp", Role: "main", Required: true},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeRefGroups() = %#v, want %#v", got, want)
	}
}

func TestNormalizeSource(t *testing.T) {
	t.Parallel()

	if got := NormalizeSource(" manager "); got != "manager" {
		t.Fatalf("source = %q, want manager", got)
	}
	if got := NormalizeSource(" "); got != "meta" {
		t.Fatalf("empty source = %q, want meta", got)
	}
}
