package resume

import (
	"strings"
	"testing"
)

func TestMarkerCloneCopiesMaps(t *testing.T) {
	marker := &Marker{
		Version:        MarkerVersionV1,
		Provider:       "parquet",
		PositionUnit:   "row_group",
		ReadPosition:   map[string]interface{}{"row_group": 1},
		CommitPosition: map[string]interface{}{"rows": 10},
		Fingerprint: map[string]interface{}{
			"etag":    "abc",
			"columns": []string{"id", "name"},
			"nested": map[string]interface{}{
				"refs": []interface{}{"part-000.parquet"},
			},
		},
		Payload: map[string]interface{}{"part_ref": "p1.parquet"},
	}

	cloned := marker.Clone()
	if cloned == marker {
		t.Fatal("Clone returned same marker pointer")
	}
	cloned.ReadPosition["row_group"] = 2
	cloned.CommitPosition["rows"] = 20
	cloned.Fingerprint["etag"] = "changed"
	cloned.Fingerprint["columns"].([]string)[0] = "changed"
	cloned.Fingerprint["nested"].(map[string]interface{})["refs"].([]interface{})[0] = "changed"
	cloned.Payload["part_ref"] = "p2.parquet"

	if marker.ReadPosition["row_group"] != 1 {
		t.Fatalf("read position was mutated: %#v", marker.ReadPosition)
	}
	if marker.CommitPosition["rows"] != 10 {
		t.Fatalf("commit position was mutated: %#v", marker.CommitPosition)
	}
	if marker.Fingerprint["etag"] != "abc" {
		t.Fatalf("fingerprint was mutated: %#v", marker.Fingerprint)
	}
	if marker.Fingerprint["columns"].([]string)[0] != "id" {
		t.Fatalf("fingerprint columns were mutated: %#v", marker.Fingerprint)
	}
	if marker.Fingerprint["nested"].(map[string]interface{})["refs"].([]interface{})[0] != "part-000.parquet" {
		t.Fatalf("nested fingerprint was mutated: %#v", marker.Fingerprint)
	}
	if marker.Payload["part_ref"] != "p1.parquet" {
		t.Fatalf("payload was mutated: %#v", marker.Payload)
	}
}

func TestRejectUnsupported(t *testing.T) {
	if err := RejectUnsupported(nil, "csv"); err != nil {
		t.Fatalf("RejectUnsupported(nil) = %v, want nil", err)
	}
	err := RejectUnsupported(&Marker{Version: MarkerVersionV1}, "csv.table_reader")
	if err == nil {
		t.Fatal("RejectUnsupported(marker) = nil, want error")
	}
	if !strings.Contains(err.Error(), "csv.table_reader") || !strings.Contains(err.Error(), "resume marker") {
		t.Fatalf("error = %q, want provider and resume marker", err)
	}
}
