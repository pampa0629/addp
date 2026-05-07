package metacleanup

import "testing"

func TestEngineFilter(t *testing.T) {
	t.Parallel()

	if got := engineFilter([]uint{3, 5}, 9); got != "(engine_id = 3 OR engine_id = 5) AND tenant_id = 9" {
		t.Fatalf("filter = %q", got)
	}
}

func TestMapReaders(t *testing.T) {
	t.Parallel()

	values := map[string]interface{}{
		"name":      "roads",
		"engine_id": float64(12),
	}
	if got := stringValue(values, "name"); got != "roads" {
		t.Fatalf("name = %q", got)
	}
	if got := uintValue(values, "engine_id"); got != 12 {
		t.Fatalf("engine_id = %d", got)
	}
}
