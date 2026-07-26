package scanprocessor

import "testing"

func TestItemRowCountFromMetaAttributesPreservesExactZero(t *testing.T) {
	rowCount := itemRowCountFromMetaAttributes(map[string]interface{}{
		"type_info": map[string]interface{}{
			"table": map[string]interface{}{"row_count": int64(0)},
		},
	})
	if rowCount == nil || *rowCount != 0 {
		t.Fatalf("rowCount = %#v, want exact zero", rowCount)
	}
}
