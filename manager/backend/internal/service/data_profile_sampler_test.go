package service

import (
	"reflect"
	"testing"
)

func TestProfileSamplePagesSpreadsBudgetAcrossSource(t *testing.T) {
	tests := []struct {
		name     string
		total    int64
		pageSize int
		maxRows  int
		want     []int
	}{
		{name: "single page", total: 500, pageSize: 500, maxRows: 10000, want: nil},
		{name: "all pages fit", total: 1500, pageSize: 500, maxRows: 2000, want: []int{2, 3}},
		{name: "spread bounded pages", total: 10000, pageSize: 500, maxRows: 2000, want: []int{7, 13, 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := profileSamplePages(tt.total, tt.pageSize, tt.maxRows); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("profileSamplePages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProfileTargetKeyNormalizesSelection(t *testing.T) {
	left := profileTargetKey(7, " addp://engine/1/item/a ", DataProfileSelection{
		ChildName: " Sheet 1 ", RefPath: "/data.csv/", NestedChildPath: "/nested/table/",
	})
	right := profileTargetKey(7, "addp://engine/1/item/a", DataProfileSelection{
		ChildName: "Sheet 1", RefPath: "data.csv", NestedChildPath: "nested/table",
	})
	if left != right {
		t.Fatalf("normalized target keys differ: %q != %q", left, right)
	}
}
