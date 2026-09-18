package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/quality/internal/models"
)

func TestAcceptanceTracksRecordsNotCountsAndRetiresRecoveredKeys(t *testing.T) {
	db := newIssueRepositoryTestDB(t)
	r := NewIssueRepository(db)
	ctx := context.Background()
	o := models.IssueObservation{TargetKey: strings.Repeat("a", 64), PlanID: 1, RuleKey: "00000000-0000-4000-8000-000000000001", RuleType: "not_null", Severity: "error", Table: "rows", ColumnName: "value", SchemaName: "public", EngineID: 1, TotalCount: 10}
	var issue *models.Issue
	sequence := 0
	observe := func(keys ...string) {
		t.Helper()
		sequence++
		o.FailedCount = int64(len(keys))
		o.Passed = len(keys) == 0
		o.Evidence = &models.FailureEvidence{Scope: strings.Repeat("b", 64), Keys: []string{}}
		for _, key := range keys {
			o.Evidence.Keys = append(o.Evidence.Keys, strings.Repeat(key, 64))
		}
		if err := r.Reconcile(ctx, 7, string(rune('a'+sequence)), []models.IssueObservation{o}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		var err error
		issue, err = r.Get(1, 7)
		if err != nil {
			t.Fatal(err)
		}
	}
	accept := func() {
		t.Helper()
		var err error
		issue, err = r.UpdateStatus(ctx, issue.ID, 7, 99, issue.Version, "accepted", "approved historical exceptions")
		if err != nil {
			t.Fatal(err)
		}
	}
	observe("c", "d")
	staleVersion := issue.Version
	accept()
	if issue.Status != "accepted" || issue.AcceptedCount != 2 || issue.PendingCount != 0 || issue.FailedCount != 2 {
		t.Fatalf("accepted: %+v", issue)
	}
	if _, err := r.UpdateStatus(ctx, 1, 7, 99, staleVersion, "accepted", "stale"); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale: %v", err)
	}
	observe("c", "d")
	if issue.Status != "accepted" || len(issue.History) != 1 {
		t.Fatalf("repeat reopens or loses audit: %+v", issue)
	}
	observe("c", "e") // same count, one NEW failure; d recovers.
	if issue.Status != "open" || issue.AcceptedCount != 1 || issue.PendingCount != 1 {
		t.Fatalf("same count different records: %+v", issue)
	}
	observe("c", "d") // previously accepted d recurs, and must not be accepted.
	if issue.Status != "open" || issue.AcceptedCount != 1 || issue.PendingCount != 1 {
		t.Fatalf("recurrence: %+v", issue)
	}
	accept()
	observe()
	if issue.Status != "resolved" || issue.AcceptedCount != 0 {
		t.Fatalf("recovered: %+v", issue)
	}
	observe("d")
	if issue.Status != "open" || issue.AcceptedCount != 0 || issue.PendingCount != 1 {
		t.Fatalf("full recovery recurrence: %+v", issue)
	}
	manual := 0
	for _, action := range issue.History {
		if action.ActorID != nil {
			manual++
			if action.Note != "approved historical exceptions" || *action.ActorID != 99 {
				t.Fatal("lost audit")
			}
		}
	}
	if manual != 2 {
		t.Fatalf("manual history=%d", manual)
	}
	public, _ := json.Marshal(issue)
	if strings.Contains(string(public), strings.Repeat("d", 64)) || strings.Contains(string(public), "accepted_keys") || strings.Contains(string(public), `"evidence":`) {
		t.Fatal("record hashes exposed")
	}
}

func TestAcceptanceRejectsIncompleteEvidenceAndNeverCrossesScope(t *testing.T) {
	for _, reason := range []string{"not_observed", "key_not_configured", "key_not_unique_or_null", "too_many_failures", "aggregate_rule", ""} {
		t.Run("reject_"+reason, func(t *testing.T) {
			db := newIssueRepositoryTestDB(t)
			r := NewIssueRepository(db)
			o := models.IssueObservation{TargetKey: strings.Repeat("a", 64), PlanID: 1, RuleKey: "r", FailedCount: 1, TotalCount: 2, Evidence: &models.FailureEvidence{Scope: strings.Repeat("b", 64), Reason: reason}}
			if err := r.Reconcile(context.Background(), 7, "one", []models.IssueObservation{o}, time.Now()); err != nil {
				t.Fatal(err)
			}
			issue, _ := r.Get(1, 7)
			if _, err := r.UpdateStatus(context.Background(), 1, 7, 99, issue.Version, "accepted", "note"); !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("evidence accepted: %v", err)
			}
			if _, err := r.UpdateStatus(context.Background(), 1, 8, 99, issue.Version, "accepted", "note"); !errors.Is(err, commonAPI.ErrNotFound) {
				t.Fatalf("tenant leaked: %v", err)
			}
		})
	}
	for _, change := range []string{"revision", "unavailable", "target"} {
		t.Run(change, func(t *testing.T) {
			db := newIssueRepositoryTestDB(t)
			r := NewIssueRepository(db)
			o := models.IssueObservation{TargetKey: strings.Repeat("a", 64), PlanID: 1, RuleKey: "r", FailedCount: 1, TotalCount: 2, Evidence: &models.FailureEvidence{Scope: strings.Repeat("b", 64), Keys: []string{strings.Repeat("c", 64)}}}
			if err := r.Reconcile(context.Background(), 7, "one", []models.IssueObservation{o}, time.Now()); err != nil {
				t.Fatal(err)
			}
			issue, _ := r.Get(1, 7)
			if _, err := r.UpdateStatus(context.Background(), 1, 7, 99, issue.Version, "accepted", "note"); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "revision":
				o.Evidence.Scope = strings.Repeat("d", 64)
			case "unavailable":
				o.Evidence = &models.FailureEvidence{Reason: "too_many_failures"}
			case "target":
				o.TargetKey = strings.Repeat("e", 64)
			}
			if err := r.Reconcile(context.Background(), 7, "two", []models.IssueObservation{o}, time.Now()); err != nil {
				t.Fatal(err)
			}
			id := int64(1)
			if change == "target" {
				id = 2
			}
			issue, _ = r.Get(id, 7)
			if issue.Status != "open" || issue.AcceptedCount != 0 || issue.PendingCount != 1 {
				t.Fatalf("incorrect retained acceptance: %+v", issue)
			}
		})
	}
}

func TestAggregateIssueDoesNotInventPendingRecordCounts(t *testing.T) {
	db := newIssueRepositoryTestDB(t)
	r := NewIssueRepository(db)
	o := models.IssueObservation{TargetKey: strings.Repeat("a", 64), PlanID: 1, RuleKey: "r", RuleType: "row_count", FailedCount: 1, TotalCount: 0, Evidence: &models.FailureEvidence{Reason: "aggregate_rule"}}
	if err := r.Reconcile(context.Background(), 7, "one", []models.IssueObservation{o}, time.Now()); err != nil {
		t.Fatal(err)
	}
	issue, err := r.Get(1, 7)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Status != "open" || issue.FailedCount != 1 || issue.PendingCount != 0 {
		t.Fatalf("aggregate records invented: %+v", issue)
	}
}
