package metacleanup

import "testing"

func TestParseCleanupRequestNormalizesRedisStreamValues(t *testing.T) {
	t.Parallel()

	event, err := ParseCleanupRequest(map[string]interface{}{
		"task_id":          "task-1",
		"action":           "scan",
		"tenant_id":        "42",
		"requested_by":     "7",
		"cleanup_mode":     "logical_cleanup",
		"trigger_type":     "manual",
		"expected_modules": `["meta","manager"]`,
		"context":          `{"engine_id":12}`,
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
	if event.CleanupMode != "logical_cleanup" || event.Context["engine_id"].(float64) != 12 {
		t.Fatalf("event = %#v", event)
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

func TestScopeFromContextReadsEngineID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  map[string]interface{}
		want uint
	}{
		{name: "uint", ctx: map[string]interface{}{"engine_id": uint(7)}, want: 7},
		{name: "float", ctx: map[string]interface{}{"engine_id": float64(8)}, want: 8},
		{name: "string", ctx: map[string]interface{}{"engine_id": "9"}, want: 9},
		{name: "missing", ctx: map[string]interface{}{}, want: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ScopeFromContext(tt.ctx).EngineID; got != tt.want {
				t.Fatalf("EngineID = %d, want %d", got, tt.want)
			}
		})
	}
}
