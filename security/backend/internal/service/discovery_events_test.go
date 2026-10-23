package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	execution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDiscoveryExecutionEvents(t *testing.T) {
	discoveryExecutionEventCases(t, openSecurityTestDB)
}

func TestDiscoveryExecutionEventsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SECURITY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SECURITY_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	if err := tx.Exec("DROP SCHEMA IF EXISTS security CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	if err := execution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Where("module = ?", execution.ModuleSecurity).Delete(&execution.TaskExecution{}).Error; err != nil {
		t.Fatal(err)
	}
	discoveryExecutionEventCases(t, func(t *testing.T) *gorm.DB {
		name := "events_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if err := tx.SavePoint(name).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := tx.RollbackTo(name).Error; err != nil {
				t.Error(err)
			}
		})
		return tx
	})
}

func discoveryExecutionEventCases(t *testing.T, open func(*testing.T) *gorm.DB) {
	t.Run("empty_queue_has_no_events", func(t *testing.T) {
		db := open(t)
		item, lease, err := NewDiscoveryService(db, nil).ClaimNext(t.Context(), "events-worker", time.Now().UTC(), time.Minute)
		if err != nil || item != nil || lease != nil {
			t.Fatal("empty queue claimed work", err)
		}
		var count int64
		if err := db.Model(&execution.Event{}).Where("module = ?", execution.ModuleSecurity).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("empty queue wrote an event", count, err)
		}
	})
	t.Run("claim_failure_rolls_back_ownership", func(t *testing.T) {
		db := open(t)
		item := seedDiscoveryEventExecution(t, db, 1)
		rejectDiscoveryEvents(t, db)
		claimed, lease, err := NewDiscoveryService(db, nil).ClaimNext(t.Context(), "events-worker", time.Now().UTC(), time.Minute)
		if err == nil || claimed != nil || lease != nil {
			t.Fatal("claim ignored failed event storage")
		}
		stored := storedDiscoveryEventExecution(t, db, item.ExecutionID)
		if stored.Status != execution.ExecutionStatusPending || stored.Attempt != 0 || stored.LeaseToken != nil || stored.StartedAt != nil {
			t.Fatal("failed started event committed a claim")
		}
		assertDiscoveryEventKinds(t, db, item.ExecutionID, nil, nil)
	})
	t.Run("failure_is_closed_unique_and_renewal_is_silent", func(t *testing.T) {
		db := open(t)
		seedDiscoveryEventExecution(t, db, 1)
		svc := NewDiscoveryService(db, nil)
		item, lease := claimDiscoveryEventExecution(t, svc)
		if err := svc.Renew(t.Context(), lease, time.Now().UTC().Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		assertDiscoveryEventKinds(t, db, item.ExecutionID, []string{"started"}, []int{1})
		if err := svc.Execute(t.Context(), item, lease); err == nil {
			t.Fatal("missing facts unexpectedly succeeded")
		}
		if storedDiscoveryEventExecution(t, db, item.ExecutionID).Status != execution.ExecutionStatusFailed {
			t.Fatal("failure state was not saved")
		}
		assertDiscoveryEventKinds(t, db, item.ExecutionID, []string{"started", "failed"}, []int{1, 1})
		if err := svc.completeFailure(t.Context(), lease, time.Now().UTC(), "duplicate_failure"); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatal("terminal execution accepted a duplicate", err)
		}
		assertDiscoveryEventKinds(t, db, item.ExecutionID, []string{"started", "failed"}, []int{1, 1})
	})
	t.Run("mismatched_lease_cannot_append_or_complete", func(t *testing.T) {
		db := open(t)
		seedDiscoveryEventExecution(t, db, 1)
		svc := NewDiscoveryService(db, nil)
		item, lease := claimDiscoveryEventExecution(t, svc)
		for _, mutate := range []func(*execution.Lease){
			func(l *execution.Lease) { l.TenantID++ },
			func(l *execution.Lease) { l.ExecutionID = uuid.NewString() },
			func(l *execution.Lease) { l.Attempt++ },
			func(l *execution.Lease) { l.Owner = "another-worker" },
			func(l *execution.Lease) { l.Token = uuid.NewString() },
		} {
			wrong := lease
			mutate(&wrong)
			if err := svc.completeFailure(t.Context(), wrong, time.Now().UTC(), "stale"); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatal("mismatched lease was not rejected", err)
			}
		}
		if storedDiscoveryEventExecution(t, db, item.ExecutionID).Status != execution.ExecutionStatusRunning {
			t.Fatal("mismatched lease changed execution")
		}
		assertDiscoveryEventKinds(t, db, item.ExecutionID, []string{"started"}, []int{1})
	})
	t.Run("retry_preserves_policy_and_records_real_attempts", func(t *testing.T) {
		db := open(t)
		seedDiscoveryEventExecution(t, db, 2)
		svc := NewDiscoveryService(db, nil)
		first, firstLease := claimDiscoveryEventExecution(t, svc)
		expireDiscoveryEventLease(t, db, first.ExecutionID)
		if err := svc.completeFailure(t.Context(), firstLease, time.Now().UTC(), "expired_worker"); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatal("expired worker completed", err)
		}
		if count, err := svc.RecoverExpired(t.Context(), time.Now().UTC(), 100); err != nil || count != 1 {
			t.Fatal("retry failed", count, err)
		}
		if row := storedDiscoveryEventExecution(t, db, first.ExecutionID); row.Status != execution.ExecutionStatusPending || row.Attempt != 1 || row.MaxAttempts != 2 {
			t.Fatal("retry policy changed")
		}
		assertDiscoveryEventKinds(t, db, first.ExecutionID, []string{"started"}, []int{1})
		second, _ := claimDiscoveryEventExecution(t, svc)
		if second.Attempt != 2 {
			t.Fatal("retry did not claim a new attempt")
		}
		if err := svc.completeFailure(t.Context(), firstLease, time.Now().UTC(), "old_attempt"); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatal("old attempt wrote after new claim", err)
		}
		expireDiscoveryEventLease(t, db, second.ExecutionID)
		if count, err := svc.RecoverExpired(t.Context(), time.Now().UTC(), 100); err != nil || count != 1 {
			t.Fatal("exhausted recovery failed", count, err)
		}
		if row := storedDiscoveryEventExecution(t, db, second.ExecutionID); row.Status != execution.ExecutionStatusFailed || row.ErrorDetails["error_code"] != "worker_lease_expired" {
			t.Fatal("exhausted recovery did not retain its failure code")
		}
		assertDiscoveryEventKinds(t, db, second.ExecutionID, []string{"started", "started", "failed"}, []int{1, 2, 2})
		if count, err := svc.RecoverExpired(t.Context(), time.Now().UTC(), 100); err != nil || count != 0 {
			t.Fatal("terminal execution recovered twice", count, err)
		}
	})
	t.Run("recovery_event_failure_rolls_back_terminal", func(t *testing.T) {
		db := open(t)
		seedDiscoveryEventExecution(t, db, 1)
		svc := NewDiscoveryService(db, nil)
		item, _ := claimDiscoveryEventExecution(t, svc)
		expireDiscoveryEventLease(t, db, item.ExecutionID)
		rejectDiscoveryEvents(t, db)
		if _, err := svc.RecoverExpired(t.Context(), time.Now().UTC(), 100); err == nil {
			t.Fatal("recovery ignored failed event storage")
		}
		if row := storedDiscoveryEventExecution(t, db, item.ExecutionID); row.Status != execution.ExecutionStatusRunning || row.CompletedAt != nil {
			t.Fatal("failed recovery event committed terminal state")
		}
		assertDiscoveryEventKinds(t, db, item.ExecutionID, []string{"started"}, []int{1})
	})
	for _, failEvents := range []bool{false, true} {
		name := "successful_findings_and_event_commit_together"
		if failEvents {
			name = "event_failure_rolls_back_findings_source_and_projection"
		}
		t.Run(name, func(t *testing.T) {
			db := open(t)
			svc, enrollmentID := prepareDiscoveryEventFinding(t, db)
			item, lease := claimDiscoveryEventExecution(t, svc)
			var before models.ProtectionEnrollment
			if err := db.First(&before, "id = ?", enrollmentID).Error; err != nil {
				t.Fatal(err)
			}
			var beforeChanges int64
			if err := db.Model(&models.ProtectionProjectionChange{}).Count(&beforeChanges).Error; err != nil {
				t.Fatal(err)
			}
			if failEvents {
				rejectDiscoveryEvents(t, db)
			}
			err := svc.Execute(t.Context(), item, lease)
			var after models.ProtectionEnrollment
			if loadErr := db.First(&after, "id = ?", enrollmentID).Error; loadErr != nil {
				t.Fatal(loadErr)
			}
			var findings, changes int64
			if countErr := db.Model(&models.SensitiveFinding{}).Count(&findings).Error; countErr != nil {
				t.Fatal(countErr)
			}
			if countErr := db.Model(&models.ProtectionProjectionChange{}).Count(&changes).Error; countErr != nil {
				t.Fatal(countErr)
			}
			stored := storedDiscoveryEventExecution(t, db, item.ExecutionID)
			if failEvents {
				if err == nil || findings != 0 || changes != beforeChanges || after.LatestDiscoveryExecutionID != before.LatestDiscoveryExecutionID || after.LatestSourceSnapshotHash != before.LatestSourceSnapshotHash || after.Version != before.Version || stored.Status != execution.ExecutionStatusRunning || stored.CompletedAt != nil {
					t.Fatal("event failure left partial discovery facts", err)
				}
				assertDiscoveryEventKinds(t, db, item.ExecutionID, []string{"started"}, []int{1})
			} else {
				if err != nil || findings != 1 || after.LatestDiscoveryExecutionID != item.ExecutionID || changes <= beforeChanges || stored.Status != execution.ExecutionStatusSuccess {
					t.Fatal("discovery and completion did not commit together", err)
				}
				assertDiscoveryEventKinds(t, db, item.ExecutionID, []string{"started", "completed"}, []int{1, 1})
			}
		})
	}
}

