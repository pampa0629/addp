package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrContentIndexIsolated = errors.New("content index outlet is isolated")
var ErrContentIndexDeliveryConflict = errors.New("content index delivery state conflict")

const (
	IndexDeliveryWrite      = "write"
	IndexDeliveryDelete     = "delete"
	IndexDeliveryPurge      = "purge"
	IndexDeliveryQueued     = "queued"
	IndexDeliverySubmitting = "submitting"
	IndexDeliverySubmitted  = "submitted"
	IndexDeliveryUnknown    = "unknown"
	IndexDeliverySucceeded  = "succeeded"
	IndexDeliveryFailed     = "failed"
	IndexDeliveryCanceled   = "canceled"
)

var unfinishedIndexDeliveries = []string{IndexDeliveryQueued, IndexDeliverySubmitting, IndexDeliverySubmitted, IndexDeliveryUnknown}

type ContentIndexDeliveryRepository struct {
	db    *gorm.DB
	index string
}

func NewContentIndexDeliveryRepository(db *gorm.DB, index string) *ContentIndexDeliveryRepository {
	return &ContentIndexDeliveryRepository{db: db, index: strings.TrimSpace(index)}
}

func (r *ContentIndexDeliveryRepository) lock(tx *gorm.DB) (*models.ContentIndexOutlet, error) {
	if r == nil || r.db == nil || r.index == "" || tx == nil {
		return nil, ErrContentIndexIsolated
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, errors.New("content index mutation requires transaction")
	}
	row := models.ContentIndexOutlet{IndexName: r.index, Isolated: true}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("index_name = ?", r.index).First(&row).Error
	return &row, err
}

// Open fences previous startup generations; it never clears pending deliveries.
func (r *ContentIndexDeliveryRepository) Open(ctx context.Context, endpointID string, configured bool) (string, error) {
	epoch := uuid.NewString()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := r.lock(tx); err != nil {
			return err
		}
		return tx.Model(&models.ContentIndexOutlet{}).Where("index_name = ?", r.index).Updates(map[string]any{
			"epoch": epoch, "endpoint_id": endpointID, "configured": configured, "isolated": true,
		}).Error
	})
	return epoch, err
}

func (r *ContentIndexDeliveryRepository) State(ctx context.Context, epoch string) (*models.ContentIndexOutlet, error) {
	if r == nil || r.db == nil || epoch == "" {
		return nil, ErrContentIndexIsolated
	}
	var state models.ContentIndexOutlet
	if err := r.db.WithContext(ctx).Where("index_name = ? AND epoch = ?", r.index, epoch).First(&state).Error; err != nil {
		return nil, ErrContentIndexIsolated
	}
	return &state, nil
}

