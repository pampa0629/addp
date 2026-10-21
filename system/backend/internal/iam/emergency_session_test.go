package iam

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestEmergencySessionDeadlineBoundariesAndImmutableReceipt(t *testing.T) {
	now := time.Now().UTC()
	deadline := now.Add(10 * time.Minute)
	r := EmergencySessionRequest{ID: uuid.New(), Status: "pending", ApprovalExpiresAt: deadline, Version: 9007199254740993, TenantID: 9007199254740995, ExecutorAuthorizationVersion: 4, TargetAuthorizationVersion: 9}
	if r.effectiveState(deadline.Add(-time.Nanosecond)) != "pending" || r.effectiveState(deadline) != "expired" {
		t.Fatal("approval expiry is not exclusive")
	}
	r.Status = "approved"
	execution := deadline.Add(15 * time.Minute)
	r.ExecuteBefore = &execution
	if r.effectiveState(execution.Add(-time.Nanosecond)) != "approved" || r.effectiveState(execution) != "expired" {
		t.Fatal("execution expiry is not exclusive")
	}
	r.Status = "executed"
	if r.effectiveState(execution.Add(time.Hour)) != "executed" {
		t.Fatal("history disappeared with time")
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"version":"9007199254740993"`, `"tenant_id":"9007199254740995"`} {
		if !strings.Contains(string(data), part) {
			t.Fatal("BIGINT identifier lost precision")
		}
	}
	if strings.Contains(string(data), "authorization_version") {
		t.Fatal("internal authorization snapshot exposed")
	}
}
