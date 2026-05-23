package datatype

import "testing"

func TestFilterTableNativeKeepsOnlyAllowedKeys(t *testing.T) {
	allowed := NewNativeAllowedKeys("engine", "table_collation")
	native := map[string]interface{}{
		"engine":          "MergeTree",
		"table_collation": "utf8mb4_bin",
		"row_count":       int64(10),
		"":                "empty",
		"nil":             nil,
	}

	filtered := FilterTableNative(native, allowed)
	native["engine"] = "Log"

	if filtered["engine"] != "MergeTree" || filtered["table_collation"] != "utf8mb4_bin" {
		t.Fatalf("filtered native = %#v", filtered)
	}
	if _, ok := filtered["row_count"]; ok {
		t.Fatalf("standardized key should be filtered: %#v", filtered)
	}
	if _, ok := filtered[""]; ok {
		t.Fatalf("empty key should be filtered: %#v", filtered)
	}
	if _, ok := filtered["nil"]; ok {
		t.Fatalf("nil value should be filtered: %#v", filtered)
	}
}

func TestFilterTableNativeReturnsNilForNoAllowedFacts(t *testing.T) {
	if got := FilterTableNative(map[string]interface{}{"row_count": int64(10)}, NewNativeAllowedKeys("engine")); got != nil {
		t.Fatalf("FilterTableNative() = %#v, want nil", got)
	}
	if got := FilterTableNative(map[string]interface{}{"engine": "MergeTree"}, nil); got != nil {
		t.Fatalf("FilterTableNative() = %#v, want nil without allowed keys", got)
	}
}
