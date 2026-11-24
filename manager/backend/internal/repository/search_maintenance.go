package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// SearchMaintenance keeps the receipt of an offline index operation across process exits.
// The outlet's existing exclusive hold prevents runtime recovery from reopening it.
type SearchMaintenance struct {
	ID             string
	IndexName      string
	EndpointID     string
	Action         string
	Step           int
	Phase          string
	PreviousUID    int64
	TaskUID        *int64
	TaskEnqueuedAt string
	Searchable     string
	DocumentCount  int64
}

func (SearchMaintenance) TableName() string { return "manager.search_maintenance" }

// PrepareSearchMaintenance runs on the deployment connection only.
func PrepareSearchMaintenance(db *gorm.DB) error {
	return db.Exec(`CREATE TABLE IF NOT EXISTS manager.search_maintenance (
        id varchar(36) PRIMARY KEY, index_name varchar(128) NOT NULL, endpoint_id varchar(64) NOT NULL,
        action varchar(12) NOT NULL, step integer NOT NULL, phase varchar(16) NOT NULL,
        previous_uid bigint NOT NULL, task_uid bigint, task_enqueued_at varchar(40) NOT NULL,
        searchable text NOT NULL, document_count bigint NOT NULL
    )`).Error
}

func (r *ContentIndexDeliveryRepository) BeginSearchMaintenance(ctx context.Context, id, endpoint, action string) (*SearchMaintenance, error) {
	var op SearchMaintenance
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		outlet, err := r.lock(tx)
		if err != nil {
			return err
		}
		err = tx.Where("id=?", id).Take(&op).Error
		if err == nil {
			if op.IndexName != r.index || op.EndpointID != endpoint || op.Action != action {
				return ErrContentIndexDeliveryConflict
			}
			if op.Phase != "done" && (outlet.CleanupHoldID != "maintenance/"+id || outlet.EndpointID != endpoint || !outlet.Isolated) {
				return ErrContentIndexIsolated
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !outlet.Configured || outlet.Isolated || outlet.CleanupHoldID != "" || outlet.EndpointID != endpoint {
			return ErrContentIndexIsolated
		}
		var n int64
		if err := tx.Model(&models.ContentIndexDelivery{}).Where("index_name=? AND status IN ?", r.index, unfinishedIndexDeliveries).Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			return ErrContentIndexIsolated
		}
		op = SearchMaintenance{ID: id, IndexName: r.index, EndpointID: endpoint, Action: action, Phase: "prepare", PreviousUID: -1}
		if err := tx.Create(&op).Error; err != nil {
			return err
		}
		return tx.Model(outlet).Updates(map[string]any{"isolated": true, "cleanup_hold_id": "maintenance/" + id, "search_revision": gorm.Expr("search_revision + 1")}).Error
	})
	return &op, err
}

// AdvanceSearchMaintenance compares the stored phase before recording a receipt.
// This also elects a single submitter without holding a transaction over HTTP.
func (r *ContentIndexDeliveryRepository) AdvanceSearchMaintenance(ctx context.Context, op *SearchMaintenance, updates map[string]any) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := r.lock(tx)
		if err != nil {
			return err
		}
		if state.CleanupHoldID != "maintenance/"+op.ID || state.EndpointID != op.EndpointID || !state.Isolated {
			return ErrContentIndexIsolated
		}
		result := tx.Model(&SearchMaintenance{}).Where("id=? AND index_name=? AND step=? AND phase=?", op.ID, r.index, op.Step, op.Phase).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrContentIndexDeliveryConflict
		}
		if updates["phase"] == "done" {
			// A protection purge may have arrived during the maintenance window.
			// The normal reconciliation loop opens the outlet only once all deliveries settle.
			var pending int64
			if err := tx.Model(&models.ContentIndexDelivery{}).Where("index_name=? AND status IN ?", r.index, unfinishedIndexDeliveries).Count(&pending).Error; err != nil {
				return err
			}
			return tx.Model(state).Updates(map[string]any{"cleanup_hold_id": "", "isolated": pending != 0 || !state.Configured}).Error
		}
		return nil
	})
}

func (r *ContentIndexDeliveryRepository) CheckSearchOrdinals(ctx context.Context, assigned map[string]int64) error {
	ids := make([]string, 0, len(assigned))
	for id := range assigned {
		ids = append(ids, id)
	}
	var rows []models.SearchDocumentOrdinal
	if err := r.db.WithContext(ctx).Where("index_name=? AND document_id IN ?", r.index, ids).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != len(assigned) {
		return fmt.Errorf("missing Manager document ordinal")
	}
	for _, row := range rows {
		if row.SearchOrdinal != assigned[row.DocumentID] {
			return fmt.Errorf("Manager document ordinal mismatch")
		}
	}
	return nil
}
