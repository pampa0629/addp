package metacleanup

import "testing"

func TestParseCleanupRequestNormalizesRedisStreamValues(t *testing.T) {
	t.Parallel()

	event, err := ParseCleanupRequest(map[string]interface{}{
		"task_id":          "task-1",
		"action":           "scan",
		"tenant_id":        "42",
		"requested_by":     "7",
		"delete_type":      "soft",
		"expected_modules": `["meta","manager"]`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.TenantID != 42 || event.RequestedBy != 7 {
		t.Fatalf("ids = tenant:%d requested_by:%d", event.TenantID, event.RequestedBy)
	}
	if len(event.ExpectedModules) != 2 || event.ExpectedModules[0] != "meta" {
		t.Fatalf("expected_modules = %#v", event.ExpectedModules)
	}
}

func TestToMap(t *testing.T) {
	t.Parallel()

	got := ToMap(struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}{Name: "roads", Size: 3})

	if got["name"] != "roads" || got["size"].(float64) != 3 {
		t.Fatalf("map = %#v", got)
	}
}
