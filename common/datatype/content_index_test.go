package datatype

import (
	"encoding/json"
	"testing"
)

func TestContentIndexHelpers(t *testing.T) {
	index := NewSparseRowContentIndex("csv", 5000, 27)
	index.RowCount = 10000
	index.AddAnchor(0, 27)
	index.AddAnchor(5000, 570122)

	if !index.IsSparseRowIndex() {
		t.Fatalf("IsSparseRowIndex() = false, want true")
	}
	if len(index.Anchors) != 2 || index.Anchors[1].ByteOffset != 570122 {
		t.Fatalf("anchors = %#v", index.Anchors)
	}

	raw, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("marshaled index is not valid json: %s", raw)
	}
}
