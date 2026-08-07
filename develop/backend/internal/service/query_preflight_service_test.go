package service

import (
	"testing"
	"time"

	"github.com/addp/develop/backend/internal/config"
)

func TestAnalyzeQuery(t *testing.T) {
	read, err := AnalyzeQuery("sql", "SELECT * FROM activities WHERE id = 1")
	if err != nil || read.Effect != string(SQLExecutionEffectRead) || read.RiskLevel != "low" || read.RequiresConfirmation {
		t.Fatalf("read preflight = %#v, err = %v", read, err)
	}

	write, err := AnalyzeQuery("sql", "DELETE FROM activities")
	if err != nil || write.Effect != string(SQLExecutionEffectWrite) || write.RiskLevel != "medium" || !write.RequiresConfirmation {
		t.Fatalf("write preflight = %#v, err = %v", write, err)
	}

	mql, err := AnalyzeQuery("mql", `{"find":"activities"}`)
	if err != nil || mql.Effect != string(SQLExecutionEffectRead) || mql.ClassificationConfidence != "provider_read_only" {
		t.Fatalf("mql preflight = %#v, err = %v", mql, err)
	}
}

func TestQueryConfirmationTokenIsBoundToRequest(t *testing.T) {
	cfg := &config.Config{EncryptionKey: []byte("query-confirmation-test-key")}
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	token, expiresAt, err := IssueQueryConfirmationToken(cfg, 7, 11, 12, "addp://engine/12/path/activities?type=table", "fingerprint", "ddl", now)
	if err != nil || !expiresAt.After(now) {
		t.Fatalf("issue token = %q, expires = %v, err = %v", token, expiresAt, err)
	}
	if err := VerifyQueryConfirmationToken(cfg, token, 7, 11, 12, "addp://engine/12/path/activities?type=table", "fingerprint", "ddl", now.Add(time.Minute)); err != nil {
		t.Fatalf("verify token error = %v", err)
	}
	if err := VerifyQueryConfirmationToken(cfg, token, 7, 11, 12, "addp://engine/12/path/other?type=table", "fingerprint", "ddl", now.Add(time.Minute)); err == nil {
		t.Fatal("token should be bound to target locator")
	}
	if err := VerifyQueryConfirmationToken(cfg, token, 7, 11, 12, "addp://engine/12/path/activities?type=table", "other", "ddl", now.Add(time.Minute)); err == nil {
		t.Fatal("token should be bound to query fingerprint")
	}
}
