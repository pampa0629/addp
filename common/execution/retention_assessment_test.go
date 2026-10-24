package execution

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func retentionHasReason(tree RetentionTreeAssessment, reason RetentionReason) bool {
	for _, actual := range tree.Reasons {
		if actual == reason {
			return true
		}
	}
	return false
}

func TestRetentionAssessmentMemberFacts(t *testing.T) {
	now := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	old, boundary, future := now.Add(-OverviewRetention-time.Microsecond), now.Add(-OverviewRetention), now.Add(time.Minute)
	for _, test := range []struct {
		name   string
		change func(*retentionRow)
		want   RetentionReason
	}{
		{"expired_terminal", func(*retentionRow) {}, ""},
		{"exact_cutoff_is_retained", func(row *retentionRow) { row.CompletedAt = &boundary }, RetentionNotExpired},
		{"active", func(row *retentionRow) { row.Status = ExecutionStatusRunning }, RetentionActive},
		{"missing_completion", func(row *retentionRow) { row.CompletedAt = nil }, RetentionMissingCompletion},
		{"unknown_status", func(row *retentionRow) { row.Status = "unknown" }, RetentionUnknownStatus},
		{"live_lease_on_terminal", func(row *retentionRow) { row.LeaseExpiresAt = &future }, RetentionActiveLease},
		{"invalid_identity", func(row *retentionRow) { row.ExecutionID = "invalid" }, RetentionRelationship},
		{"invalid_tenant", func(row *retentionRow) { row.TenantID = 0 }, RetentionRelationship},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := retentionRow{ExecutionID: uuid.NewString(), TenantID: 7, Status: ExecutionStatusSuccess, CompletedAt: &old}
			test.change(&row)
			tree := RetentionTreeAssessment{Complete: true}
			reasons := map[RetentionReason]bool{RetentionOwnerUnverified: true, RetentionConsumersUnverified: true}
			assessRetentionMembers(&tree, map[string]retentionRow{row.ExecutionID: row}, now, reasons)
			if tree.AgeEligible != (test.want == "") || (test.want != "" && !retentionHasReason(tree, test.want)) {
				t.Fatalf("unexpected assessment: %+v", tree)
			}
			if !retentionHasReason(tree, RetentionOwnerUnverified) || !retentionHasReason(tree, RetentionConsumersUnverified) {
				t.Fatal("unknown Owner/consumer coverage became permission to release")
			}
		})
	}
	for _, status := range []string{ExecutionStatusSuccess, ExecutionStatusFailed, ExecutionStatusTimeout, ExecutionStatusCancelled} {
		t.Run(status, func(t *testing.T) {
			id := uuid.NewString()
			tree := RetentionTreeAssessment{Complete: true}
			assessRetentionMembers(&tree, map[string]retentionRow{id: {ExecutionID: id, TenantID: 7, Status: status, CompletedAt: &old}}, now, map[RetentionReason]bool{})
			if !tree.AgeEligible {
				t.Fatal("known terminal status was not recognized")
			}
		})
	}
}

func TestRetentionAssessmentPartialTreeCannotBecomeCandidate(t *testing.T) {
	now := time.Now().UTC()
	old, parent, child := now.Add(-OverviewRetention-time.Hour), uuid.NewString(), uuid.NewString()
	tree := RetentionTreeAssessment{Complete: false}
	assessRetentionMembers(&tree, map[string]retentionRow{child: {ExecutionID: child, TenantID: 7, ParentExecutionID: &parent, Status: ExecutionStatusSuccess, CompletedAt: &old}}, now,
		map[RetentionReason]bool{RetentionTreeBudget: true, RetentionOwnerUnverified: true, RetentionConsumersUnverified: true})
	if tree.AgeEligible || retentionHasReason(tree, RetentionRelationship) || !retentionHasReason(tree, RetentionTreeBudget) {
		t.Fatal("a budget-limited parent was mistaken for a dangling relationship or complete candidate", tree)
	}
}

func TestRetentionAssessmentBudgetsAndSafeShape(t *testing.T) {
	for _, options := range []RetentionAssessmentOptions{{AfterID: -1}, {SeedLimit: 101}, {TreeLimit: -1}, {ReadLimit: 10001}, {QueryLimit: 501}} {
		if _, err := options.normalized(); err == nil {
			t.Fatalf("accepted invalid limits: %+v", options)
		}
	}
	if _, err := AssessRetention(t.Context(), nil, RetentionAssessmentOptions{}); err == nil {
		t.Fatal("nil store accepted")
	}
	encoded, err := json.Marshal(RetentionAssessment{ReadOnly: true, Trees: []RetentionTreeAssessment{{Reasons: []RetentionReason{RetentionOwnerUnverified, RetentionConsumersUnverified}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"execution_config", "metadata", "result", "lease_token", "authorization_id", "can_delete", "delete_allowed", "owner_unavailable"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("report contains forbidden claim or payload: %s", forbidden)
		}
	}
}
