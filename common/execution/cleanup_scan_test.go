package execution

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestCleanupScanReceipt(t *testing.T) { testCleanupScanReceipt(t, newCleanupProtocolDB(t)) }

func testCleanupScanReceipt(t *testing.T, db *gorm.DB) {
	for _, mode := range []string{"valid", "different_requester", "missing_scan", "wrong_tenant", "wrong_owner", "wrong_engine", "wrong_cause", "wrong_modules", "parent_failed", "child_failed", "missing_receipt", "forged_receipt", "parent_binding", "duplicate_scan"} {
		t.Run(mode, func(t *testing.T) {
			p, c, input := cleanupProtocolFixture(t, db)
			input.Context.EngineID = 1
			if err := input.Seal(); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range []TaskExecution{p, c} {
				if err := db.Model(&TaskExecution{}).Where("execution_id = ?", item.ExecutionID).Update("execution_config", models.JSONMap{cleanupInputKey: string(encoded), DisplayLanguageKey: "en"}).Error; err != nil {
					t.Fatal(err)
				}
			}

			_, pl, err := ClaimPending(t.Context(), db, cleanupIdentity(p), cleanupOptions(p), nil)
			if err != nil {
				t.Fatal(err)
			}
			child, cl, err := ClaimCleanupChild(t.Context(), db, cleanupIdentity(c), cleanupOptions(c), nil)
			if err != nil {
				t.Fatal(err)
			}
			impact, err := events.BuildCleanupImpactData([]events.CleanupImpactItem{{StableRef: "owned:1", Disposition: events.CleanupImpactWillDisable}}, "")
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := NewCleanupReceipt(*child, events.CleanupResultData{TaskID: input.RequestID, TenantID: input.TenantID, Module: c.Module, Action: "scan", Status: "success", Summary: events.CleanupResultSummary{ScannedItems: 1, RiskLevel: "low"}, Impact: &impact}, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := CompleteCleanupWithReceipt(t.Context(), db, *cl, receipt); err != nil {
				t.Fatal(err)
			}
			if err := CompleteWithLease(t.Context(), db, *pl, ExecutionStatusSuccess, nil); err != nil {
				t.Fatal(err)
			}
			next := input
			next.RequestedAt, err = DatabaseNow(db)
			if err != nil {
				t.Fatal(err)
			}
			next.Deadline = next.RequestedAt.Add(time.Minute)
			next.RequestID, next.ParentExecutionID, next.Action = uuid.NewString(), uuid.NewString(), events.CleanupActionExecute
			next.BasedOnScan, next.CleanupMode = input.RequestID, events.CleanupModeLogical
			next.Confirmation = events.CleanupConfirmation{Confirmed: true, RiskLevel: "low", ImpactDigest: strings.Repeat("a", 64)}
			parent := p
			parent.ID = 0
			parent.ExecutionID = next.ParentExecutionID
			parent.ExecutionConfig = models.JSONMap{DisplayLanguageKey: "en"}
			if mode == "different_requester" {
				actor, by := int64(8), 8
				parent.ActorPrincipalID = &actor
				parent.TriggeredBy = &by
				next.RequestedBy = 8
			}
			if err := next.Seal(); err != nil {
				t.Fatal(err)
			}
			rows, err := CreateCleanupExecutions(t.Context(), db, &parent, next)
			if err != nil {
				t.Fatal(err)
			}
			current := rows[0]
			switch mode {
			case "missing_scan":
				next.BasedOnScan = uuid.NewString()
			case "wrong_tenant":
				next.TenantID++
				current.TenantID = int(next.TenantID)
			case "wrong_owner":
				current.Module = ModuleMeta
				next.ExpectedModules = []string{ModuleMeta}
			case "wrong_engine":
				next.Context.EngineID = 2
			case "wrong_cause":
				next.CauseEvent = events.CleanupCauseEngineDeleting
			case "wrong_modules":
				next.ExpectedModules = []string{ModuleGraph, ModuleMeta}
			case "parent_failed":
				if err := db.Model(&TaskExecution{}).Where("execution_id = ?", p.ExecutionID).Update("status", ExecutionStatusFailed).Error; err != nil {
					t.Fatal(err)
				}
			case "child_failed":
				if err := db.Model(&TaskExecution{}).Where("execution_id = ?", c.ExecutionID).Update("status", ExecutionStatusFailed).Error; err != nil {
					t.Fatal(err)
				}
			case "missing_receipt", "forged_receipt":
				metadata := models.JSONMap{}
				if mode == "forged_receipt" {
					receipt.Attempt++
					raw, _ := json.Marshal(receipt)
					metadata["cleanup_receipt"] = string(raw)
				}
				if err := db.Model(&TaskExecution{}).Where("execution_id = ?", c.ExecutionID).Update("metadata", metadata).Error; err != nil {
					t.Fatal(err)
				}
			case "parent_binding":
				if err := db.Model(&TaskExecution{}).Where("execution_id = ?", p.ExecutionID).Update("actor_tenant_membership_id", 18).Error; err != nil {
					t.Fatal(err)
				}
			case "duplicate_scan":
				dup := readCleanupExecution(t, db, c)
				dup.ID = 0
				dup.ExecutionID = uuid.NewString()
				other := uuid.NewString()
				dup.ParentExecutionID = &other
				if err := db.Create(&dup).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := next.Seal(); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(next)
			current.ExecutionConfig = models.JSONMap{cleanupInputKey: string(raw), DisplayLanguageKey: "en"}
			var before []TaskExecution
			if err := db.Where("tenant_id = ?", p.TenantID).Order("id").Find(&before).Error; err != nil {
				t.Fatal(err)
			}
			got, err := ReadCleanupScanReceipt(t.Context(), db, current)
			valid := mode == "valid" || mode == "different_requester"
			if valid {
				if err != nil || got == nil || got.ImpactDigest != impact.Digest {
					t.Fatalf("valid source rejected: %v", err)
				}
			} else if err == nil || got != nil {
				t.Fatalf("invalid scan baseline accepted: %s", mode)
			}
			var after []TaskExecution
			if err := db.Where("tenant_id = ?", p.TenantID).Order("id").Find(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("baseline read wrote execution facts: %v", err)
			}
			if valid && db.Dialector.Name() == "postgres" {
				holder := db.Begin()
				if holder.Error != nil {
					t.Fatal(holder.Error)
				}
				defer holder.Rollback()
				var locked TaskExecution
				if err := holder.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "execution_id = ?", p.ExecutionID).Error; err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				if _, err := ReadCleanupScanReceipt(ctx, db, current); err != nil {
					t.Fatalf("ordinary baseline read waited on P row lock: %v", err)
				}
			}
		})
	}
}
