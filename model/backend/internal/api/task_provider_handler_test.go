package api

import "testing"

func TestMaterializationGroupExpectationIsExact(t *testing.T) {
	id, version, err := materializationGroupExpectation(map[string]interface{}{"expected_group_id": float64(12), "expected_group_version": float64(4)})
	if err != nil || id != 12 || version != 4 {
		t.Fatalf("expectation = %d/%d, err=%v", id, version, err)
	}
	for _, parameters := range []map[string]interface{}{
		{},
		{"expected_group_id": float64(12), "expected_group_version": float64(4), "physical_table": "public.orders"},
		{"expected_group_id": float64(12), "expected_group_version": float64(4.5)},
	} {
		if _, _, err := materializationGroupExpectation(parameters); err == nil {
			t.Fatalf("invalid parameters accepted: %#v", parameters)
		}
	}
}
