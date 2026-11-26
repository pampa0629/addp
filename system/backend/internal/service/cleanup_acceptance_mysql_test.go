package service

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/common/infra/bootstrap"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The runtime writes real immutable requests and executions. Count calls at
// the issue/audit boundaries so a replay cannot silently repeat either.
func TestCleanupAcceptanceAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_CLEANUP_ACCEPTANCE") != "1" {
		t.Skip("requires the disposable System and Common installation")
	}
	db, err := bootstrap.OpenPlatformRuntime(config.LoadPlatformDatabase(), "system", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, identity := range []string{"request", "source_operation"} {
		t.Run(identity, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			first := db.WithContext(ctx).Begin()
			if first.Error != nil {
				t.Fatal(first.Error)
			}
			defer first.Rollback()
			now, err := execution.DatabaseNow(first)
			if err != nil {
				t.Fatal(err)
			}
			actor, member, version, by := int64(7), int64(17), int64(1), 7
			parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 940000007, Module: execution.ModuleSystem, TaskType: execution.TaskTypeCleanup, Source: execution.ModuleSystem, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, TriggeredBy: &by, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}, MaxAttempts: 3}
			input := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: uint(parent.TenantID), RequestedBy: uint(by), Action: events.CleanupActionScan, TriggerType: execution.TriggerTypeManual, ExpectedModules: []string{"meta"}, RequestedAt: now, Deadline: now.Add(time.Minute)}
			if identity == "source_operation" {
				input.SourceOperationID = uuid.NewString()
			}
			if err := input.Seal(); err != nil {
				t.Fatal(err)
			}
			coordinator := newCleanupCoordinator(first)
			var issued, audited atomic.Int32
			coordinator.issue = func(context.Context, *gorm.DB, execution.TaskExecution) error { issued.Add(1); return nil }
			coordinator.audit = func(context.Context, *gorm.DB, iam.AuditEvent) error { audited.Add(1); return nil }
			original, err := coordinator.accept(ctx, input, parent, "", input.ExpectedModules)
			if err != nil {
				t.Fatal(err)
			}

			retry := input
			if identity == "source_operation" {
				retry.RequestID = uuid.NewString()
			}
			retry.ParentExecutionID = uuid.NewString()
			if err := retry.Seal(); err != nil {
				t.Fatal(err)
			}
			parent.ExecutionID = retry.ParentExecutionID
			err = db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
				var id int64
				if err := conn.Raw("SELECT CONNECTION_ID()").Scan(&id).Error; err != nil {
					return err
				}
				competing := *coordinator
				competing.db = conn.Session(&gorm.Session{NewDB: true})
				done := make(chan error, 1)
				go func() {
					record, err := competing.accept(ctx, retry, parent, "", retry.ExpectedModules)
					if err == nil && (record.RequestID != original.RequestID || record.Protocol != original.Protocol) {
						err = errors.New("retry replaced the original acceptance")
					}
					done <- err
				}()
				// Wait for the other runtime connection to reach the INSERT before
				// committing the winner. Both identities must arbitrate in the DB.
				for {
					var inserting bool
					err := first.Raw("SELECT EXISTS(SELECT 1 FROM information_schema.processlist WHERE ID=? AND LOWER(INFO) LIKE '%insert into%cleanup_requests%')", id).Scan(&inserting).Error
					if err != nil {
						first.Rollback()
						<-done
						return err
					}
					if inserting {
						break
					}
					select {
					case err := <-done:
						if err == nil {
							return errors.New("retry finished before the first acceptance committed")
						}
						return err
					case <-ctx.Done():
						first.Rollback()
						<-done
						return ctx.Err()
					case <-time.After(10 * time.Millisecond):
					}
				}
				err := first.Commit().Error
				return errors.Join(err, <-done)
			})
			if err != nil {
				t.Fatal(err)
			}
			coordinator.db = db
			retry.RequestedBy++
			if err := retry.Seal(); err != nil {
				t.Fatal(err)
			}
			if _, err := coordinator.accept(ctx, retry, parent, "", retry.ExpectedModules); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("different actor: %v", err)
			}
			var total int64
			if err := db.Model(&execution.TaskExecution{}).Where("execution_id IN ? OR parent_execution_id IN ?", []string{input.ParentExecutionID, retry.ParentExecutionID}, []string{input.ParentExecutionID, retry.ParentExecutionID}).Count(&total).Error; err != nil {
				t.Fatal(err)
			}
			if total != 2 || issued.Load() != 1 || audited.Load() != 1 {
				t.Fatalf("executions=%d issued=%d audited=%d", total, issued.Load(), audited.Load())
			}
		})
	}
}
