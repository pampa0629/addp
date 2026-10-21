package iam

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func recoveryReceiptTestEvents(t *testing.T, submission uuid.UUID) []AuditLog {
	t.Helper()
	detail, err := json.Marshal(map[string]any{"submission_id": submission.String(), "administrator_count": 4})
	if err != nil {
		t.Fatal(err)
	}
	events := []AuditLog{{EventName: "iam.recovery.prepared", Result: AuditResultSucceeded}, {EventName: "iam.recovery.completed", Result: AuditResultSucceeded, Details: detail}}
	for i, role := range append(append([]string{}, bootstrapRoleOrder...), bootstrapRoleOrder[0]) {
		id := int64(i + 1)
		detail, _ := json.Marshal(map[string]any{"role_key": role, "authorization_version": 7})
		events = append(events, AuditLog{EventName: "iam.recovery.administrator_credentials_replaced", Result: AuditResultSucceeded, PrincipalID: &id, Details: detail})
	}
	return events
}

func TestRecoveryReceiptClassifiesHistoryWithoutCurrentCredentials(t *testing.T) {
	now := time.Now().UTC()
	winner := uuid.New()
	attempt := &IAMRecoveryAttempt{ID: 1, Status: IAMRecoveryStatusCompleted, CompletedAt: &now, ExpiresAt: now.Add(-time.Hour)}
	events := recoveryReceiptTestEvents(t, winner)
	for _, tc := range []struct {
		reference uuid.UUID
		want      RecoveryResolutionState
	}{{winner, RecoveryResolutionCompletedThis}, {uuid.New(), RecoveryResolutionCompletedOther}} {
		r, err := resolveRecoveryReceipt(attempt, events, tc.reference, now)
		if err != nil || r.State != tc.want || r.SubmissionID != winner || len(r.Principals) != 4 || r.AuthorizationVersions[1] != 7 {
			t.Fatalf("historical receipt: %v", err)
		}
	}
	for _, broken := range [][]AuditLog{events[:2], events[1:], append(append([]AuditLog{}, events...), events[2]), append(append([]AuditLog{}, events...), events[1])} {
		if _, err := resolveRecoveryReceipt(attempt, broken, winner, now); err == nil {
			t.Fatal("incomplete or duplicate completion evidence was accepted")
		}
	}
	var detail map[string]any
	_ = json.Unmarshal(events[1].Details, &detail)
	delete(detail, "submission_id")
	events[1].Details, _ = json.Marshal(detail)
	if _, err := resolveRecoveryReceipt(attempt, events, winner, now); err == nil {
		t.Fatal("completion without a submission binding was treated as a known result")
	}
}

func TestRecoveryPreparedIsObservationAndDoesNotConsumeOrExtendSecret(t *testing.T) {
	now := time.Now().UTC()
	secretHash := "unchanged-hash"
	attempt := &IAMRecoveryAttempt{ID: 1, Status: IAMRecoveryStatusPrepared, SecretHash: &secretHash, ExpiresAt: now.Add(time.Minute)}
	events := []AuditLog{{EventName: "iam.recovery.prepared", Result: AuditResultSucceeded}}
	for _, tc := range []struct {
		at   time.Time
		want RecoveryResolutionState
	}{{now, RecoveryResolutionPrepared}, {attempt.ExpiresAt, RecoveryResolutionExpired}} {
		r, err := resolveRecoveryReceipt(attempt, events, uuid.New(), tc.at)
		if err != nil || r.State != tc.want || *attempt.SecretHash != secretHash || attempt.Status != IAMRecoveryStatusPrepared {
			t.Fatal("prepared/expired observation mutated authority")
		}
	}
	if _, err := resolveRecoveryReceipt(attempt, recoveryReceiptTestEvents(t, uuid.New()), uuid.New(), now); err == nil {
		t.Fatal("prepared state with committed replacement audits was accepted")
	}
}

func TestRecoverySubmissionRejectsChangedTargetFacts(t *testing.T) {
	current := make([]RecoveryAdministratorTarget, 3)
	for i, role := range bootstrapRoleOrder {
		current[i] = RecoveryAdministratorTarget{PrincipalID: int64(i + 1), RoleKey: role, RoleAssignmentID: int64(i + 10), AccountID: int64(i + 20), AuthorizationVersion: 3, PrincipalStatus: PrincipalStatusActive, AccountStatus: LocalAccountStatusActive}
	}
	if !sameRecoveryTargets(current, current) {
		t.Fatal("current targets rejected")
	}
	changes := []func(*RecoveryAdministratorTarget){func(t *RecoveryAdministratorTarget) { t.AuthorizationVersion++ }, func(t *RecoveryAdministratorTarget) { t.RoleAssignmentID++ }, func(t *RecoveryAdministratorTarget) { t.PrincipalStatus = PrincipalStatusSuspended }, func(t *RecoveryAdministratorTarget) { t.AccountStatus = LocalAccountStatusLocked }, func(t *RecoveryAdministratorTarget) { t.PrincipalID = 999 }}
	for _, change := range changes {
		old := append([]RecoveryAdministratorTarget{}, current...)
		change(&old[0])
		if sameRecoveryTargets(old, current) {
			t.Fatal("changed target observation was silently refreshed")
		}
	}
}
