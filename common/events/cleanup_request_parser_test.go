package events

import "testing"

func TestParseCleanupRequestNormalizesRedisStreamValues(t *testing.T) {
	t.Parallel()

	event, err := ParseCleanupRequest(map[string]interface{}{
		"task_id":             "cleanup-1",
		"action":              CleanupActionExecute,
		"tenant_id":           "42",
		"requested_by":        "7",
		"cleanup_mode":        CleanupModeLogical,
		"trigger_type":        "event",
		"cause_event":         CleanupCauseEngineDeleted,
		"expected_modules":    `["meta","manager"]`,
		"based_on_scan":       "scan-1",
		"parent_execution_id": "execution-1",
		"context":             `{"engine_id":12,"item_fingerprint":"fp-1"}`,
	})
	if err != nil {
		t.Fatalf("ParseCleanupRequest() error = %v", err)
	}

	if event.TaskID != "cleanup-1" || event.Action != CleanupActionExecute {
		t.Fatalf("event identity = %#v", event)
	}
	if event.TenantID != 42 || event.RequestedBy != 7 {
		t.Fatalf("ids = tenant:%d requested_by:%d", event.TenantID, event.RequestedBy)
	}
	if event.CleanupMode != CleanupModeLogical || event.TriggerType != "event" || event.CauseEvent != CleanupCauseEngineDeleted {
		t.Fatalf("event trigger fields = %#v", event)
	}
	if len(event.ExpectedModules) != 2 || event.ExpectedModules[0] != ModuleMeta || event.ExpectedModules[1] != ModuleManager {
		t.Fatalf("expected_modules = %#v", event.ExpectedModules)
	}
	if event.BasedOnScan != "scan-1" || event.ParentExecutionID != "execution-1" {
		t.Fatalf("execution linkage = based_on_scan:%q parent:%q", event.BasedOnScan, event.ParentExecutionID)
	}
	if event.Context["engine_id"].(float64) != 12 || event.Context["item_fingerprint"] != "fp-1" {
		t.Fatalf("context = %#v", event.Context)
	}
}

func TestParseCleanupRequestKeepsTypedValues(t *testing.T) {
	t.Parallel()

	event, err := ParseCleanupRequest(map[string]interface{}{
		"task_id":          "cleanup-2",
		"action":           CleanupActionScan,
		"tenant_id":        uint(3),
		"requested_by":     uint(9),
		"trigger_type":     "manual",
		"expected_modules": []string{ModuleManager},
		"context":          map[string]interface{}{"item_id": uint(99)},
	})
	if err != nil {
		t.Fatalf("ParseCleanupRequest() error = %v", err)
	}

	if event.TenantID != 3 || event.RequestedBy != 9 {
		t.Fatalf("ids = tenant:%d requested_by:%d", event.TenantID, event.RequestedBy)
	}
	if len(event.ExpectedModules) != 1 || event.ExpectedModules[0] != ModuleManager {
		t.Fatalf("expected_modules = %#v", event.ExpectedModules)
	}
	if event.Context["item_id"].(float64) != 99 {
		t.Fatalf("context = %#v", event.Context)
	}
}

func TestValidateCleanupMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{CleanupModeLogical, CleanupModePhysical} {
		if err := ValidateCleanupMode(mode); err != nil {
			t.Fatalf("ValidateCleanupMode(%q) error = %v", mode, err)
		}
	}

	for _, mode := range []string{"soft_delete", "hard_delete", " " + CleanupModeLogical + " "} {
		if err := ValidateCleanupMode(mode); err == nil {
			t.Fatalf("ValidateCleanupMode(%q) should reject non-canonical cleanup mode", mode)
		}
	}
}
