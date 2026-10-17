package service

import "testing"

func TestWorkflowConflictRequiresFailedTypedRuntimeEvidence(t *testing.T) {
	for _, tc := range []struct{ taskType, suffix, status, code string }{
		{"workflow", "execution_failed", "failed", "EXECUTION_FAILED"},
		{"workflow", "execution_failed", "success", "TARGET_ALREADY_EXISTS"},
		{"workflow", "execution_failed", "running", "TARGET_ALREADY_EXISTS"},
		{"workflow", "execution_timeout", "failed", "TARGET_ALREADY_EXISTS"},
		{"query", "execution_failed", "failed", "TARGET_ALREADY_EXISTS"},
	} {
		details := executionFailureDetails(tc.taskType, tc.suffix, "target already exists: private-path", map[string]interface{}{
			"runtime_status": map[string]interface{}{"status": tc.status, "error_code": tc.code},
		})
		if details["diagnostic"] != nil || details["code"] != "develop."+tc.taskType+"."+tc.suffix {
			t.Fatalf("fabricated conflict for %+v: %+v", tc, details)
		}
	}
	if executionFailureDetails("workflow", "execution_failed", "target already exists", nil)["diagnostic"] != nil {
		t.Fatal("missing runtime evidence fabricated a conflict")
	}
}
