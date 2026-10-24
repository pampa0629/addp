package service

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/addp/common/execution"
)

func TestExecutionRetentionAssessmentLogsOnlyBatchSummary(t *testing.T) {
	var output bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&output, nil))
	report := &execution.RetentionAssessment{ReadOnly: true, SeedsEvaluated: 3, CompleteTrees: 1, AgeEligibleTrees: 1,
		NextAfterID: 42, HasMore: true, ReasonCounts: map[execution.RetentionReason]int{execution.RetentionOwnerUnverified: 1, execution.RetentionConsumersUnverified: 1},
		Trees: []execution.RetentionTreeAssessment{{TenantID: 987654, AnchorExecutionID: "private-execution-identity"}}}
	logExecutionRetentionAssessment(log, report)
	var got map[string]interface{}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["read_only"] != true || got["age_eligible_trees"] != float64(1) || got["next_after_id"] != float64(42) || got["has_more"] != true {
		t.Fatal("lost bounded read-only report", got)
	}
	for _, forbidden := range []string{"private-execution-identity", "987654", "tenant_id", "anchor_execution_id", "execution_config", "authorization", "metadata", "delete_allowed"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("maintenance log exposed %s", forbidden)
		}
	}
}
