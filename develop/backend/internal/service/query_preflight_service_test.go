package service

import (
	"testing"
	"time"

	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
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

func TestAnalyzeRelationParameterQueryDoesNotTreatCTEsOrLiteralsAsFields(t *testing.T) {
	query := `WITH governed_activities AS (
  SELECT activity_id, activity_date_raw
  FROM activities
  WHERE COALESCE(activity_status, '') NOT IN ('拟定中', '已取消')
), eligible_members AS (
  SELECT m.person_id, NULLIF(BTRIM(m.member_nickname_snapshot), '') AS person_nickname,
         a.activity_date_raw, m.activity_id, m.member_index,
         'persons' AS source_kind, 'activity_member_snapshot' AS snapshot_kind
  FROM members AS m
  JOIN governed_activities AS a ON a.activity_id = m.activity_id
), ranked_snapshots AS (
  SELECT *, ROW_NUMBER() OVER (PARTITION BY person_id ORDER BY activity_date_raw DESC) AS rank
  FROM eligible_members
)
SELECT r.person_id, p.person_name
FROM ranked_snapshots AS r
LEFT JOIN persons AS p ON p.person_id = r.person_id
WHERE r.rank = 1`

	analysis, err := analyzeRelationParameterQuery(
		"postgresql", "sql", query, []models.QueryParameterDefinition{
			{Name: "persons", Type: "relation"}, {Name: "activities", Type: "relation"}, {Name: "members", Type: "relation"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.SchemaCoverage != "unknown" || len(analysis.Diagnostics) != 0 {
		t.Fatalf("relation query analysis = %#v", analysis)
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
