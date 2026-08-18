package oracle

import "testing"

func TestOracleSpatialMetadataFieldName(t *testing.T) {
	tests := map[string]string{
		"SHAPE":      "SHAPE",
		`"Shape"`:    "Shape",
		`"Shape""2"`: "Shape\"2",
	}
	for input, want := range tests {
		if got := oracleSpatialMetadataFieldName(input); got != want {
			t.Errorf("oracleSpatialMetadataFieldName(%q) = %q, want %q", input, got, want)
		}
	}
}