func (r *ContentIndexDeliveryRepository) RequireActive(ctx context.Context, epoch string) error {
	if r == nil || r.db == nil || epoch == "" {
		return ErrContentIndexIsolated
	}
	var active int64
	if err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM manager.content_index_outlets o
 WHERE o.index_name = ? AND o.epoch = ? AND o.configured = TRUE AND o.isolated = FALSE
 AND NOT EXISTS (SELECT 1 FROM manager.content_index_deliveries d WHERE d.index_name = o.index_name AND d.status IN ('submitting','unknown'))`, r.index, epoch).Scan(&active).Error; err != nil {
		return err
	}
	if active != 1 {
		return ErrContentIndexIsolated
	}
	return nil
}

func (r *ContentIndexDeliveryRepository) Fence(ctx context.Context, epoch string) error {
	result := r.db.WithContext(ctx).Model(&models.ContentIndexOutlet{}).Where("index_name = ? AND epoch = ?", r.index, epoch).Update("isolated", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrContentIndexIsolated
	}
	return nil
}

// Register is called inside the same protection checkpoint transaction.
func (r *ContentIndexDeliveryRepository) Register(ctx context.Context, tx *gorm.DB, epoch string, op *models.ContentIndexDelivery) error {
	if tx == nil {
		return ErrContentIndexIsolated
	}
	state, err := r.lock(tx.WithContext(ctx))
	if err != nil {
		return err
	}
	if state.Epoch != epoch || !state.Configured || state.Isolated || op == nil || op.TenantID <= 0 || op.Kind != IndexDeliveryWrite {
		return ErrContentIndexIsolated
	}
	var pending int64
	if err := tx.Model(&models.ContentIndexDelivery{}).Where("index_name = ? AND status IN ? AND (kind IN ? OR status IN ? OR (tenant_id = ? AND document_id = ?))", r.index, unfinishedIndexDeliveries, []string{IndexDeliveryDelete, IndexDeliveryPurge}, []string{IndexDeliverySubmitting, IndexDeliveryUnknown}, op.TenantID, op.DocumentID).Count(&pending).Error; err != nil {
		return err
	}
	if pending != 0 {
		return ErrContentIndexIsolated
	}
	op.ID, op.IndexName, op.EndpointID, op.Status = uuid.NewString(), r.index, state.EndpointID, IndexDeliverySubmitting
	op.TaskCorrelation = op.ID
	if err := tx.Create(op).Error; err != nil {
		return err
	}
	return nil
}

// QueueProtectionPurges performs database work only, under the caller's lock.
func (r *ContentIndexDeliveryRepository) QueueProtectionPurges(ctx context.Context, tx *gorm.DB, tenantID int64, fingerprints []string) error {
	if tx == nil {
		return ErrContentIndexIsolated
	}
	state, err := r.lock(tx.WithContext(ctx))
	if err != nil {
		return err
	}
	if tenantID <= 0 {
		return errors.New("invalid index purge tenant")
	}
	for _, fingerprint := range fingerprints {
		if strings.TrimSpace(fingerprint) == "" {
			return errors.New("invalid index purge fingerprint")
		}
		var pending int64
		if err := tx.Model(&models.ContentIndexDelivery{}).Where("index_name = ? AND tenant_id = ? AND document_id = ? AND kind = ? AND status IN ?", r.index, tenantID, fingerprint, IndexDeliveryPurge, unfinishedIndexDeliveries).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			continue
		}
		op := models.ContentIndexDelivery{ID: uuid.NewString(), IndexName: r.index, TenantID: tenantID, DocumentID: fingerprint, Kind: IndexDeliveryPurge, Status: IndexDeliveryQueued,
			Filter: fmt.Sprintf("tenant_id = %d AND document_id = '%s'", tenantID, strings.ReplaceAll(fingerprint, "'", "\\'"))}
		if err := tx.Create(&op).Error; err != nil {
			return err
		}
	}
	return tx.Model(state).Update("isolated", true).Error
}

func (r *ContentIndexDeliveryRepository) QueueDelete(ctx context.Context, epoch string, tenantID int64, filter string) (*models.ContentIndexDelivery, error) {
	op := &models.ContentIndexDelivery{ID: uuid.NewString(), IndexName: r.index, TenantID: tenantID, Kind: IndexDeliveryDelete, Filter: filter, Status: IndexDeliveryQueued}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := r.lock(tx)
		if err != nil {
			return err
		}
		if state.Epoch != epoch || !state.Configured || tenantID <= 0 || filter == "" {
			return ErrContentIndexIsolated
		}
		if err := tx.Create(op).Error; err != nil {
			return err
		}
		return tx.Model(state).Update("isolated", true).Error
	})
	return op, err
}

// Unknown submissions are only discoverable with pre-submission correlation.
// Legacy no-UID records remain fenced, never retroactively marked or resent.
func (r *ContentIndexDeliveryRepository) Recoverable(ctx context.Context) ([]models.ContentIndexDelivery, error) {
	var ops []models.ContentIndexDelivery
	err := r.db.WithContext(ctx).Where("index_name = ? AND (status IN ? OR (status IN ? AND task_correlation <> ''))", r.index, []string{IndexDeliverySubmitted, IndexDeliveryQueued}, []string{IndexDeliverySubmitting, IndexDeliveryUnknown}).Order("created_at, id").Limit(100).Find(&ops).Error
	return ops, err
}

func (r *ContentIndexDeliveryRepository) ClaimMaintenance(ctx context.Context, epoch string, op *models.ContentIndexDelivery) (bool, error) {
	claimed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := r.lock(tx)
		if err != nil {
			return err
		}
		if state.Epoch != epoch || !state.Configured {
			return ErrContentIndexIsolated
		}
		var pending int64
		if err := tx.Model(&models.ContentIndexDelivery{}).Where("index_name = ? AND ((kind = ? AND status IN ?) OR (kind <> ? AND status IN ?))", r.index, IndexDeliveryWrite, unfinishedIndexDeliveries, IndexDeliveryWrite, []string{IndexDeliverySubmitting, IndexDeliverySubmitted, IndexDeliveryUnknown}).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return nil
		}
		result := tx.Model(&models.ContentIndexDelivery{}).Where("id = ? AND index_name = ? AND kind IN ? AND status = ?", op.ID, r.index, []string{IndexDeliveryDelete, IndexDeliveryPurge}, IndexDeliveryQueued).Updates(map[string]any{"status": IndexDeliverySubmitting, "endpoint_id": state.EndpointID, "task_correlation": op.ID})
		if result.Error != nil {
			return result.Error
		}
		claimed = result.RowsAffected == 1
		if claimed {
			op.Status, op.EndpointID = IndexDeliverySubmitting, state.EndpointID
			op.TaskCorrelation = op.ID
		}
		return nil
	})
	return claimed, err
}

func (r *ContentIndexDeliveryRepository) Receipt(ctx context.Context, op *models.ContentIndexDelivery, uid int64, enqueuedAt time.Time) error {
	if op == nil || uid < 0 || enqueuedAt.IsZero() {
		return ErrContentIndexDeliveryConflict
	}
	result := r.db.WithContext(ctx).Model(&models.ContentIndexDelivery{}).Where("id = ? AND index_name = ? AND tenant_id = ? AND endpoint_id = ? AND task_correlation = ? AND kind = ? AND status IN ? AND task_uid IS NULL", op.ID, r.index, op.TenantID, op.EndpointID, op.TaskCorrelation, op.Kind, []string{IndexDeliverySubmitting, IndexDeliveryUnknown}).Updates(map[string]any{"status": IndexDeliverySubmitted, "task_uid": uid, "task_enqueued_at": enqueuedAt.UTC().Format(time.RFC3339Nano)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrContentIndexDeliveryConflict
	}
	op.Status, op.TaskUID = IndexDeliverySubmitted, &uid
	op.TaskEnqueuedAt = enqueuedAt.UTC().Format(time.RFC3339Nano)
	return nil
}

func (r *ContentIndexDeliveryRepository) Get(ctx context.Context, tenantID int64, id string) (*models.ContentIndexDelivery, error) {
	var op models.ContentIndexDelivery
	err := r.db.WithContext(ctx).Where("index_name = ? AND tenant_id = ? AND id = ?", r.index, tenantID, id).First(&op).Error
	return &op, err
}

func (r *ContentIndexDeliveryRepository) Unknown(ctx context.Context, op *models.ContentIndexDelivery) error {
	return r.db.WithContext(ctx).Model(&models.ContentIndexDelivery{}).Where("id = ? AND index_name = ? AND tenant_id = ? AND status = ? AND task_uid IS NULL", op.ID, r.index, op.TenantID, IndexDeliverySubmitting).Update("status", IndexDeliveryUnknown).Error
}

// Finish records proven terminal results. Failed maintenance is retried as a
// fresh delivery, retaining the previous terminal task as evidence.
func (r *ContentIndexDeliveryRepository) Finish(ctx context.Context, op *models.ContentIndexDelivery, status string) error {
	if op == nil || op.TaskUID == nil || (status != IndexDeliverySucceeded && status != IndexDeliveryFailed && status != IndexDeliveryCanceled) {
		return ErrContentIndexDeliveryConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := r.lock(tx); err != nil {
			return err
		}
		var actual models.ContentIndexDelivery
		if err := tx.Where("id = ? AND index_name = ? AND tenant_id = ?", op.ID, r.index, op.TenantID).First(&actual).Error; err != nil {
			return err
		}
		if actual.Kind != op.Kind || actual.Filter != op.Filter || actual.EndpointID != op.EndpointID || actual.TaskEnqueuedAt != op.TaskEnqueuedAt || actual.TaskCorrelation != op.TaskCorrelation {
			return ErrContentIndexDeliveryConflict
		}
		result := tx.Model(&models.ContentIndexDelivery{}).Where("id = ? AND index_name = ? AND tenant_id = ? AND status = ? AND task_uid = ?", op.ID, r.index, op.TenantID, IndexDeliverySubmitted, *op.TaskUID).Update("status", status)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrContentIndexDeliveryConflict
		}
		if op.Kind != IndexDeliveryWrite && status != IndexDeliverySucceeded {
			retry := models.ContentIndexDelivery{ID: uuid.NewString(), IndexName: r.index, TenantID: op.TenantID, DocumentID: op.DocumentID, Kind: op.Kind, Filter: op.Filter, Status: IndexDeliveryQueued}
			return tx.Create(&retry).Error
		}
		return nil
	})
}

func (r *ContentIndexDeliveryRepository) ActivateIfSettled(ctx context.Context, epoch string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := r.lock(tx)
		if err != nil {
			return err
		}
		if state.Epoch != epoch || !state.Configured {
			return ErrContentIndexIsolated
		}
		var pending int64
		if err := tx.Model(&models.ContentIndexDelivery{}).Where("index_name = ? AND status IN ?", r.index, unfinishedIndexDeliveries).Count(&pending).Error; err != nil {
			return err
		}
		if pending != 0 {
			return ErrContentIndexIsolated
		}
		return tx.Model(state).Updates(map[string]any{"isolated": false, "updated_at": time.Now().UTC()}).Error
	})
}

// RequeueUnreadPurge is only called when the preliminary read failed, before
// any mutation request. Uncertain submissions must use receipt recovery instead.
func (r *ContentIndexDeliveryRepository) RequeueUnreadPurge(ctx context.Context, op *models.ContentIndexDelivery) error {
	result := r.db.WithContext(ctx).Model(&models.ContentIndexDelivery{}).
		Where("id = ? AND index_name = ? AND kind = ? AND status = ? AND task_uid IS NULL", op.ID, r.index, IndexDeliveryPurge, IndexDeliverySubmitting).
		Updates(map[string]any{"status": IndexDeliveryQueued, "task_correlation": "", "endpoint_id": ""})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrContentIndexDeliveryConflict
	}
	return nil
}
