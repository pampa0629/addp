package datatype

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAccessIndexSourceVersion(t *testing.T) {
	stamp := time.Date(2026, 10, 18, 0, 0, 0, 123456789, time.UTC)
	for _, tc := range []struct {
		name     string
		source   map[string]interface{}
		size     int64
		etag     string
		modified *time.Time
		want     bool
	}{
		{"nil", nil, 20, "v1", &stamp, false},
		{"size only", map[string]interface{}{"size_bytes": 20}, 20, "v1", &stamp, false},
		{"etag", map[string]interface{}{"size_bytes": float64(20), "etag": "v1"}, 20, "v1", nil, true},
		{"mtime", map[string]interface{}{"size_bytes": 20, "last_modified_at": stamp.Format(time.RFC3339Nano)}, 20, "", &stamp, true},
		{"offset equivalent", map[string]interface{}{"size_bytes": 20, "last_modified_at": stamp.In(time.FixedZone("east", 28800)).Format(time.RFC3339Nano)}, 20, "", &stamp, true},
		{"all facts", map[string]interface{}{"size_bytes": json.Number("20"), "etag": "v1", "last_modified_at": stamp}, 20, "v1", &stamp, true},
		{"etag mismatch", map[string]interface{}{"size_bytes": 20, "etag": "v1"}, 20, "v2", &stamp, false},
		{"missing mtime", map[string]interface{}{"size_bytes": 20, "last_modified_at": stamp.Format(time.RFC3339Nano)}, 20, "", nil, false},
		{"missing size", map[string]interface{}{"etag": "v1"}, 0, "v1", nil, false},
		{"fractional size", map[string]interface{}{"size_bytes": 20.5, "etag": "v1"}, 20, "v1", nil, false},
		{"different size", map[string]interface{}{"size_bytes": 20, "etag": "v1"}, 21, "v1", nil, false},
		{"invalid mtime", map[string]interface{}{"size_bytes": 20, "etag": "v1", "last_modified_at": "invalid"}, 20, "v1", &stamp, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (&AccessIndex{Source: tc.source}).MatchesSource(tc.size, tc.etag, tc.modified); got != tc.want {
				t.Fatalf("MatchesSource()=%t want %t", got, tc.want)
			}
		})
	}
}
