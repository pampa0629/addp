package execution

import (
	"context"
	"fmt"
	"regexp"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const EventRetention = 30 * 24 * time.Hour
const MaxDailyAttemptEvents = 1000

// Event contains only bounded diagnostic facts. Identity comes from the locked execution.
type Event struct {
	ID          int64          `json:"id" gorm:"primaryKey;autoIncrement"`
	ExecutionID string         `json:"execution_id" gorm:"not null"`
	TenantID    int            `json:"-" gorm:"not null"`
	Module      string         `json:"-" gorm:"not null"`
	TaskType    string         `json:"-" gorm:"not null"`
	Attempt     int            `json:"attempt" gorm:"not null"`
	OccurredAt  time.Time      `json:"occurred_at" gorm:"not null"`
	Kind        string         `json:"kind" gorm:"not null"`
	StepID      string         `json:"step_id,omitempty"`
	Counters    models.JSONMap `json:"counters" gorm:"type:jsonb;not null"`
}

func (Event) TableName() string { return "common.execution_events" }

// EventInput deliberately has no arbitrary message, payload, time or tenant fields.
type EventInput struct {
	Kind     string
	StepID   string
	Counters map[string]int64
}

var eventStepID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
var eventCounterKeys = map[string]bool{
	"progress": true, "batch_index": true, "batch_records": true,
	"records_read": true, "records_written": true, "bytes_read": true, "bytes_written": true,
	"items_scanned": true, "catalog_nodes_scanned": true, "fields_scanned": true,
}

func validateEvent(input EventInput) error {
	switch input.Kind {
	case "started", "progress", "completed", "failed", "cancelled", "timeout":
	default:
		return fmt.Errorf("invalid execution event kind")
	}
	if input.StepID != "" && !eventStepID.MatchString(input.StepID) {
		return fmt.Errorf("invalid execution event step identity")
	}
	for key, value := range input.Counters {
		if !eventCounterKeys[key] || value < 0 {
			return fmt.Errorf("invalid execution event counter")
		}
	}
	return nil
}

// UpdateWithEvent commits diagnostic evidence with the corresponding progress update.
func UpdateWithEvent(ctx context.Context, db *gorm.DB, lease Lease, fields map[string]interface{}, input EventInput) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := AppendBoundedEvent(ctx, tx, lease, input); err != nil {
			return err
		}
		return UpdateWithLease(ctx, tx, lease, fields)
	})
}

func CompleteWithEvent(ctx context.Context, db *gorm.DB, lease Lease, status string, at time.Time, fields map[string]interface{}) error {
	kind := status
	if status == ExecutionStatusSuccess {
		kind = "completed"
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := AppendBoundedEvent(ctx, tx, lease, EventInput{Kind: kind}); err != nil {
			return err
		}
		return CompleteWithLease(ctx, tx, lease, status, at, fields)
	})
}

// AppendBoundedEvent serializes admission against completion, recovery and other writers.
// A stale owner cannot append, even when its old token is still known.
func AppendBoundedEvent(ctx context.Context, db *gorm.DB, lease Lease, input EventInput) error {
	if err := validateEvent(input); err != nil {
		return err
	}
	if db == nil || lease.TenantID <= 0 || lease.Attempt <= 0 || lease.Owner == "" || lease.Token == "" {
		return fmt.Errorf("execution event requires a valid bounded lease")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var item TaskExecution
		query := ownedActiveExecution(tx, lease, now).Where("execution_boundary = ? AND lease_owner = ?", ExecutionBoundaryBounded, lease.Owner)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&item).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("%w: execution event lease is no longer current", commonapi.ErrConflict)
			}
			return err
		}
		var count int64
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if err := tx.Model(&Event{}).Where("execution_id = ? AND attempt = ? AND occurred_at >= ?", item.ExecutionID, item.Attempt, dayStart).Count(&count).Error; err != nil {
			return err
		}
		if count >= MaxDailyAttemptEvents {
			return nil
		}
		event := Event{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Module: item.Module, TaskType: item.TaskType, Attempt: item.Attempt, OccurredAt: now, Kind: input.Kind, StepID: input.StepID, Counters: models.JSONMap{}}
		for key, value := range input.Counters {
			event.Counters[key] = value
		}
		if count == MaxDailyAttemptEvents-1 {
			event.Kind = "truncated"
			event.StepID = ""
			event.Counters = models.JSONMap{}
		}
		return tx.Create(&event).Error
	})
}

type EventPage struct {
	Items         []Event   `json:"items"`
	NextCursor    int64     `json:"next_cursor"`
	HasMore       bool      `json:"has_more"`
	RetainedAfter time.Time `json:"retained_after"`
}

// ListEvents first authorizes the execution using the same scope as details/counts.
func (r *TaskExecutionRepository) ListEvents(ctx context.Context, executionID string, tenantID int, after int64, limit int) (*EventPage, error) {
	if tenantID <= 0 || after < 0 || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid execution event query")
	}
	if _, err := r.GetByExecutionID(ctx, executionID, tenantID); err != nil {
		return nil, err
	}
	page := &EventPage{Items: []Event{}, RetainedAfter: time.Now().UTC().Add(-EventRetention)}
	err := r.db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Where("execution_id = ? AND tenant_id = ? AND id > ? AND occurred_at >= ?", executionID, tenantID, after, page.RetainedAfter).Order("id ASC").Limit(limit + 1).Find(&page.Items).Error
	if err != nil {
		return nil, err
	}
	page.HasMore = len(page.Items) > limit
	if page.HasMore {
		page.Items = page.Items[:limit]
	}
	if len(page.Items) > 0 {
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

type EventPruneResult struct {
	Deleted       int64     `json:"deleted"`
	ExpiredBefore time.Time `json:"expired_before"`
}

// PruneEvents is shared-storage maintenance; it never deletes task executions or audit.
func PruneEvents(ctx context.Context, db *gorm.DB, now time.Time, limit int) (*EventPruneResult, error) {
	if db == nil || now.IsZero() || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid execution event maintenance request")
	}
	result := &EventPruneResult{ExpiredBefore: now.UTC().Add(-EventRetention)}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []int64
		query := tx.Model(&Event{}).Where("occurred_at < ?", result.ExpiredBefore).Order("occurred_at ASC, id ASC").Limit(limit)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		deleted := tx.Where("id IN ? AND occurred_at < ?", ids, result.ExpiredBefore).Delete(&Event{})
		result.Deleted = deleted.RowsAffected
		return deleted.Error
	})
	return result, err
}