func seedDiscoveryEventExecution(t *testing.T, db *gorm.DB, attempts int) execution.TaskExecution {
	t.Helper()
	source := uuid.NewString()
	item := execution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: execution.ModuleSecurity,
		TaskType: execution.TaskTypeSensitiveDataDiscovery, Source: execution.ModuleSecurity, SourceTaskID: &source,
		Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded,
		TriggerType: execution.TriggerTypeEvent, MaxAttempts: attempts,
		ExecutionConfig: commonmodels.JSONMap{"private": "private-event-sentinel"}, Metadata: commonmodels.JSONMap{"private": "private-event-sentinel"}}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func claimDiscoveryEventExecution(t *testing.T, svc *DiscoveryService) (*execution.TaskExecution, execution.Lease) {
	t.Helper()
	item, lease, err := svc.ClaimNext(t.Context(), "private-event-sentinel-worker", time.Now().UTC(), time.Minute)
	if err != nil || item == nil || lease == nil {
		t.Fatal("claim failed", err)
	}
	return item, *lease
}

func storedDiscoveryEventExecution(t *testing.T, db *gorm.DB, id string) execution.TaskExecution {
	t.Helper()
	var item execution.TaskExecution
	if err := db.First(&item, "execution_id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func expireDiscoveryEventLease(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id = ?", id).Update("lease_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
}

func rejectDiscoveryEvents(t *testing.T, db *gorm.DB) {
	t.Helper()
	name := "reject_discovery_event_" + uuid.NewString()
	if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == (execution.Event{}).TableName() {
			tx.AddError(errors.New("event storage unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(name) })
}

func assertDiscoveryEventKinds(t *testing.T, db *gorm.DB, id string, kinds []string, attempts []int) {
	t.Helper()
	var events []execution.Event
	if err := db.Where("execution_id = ?", id).Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != len(kinds) {
		t.Fatalf("event count=%d want=%d", len(events), len(kinds))
	}
	for index, event := range events {
		if event.Kind != kinds[index] || event.Attempt != attempts[index] || event.TenantID != 7 || event.Module != execution.ModuleSecurity || event.TaskType != execution.TaskTypeSensitiveDataDiscovery || event.StepID != "" || len(event.Counters) != 0 || event.OccurredAt.IsZero() {
			t.Fatal("event has incorrect identity or non-lifecycle content")
		}
		data, err := json.Marshal(event)
		if err != nil || strings.Contains(string(data), "private-event-sentinel") {
			t.Fatal("event leaked owner data", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 6 {
			t.Fatal("event public fields are not closed", err)
		}
		for _, key := range []string{"id", "execution_id", "attempt", "occurred_at", "kind", "counters"} {
			if _, ok := fields[key]; !ok {
				t.Fatal("event public field missing", key)
			}
		}
	}
}

func prepareDiscoveryEventFinding(t *testing.T, db *gorm.DB) (*DiscoveryService, string) {
	t.Helper()
	definitions := newTestDefinitionService(db)
	classification, err := definitions.CreateClassification(models.DefinitionRequest{Code: "event_personal", Name: "Personal"}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	grade, err := definitions.CreateGrade(models.DefinitionRequest{Code: "event_l3", Name: "L3", RiskOrder: 3}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	dataType, err := definitions.createTypeWithoutBaseline(models.SensitiveDataTypeRequest{Code: "event_phone", Name: "Phone", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateDetector(models.DetectorRequest{CapabilityKey: models.FindingDetectorPhoneMetadataV2, SensitiveDataTypeID: dataType.ID, ConfidenceThreshold: 0.9}, 7, 11); err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}, AllowedAlgorithms: []string{dataprotection.AlgorithmKeepPrefixSuffixV2}, InvalidValueEffect: dataprotection.EffectSuppress}, 7, 11); err != nil {
		t.Fatal(err)
	}
	enrollments := NewEnrollmentService(db)
	created, err := enrollments.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range allRequiredProtectionOwners() {
		changes, err := enrollments.ListChanges(t.Context(), 7, owner, "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if err := enrollments.Acknowledge(t.Context(), 7, owner, changes.NextCursor); err != nil {
			t.Fatal(err)
		}
	}
	fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString}}
	hash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	facts := &dataprotection.DataItemSecurityFacts{SchemaVersion: dataprotection.DataItemSecurityFactsSchemaV1,
		ItemFingerprint: created.Target.ResourceIdentity, ItemType: "collection", Fields: fields, SourceSnapshotHash: hash, ObservedAt: time.Now().UTC()}
	return NewDiscoveryService(db, func(uint) SecurityFactsReader { return staticSecurityFactsReader{facts: facts} }), created.ID
}
