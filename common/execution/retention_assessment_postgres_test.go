package execution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestExecutionRetentionAssessmentAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_EXECUTION_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_EXECUTION_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-OverviewRetention - 24*time.Hour)
	fixture := func(t *testing.T, tenant int, parent *string) *TaskExecution {
		t.Helper()
		completion := old
		row := &TaskExecution{ExecutionID: uuid.NewString(), TenantID: tenant, Module: ModuleDevelop, TaskType: TaskTypeWorkflow,
			Source: ModuleDevelop, Status: ExecutionStatusSuccess, TriggerType: TriggerTypeManual, CompletedAt: &completion, ParentExecutionID: parent,
			ExecutionConfig: models.JSONMap{"query": "private-query-marker"}, ErrorDetails: models.JSONMap{"message": "private-error-marker"},
			Metadata: models.JSONMap{"result": "professional-result-marker", "lineage_facts": map[string]interface{}{"schema_version": "unassessed"}}}
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			for _, model := range []interface{}{&Event{}, &TaskExecution{}} {
				if err := db.Where("execution_id = ?", row.ExecutionID).Delete(model).Error; err != nil {
					t.Error(err)
				}
				var count int64
				if err := db.Model(model).Where("execution_id = ?", row.ExecutionID).Count(&count).Error; err != nil || count != 0 {
					t.Errorf("retention fixture residual=%d: %v", count, err)
				}
			}
		})
		return row
	}
	assess := func(t *testing.T, after int64, options RetentionAssessmentOptions) *RetentionAssessment {
		t.Helper()
		options.AfterID = after
		if options.SeedLimit == 0 {
			options.SeedLimit = 1
		}
		report, err := AssessRetention(t.Context(), db, options)
		if err != nil {
			t.Fatal(err)
		}
		if !report.ReadOnly || report.RowsRead > 10000 || report.Queries > 500 {
			t.Fatal("invalid bounded report", report)
		}
		for _, tree := range report.Trees {
			if !retentionHasReason(tree, RetentionOwnerUnverified) || !retentionHasReason(tree, RetentionConsumersUnverified) {
				t.Fatal("Owner coverage guessed", tree)
			}
		}
		return report
	}
	first := func(t *testing.T, report *RetentionAssessment) RetentionTreeAssessment {
		t.Helper()
		if len(report.Trees) != 1 {
			t.Fatalf("wanted one observed tree, got %+v", report)
		}
		return report.Trees[0]
	}
	t.Run("whole_tree_from_child_seed_and_professional_facts_unchanged", func(t *testing.T) {
		root := fixture(t, 7, nil)
		child := fixture(t, 7, &root.ExecutionID)
		grandchild := fixture(t, 7, &child.ExecutionID)
		ids := []string{root.ExecutionID, child.ExecutionID, grandchild.ExecutionID}
		load := func() []TaskExecution {
			var rows []TaskExecution
			if err := db.Where("execution_id IN ?", ids).Order("id").Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			return rows
		}
		before := load()
		event := Event{ExecutionID: child.ExecutionID, TenantID: 7, Module: ModuleDevelop, TaskType: TaskTypeWorkflow,
			Attempt: 1, Kind: "progress", OccurredAt: old, Counters: models.JSONMap{"progress": 1}}
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
		var beforeEvent Event
		if err := db.First(&beforeEvent, event.ID).Error; err != nil {
			t.Fatal(err)
		}
		report := assess(t, child.ID-1, RetentionAssessmentOptions{})
		encoded, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"private-query-marker", "private-error-marker", "professional-result-marker", "lineage_facts", "authorization"} {
			if strings.Contains(string(encoded), marker) {
				t.Fatalf("assessment exposed private facts: %s", marker)
			}
		}
		tree := first(t, report)
		if !tree.Complete || !tree.AgeEligible || tree.ObservedMembers != 3 || report.AgeEligibleTrees != 1 {
			t.Fatal("child seed lost parent or descendants", tree)
		}
		if !reflect.DeepEqual(before, load()) {
			t.Fatal("read-only assessment changed execution/professional facts")
		}
		var got Event
		if err := db.First(&got, event.ID).Error; err != nil || !reflect.DeepEqual(beforeEvent, got) {
			t.Fatal("assessment changed even an expired process event", err)
		}
	})
	t.Run("young_child_retains_old_parent", func(t *testing.T) {
		root := fixture(t, 7, nil)
		child := fixture(t, 7, &root.ExecutionID)
		if err := db.Model(child).Update("completed_at", time.Now().UTC()).Error; err != nil {
			t.Fatal(err)
		}
		tree := first(t, assess(t, root.ID-1, RetentionAssessmentOptions{}))
		if tree.AgeEligible || !retentionHasReason(tree, RetentionNotExpired) {
			t.Fatal("old parent hid young child", tree)
		}
	})
	t.Run("foreign_parent_and_foreign_child_do_not_merge_tenants", func(t *testing.T) {
		root := fixture(t, 7, nil)
		child := fixture(t, 8, &root.ExecutionID)
		for _, row := range []*TaskExecution{root, child} {
			tree := first(t, assess(t, row.ID-1, RetentionAssessmentOptions{}))
			if tree.TenantID != row.TenantID || tree.ObservedMembers != 1 || tree.AgeEligible || !retentionHasReason(tree, RetentionRelationship) {
				t.Fatal("foreign execution entered tenant tree", tree)
			}
		}
	})
	t.Run("cycles_and_missing_parent_are_not_roots", func(t *testing.T) {
		a := fixture(t, 7, nil)
		b := fixture(t, 7, &a.ExecutionID)
		if err := db.Model(a).Update("parent_execution_id", b.ExecutionID).Error; err != nil {
			t.Fatal(err)
		}
		tree := first(t, assess(t, a.ID-1, RetentionAssessmentOptions{}))
		if tree.AgeEligible || tree.ObservedMembers != 2 || !retentionHasReason(tree, RetentionRelationship) {
			t.Fatal("cycle accepted", tree)
		}
		missing := uuid.NewString()
		orphan := fixture(t, 7, &missing)
		tree = first(t, assess(t, orphan.ID-1, RetentionAssessmentOptions{}))
		if tree.AgeEligible || !retentionHasReason(tree, RetentionRelationship) {
			t.Fatal("missing parent accepted", tree)
		}
	})
	t.Run("retry_and_recovery_references_are_not_tree_edges", func(t *testing.T) {
		source := fixture(t, 7, nil)
		retry := fixture(t, 7, nil)
		if err := db.Model(retry).Updates(map[string]interface{}{"retry_of_execution_id": source.ExecutionID,
			"metadata": models.JSONMap{"recovered_from_execution_id": source.ExecutionID}}).Error; err != nil {
			t.Fatal(err)
		}
		tree := first(t, assess(t, source.ID-1, RetentionAssessmentOptions{}))
		if tree.ObservedMembers != 1 || !tree.AgeEligible || !retentionHasReason(tree, RetentionExecutionReference) {
			t.Fatal("retry/recovery was merged or ignored", tree)
		}
		foreign := fixture(t, 8, nil)
		if err := db.Model(foreign).Update("retry_of_execution_id", source.ExecutionID).Error; err != nil {
			t.Fatal(err)
		}
		tree = first(t, assess(t, source.ID-1, RetentionAssessmentOptions{}))
		if tree.AgeEligible || !retentionHasReason(tree, RetentionRelationship) {
			t.Fatal("foreign reverse reference ignored", tree)
		}
	})
	t.Run("malformed_recovery_fact_is_anomaly_without_payload", func(t *testing.T) {
		row := fixture(t, 7, nil)
		if err := db.Model(row).Update("metadata", models.JSONMap{"recovered_from_execution_id": map[string]interface{}{"secret": "private-invalid-reference"}}).Error; err != nil {
			t.Fatal(err)
		}
		tree := first(t, assess(t, row.ID-1, RetentionAssessmentOptions{}))
		if tree.AgeEligible || !retentionHasReason(tree, RetentionRelationship) {
			t.Fatal("malformed source accepted", tree)
		}
	})
	t.Run("budgets_do_not_certify_partial_tree_or_skip_unprocessed_seeds", func(t *testing.T) {
		root := fixture(t, 7, nil)
		fixture(t, 7, &root.ExecutionID)
		tree := first(t, assess(t, root.ID-1, RetentionAssessmentOptions{TreeLimit: 1}))
		if tree.Complete || tree.AgeEligible || tree.ObservedMembers != 1 || !retentionHasReason(tree, RetentionTreeBudget) {
			t.Fatal("large tree certified", tree)
		}
		report := assess(t, root.ID-1, RetentionAssessmentOptions{QueryLimit: 1})
		if report.NextAfterID != root.ID-1 || !report.HasMore || !report.BudgetExhausted || report.AgeEligibleTrees != 0 {
			t.Fatal("unprocessed seeds were skipped", report)
		}
		report = assess(t, root.ID-1, RetentionAssessmentOptions{ReadLimit: 3})
		if report.RowsRead > 3 || !report.BudgetExhausted || report.AgeEligibleTrees != 0 || first(t, report).Complete {
			t.Fatal("row read budget was exceeded or certified a partial observation", report)
		}
	})
	t.Run("query_failure_discards_partial_report", func(t *testing.T) {
		root := fixture(t, 7, nil)
		callback, queries := "retention_failure_"+uuid.NewString(), 0
		injected := errors.New("injected retention observation failure")
		if err := db.Callback().Row().Before("gorm:row").Register(callback, func(tx *gorm.DB) {
			if strings.HasSuffix(tx.Statement.Table, "task_executions") {
				queries++
				if queries == 2 {
					tx.AddError(injected)
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		report, err := AssessRetention(t.Context(), db, RetentionAssessmentOptions{AfterID: root.ID - 1})
		if removeErr := db.Callback().Row().Remove(callback); removeErr != nil {
			t.Fatal(removeErr)
		}
		if !errors.Is(err, injected) || report != nil {
			t.Fatal("query failure published partial success", report, err)
		}
	})
	t.Run("readonly_repeatable_read_snapshot_and_seed_cursor", func(t *testing.T) {
		root := fixture(t, 7, nil)
		callback := "retention_readonly_" + uuid.NewString()
		checked, injected := 0, false
		if err := db.Callback().Row().Before("gorm:row").Register(callback, func(tx *gorm.DB) {
			var mode, isolation string
			if err := tx.Statement.ConnPool.QueryRowContext(t.Context(), "SELECT current_setting('transaction_read_only'), current_setting('transaction_isolation')").Scan(&mode, &isolation); err != nil {
				tx.AddError(err)
				return
			}
			if mode != "on" || isolation != "repeatable read" {
				t.Errorf("assessment transaction is %s / %s", mode, isolation)
			}
			checked++
			if !injected && strings.HasSuffix(tx.Statement.Table, "task_executions") {
				injected = true
				fixture(t, 7, &root.ExecutionID)
			}
		}); err != nil {
			t.Fatal(err)
		}
		report, err := AssessRetention(t.Context(), db, RetentionAssessmentOptions{AfterID: root.ID - 1, SeedLimit: 1})
		if err := db.Callback().Row().Remove(callback); err != nil {
			t.Fatal(err)
		}
		if err != nil {
			t.Fatal(err)
		}
		if checked < 2 || !injected || first(t, report).ObservedMembers != 1 {
			t.Fatal("inconsistent or unchecked snapshot", report)
		}
		next := assess(t, report.NextAfterID, RetentionAssessmentOptions{})
		if first(t, next).ObservedMembers != 2 {
			t.Fatal("next snapshot failed to include new child", next)
		}
		empty := assess(t, next.NextAfterID, RetentionAssessmentOptions{})
		if len(empty.Trees) != 0 || empty.HasMore {
			t.Fatal("end cursor did not converge", empty)
		}
	})
	t.Run("cancellation_returns_no_partial_success", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if report, err := AssessRetention(ctx, db, RetentionAssessmentOptions{}); err == nil || report != nil {
			t.Fatal("cancelled assessment returned success", report, err)
		}
	})
	t.Run("inherited_writable_transaction_is_rejected", func(t *testing.T) {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		if report, err := AssessRetention(t.Context(), tx, RetentionAssessmentOptions{}); err == nil || report != nil {
			t.Fatal("nested transaction silently ignored read-only options", report, err)
		}
	})
}
