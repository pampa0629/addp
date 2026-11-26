package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	commonclient "github.com/addp/common/client"
	"time"

	"github.com/addp/manager/internal/config"
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

// The logical index owns document ordinals and deletion fences. A new physical
// endpoint can serve that index; adopting a different name requires migrating
// those facts as well as the external documents.
type searchBinding struct {
	Singleton int `gorm:"primaryKey;autoIncrement:false"`
	IndexName string
}

func (searchBinding) TableName() string { return "manager.content_search_binding" }

func DeployedSearchIndex(ctx context.Context, db *gorm.DB) (string, error) {
	var binding searchBinding
	err := db.WithContext(ctx).Where("singleton = 1").Take(&binding).Error
	return binding.IndexName, err
}

func confirmDeployedSearchTarget(ctx context.Context, db *gorm.DB, index, endpoint string, reconcile func(context.Context, *gorm.DB, *ContentIndexDeliveryRepository) error) error {
	if index == "" || len(index) > 128 {
		return errors.New("invalid Manager content index name")
	}
	id := config.SearchEndpointID(endpoint)
	enabled := strings.TrimSpace(endpoint) != ""
	repo := NewContentIndexDeliveryRepository(db, index)
	current, err := DeployedSearchIndex(ctx, db)
	if err == nil {
		if current != index {
			return fmt.Errorf("Manager content index %q owns the stored ordinals and deletion fences; migrate them before renaming it", current)
		}
		// An unchanged release is also supported on a read-only connection.
		if _, err = repo.JoinTarget(ctx, id, enabled); err == nil {
			return nil
		}
		if !errors.Is(err, ErrContentIndexIsolated) {
			return err
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		binding := searchBinding{Singleton: 1, IndexName: index}
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&binding)
		if insert.Error != nil {
			return insert.Error
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("singleton=1").Take(&binding).Error; err != nil {
			return err
		}
		if binding.IndexName != index {
			return errors.New("Manager content index binding changed during deployment")
		}
		target := NewContentIndexDeliveryRepository(tx, index)
		if insert.RowsAffected == 1 {
			// Older installations had only the per-index rows. Do not silently
			// adopt a fresh name while an earlier deployment still has a ledger.
			var other int64
			if err := tx.Model(&models.ContentIndexOutlet{}).Where("index_name <> ? AND epoch <> ''", index).Count(&other).Error; err != nil {
				return err
			}
			if other != 0 {
				return errors.New("existing Manager search outlets differ from the deployment binding; reconcile their ledgers before adoption")
			}
		} else {
			if _, err := target.JoinTarget(ctx, id, enabled); err == nil {
				return nil
			} else if !errors.Is(err, ErrContentIndexIsolated) {
				return err
			}
		}
		if _, err := target.ConfirmTarget(ctx, id, enabled); err != nil {
			return err
		}
		return reconcile(ctx, tx, target)
	})
}

// ConfirmTarget is a deployment operation. Repeating a release must not
// replace a live epoch or change an in-progress isolation decision.
func (r *ContentIndexDeliveryRepository) ConfirmTarget(ctx context.Context, endpointID string, configured bool) (string, error) {
	var epoch string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := r.lock(tx)
		if err != nil {
			return err
		}
		if row.Epoch != "" && row.Configured == configured && (!configured || row.EndpointID == endpointID) {
			epoch = row.Epoch
			return nil
		}
		if row.CleanupHoldID != "" {
			return ErrContentIndexIsolated
		}
		if configured && row.Epoch != "" {
			// Unsent protection purges have no external effect yet. A receipt or
			// uncertain submission must be reconciled against its original target.
			var pending int64
			if err := tx.Model(&models.ContentIndexDelivery{}).
				Where("index_name = ? AND status IN ? AND NOT (kind = ? AND status = ? AND endpoint_id = '')", r.index, unfinishedIndexDeliveries, IndexDeliveryPurge, IndexDeliveryQueued).
				Count(&pending).Error; err != nil {
				return err
			}
			if pending != 0 && row.EndpointID != endpointID {
				return ErrContentIndexIsolated
			}
		}
		epoch = uuid.NewString()
		// Keep the last physical target on disable, so a return to that target
		// can drain its outstanding receipts without admitting a different one.
		if !configured {
			endpointID = row.EndpointID
		}
		return tx.Model(row).Updates(map[string]any{
			"epoch": epoch, "endpoint_id": endpointID, "configured": configured, "isolated": true,
		}).Error
	})
	return epoch, err
}

func (r *ContentIndexDeliveryRepository) JoinConfiguredIndex(ctx context.Context, index, endpointID string, configured bool) (string, error) {
	if index != r.index {
		return "", ErrContentIndexIsolated
	}
	return r.JoinTarget(ctx, endpointID, configured)
}

// JoinTarget performs no writes. A replica must keep its own epoch to be fenced
// by a later deployment, even when the deployment returns to the same address.
func (r *ContentIndexDeliveryRepository) JoinTarget(ctx context.Context, endpointID string, configured bool) (string, error) {
	var row models.ContentIndexOutlet
	err := r.db.WithContext(ctx).Where("index_name = ?", r.index).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrContentIndexIsolated
	}
	if err != nil {
		return "", err
	}
	if row.Epoch == "" || configured != row.Configured || (configured && row.EndpointID != endpointID) {
		return "", ErrContentIndexIsolated
	}
	return row.Epoch, nil
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
 WHERE o.index_name = ? AND o.epoch = ? AND o.configured = TRUE AND o.isolated = FALSE AND o.cleanup_hold_id = ''
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
	if state.Epoch != epoch || !state.Configured || state.Isolated || state.CleanupHoldID != "" || op == nil || op.TenantID <= 0 || op.Kind != IndexDeliveryWrite {
		return ErrContentIndexIsolated
	}
	if op.DocumentID != "" || len(op.DocumentIDs) == 0 || len(op.EngineIDs) == 0 {
		return ErrContentIndexDeliveryConflict
	}
	for _, engineID := range op.EngineIDs {
		if engineID == 0 {
			return ErrContentIndexDeliveryConflict
		}
		var blocked int64
		if err := tx.Model(&models.CleanupIndexTarget{}).Where("index_name=? AND tenant_id=? AND engine_id=?", r.index, op.TenantID, engineID).Count(&blocked).Error; err != nil {
			return err
		}
		if blocked != 0 {
			return ErrContentIndexIsolated
		}
	}
	members := make(map[string]bool, len(op.DocumentIDs))
	for _, id := range op.DocumentIDs {
		if strings.TrimSpace(id) == "" || members[id] {
			return ErrContentIndexDeliveryConflict
		}
		members[id] = true
	}
	var pending int64
	if err := tx.Model(&models.ContentIndexDelivery{}).Where("index_name = ? AND status IN ? AND (kind IN ? OR status IN ?)", r.index, unfinishedIndexDeliveries, []string{IndexDeliveryDelete, IndexDeliveryPurge}, []string{IndexDeliverySubmitting, IndexDeliveryUnknown}).Count(&pending).Error; err != nil {
		return err
	}
	if pending != 0 {
		return ErrContentIndexIsolated
	}
	var writes []models.ContentIndexDelivery
	if err := tx.Where("index_name = ? AND tenant_id = ? AND kind = ? AND status = ?", r.index, op.TenantID, IndexDeliveryWrite, IndexDeliverySubmitted).Find(&writes).Error; err != nil {
		return err
	}
	for _, write := range writes {
		if len(write.DocumentIDs) == 0 {
			return ErrContentIndexIsolated
		}
		for _, id := range write.DocumentIDs {
			if members[id] {
				return ErrContentIndexIsolated
			}
		}
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

func (r *ContentIndexDeliveryRepository) QueueDelete(ctx context.Context, epoch string, tenantID int64, filter string, scope commonclient.ManagerContentDeleteScope) (*models.ContentIndexDelivery, error) {
	op := &models.ContentIndexDelivery{ID: uuid.NewString(), IndexName: r.index, TenantID: tenantID, Kind: IndexDeliveryDelete, Filter: filter, Status: IndexDeliveryQueued}
	if scope.OperationID != "" {
		op.ID = scope.OperationID
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := r.lock(tx)
		if err != nil {
			return err
		}
		if state.Epoch != epoch || !state.Configured || state.CleanupHoldID != "" || tenantID <= 0 || filter == "" {
			return ErrContentIndexIsolated
		}
		if scope.OperationID != "" {
			var saved models.ContentIndexDelivery
			err := tx.Where("id = ?", op.ID).Take(&saved).Error
			switch {
			case err == nil:
				if saved.IndexName != r.index || saved.TenantID != tenantID || saved.Kind != IndexDeliveryDelete || saved.Filter != filter || saved.CleanupOperationID != "" {
					return ErrContentIndexDeliveryConflict
				}
				op = &saved
				return nil
			case !errors.Is(err, gorm.ErrRecordNotFound):
				return err
			}
			fences := make([]models.ContentIndexDeletionFence, 0, len(scope.DocumentIDs))
			for _, id := range scope.DocumentIDs {
				fences = append(fences, models.ContentIndexDeletionFence{IndexName: r.index, TenantID: tenantID, DocumentID: id, EngineID: scope.EngineID, OperationID: op.ID})
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "index_name"}, {Name: "tenant_id"}, {Name: "document_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"engine_id", "operation_id"}),
			}).Create(&fences).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(op).Error; err != nil {
			return err
		}
		return tx.Model(state).Update("isolated", true).Error
	})
	return op, err
}

// CheckWriteAfterDeletion uses the outlet lock already held by Register.
func (r *ContentIndexDeliveryRepository) CheckWriteAfterDeletion(tx *gorm.DB, op *models.ContentIndexDelivery, documents []commonclient.ManagerContentDocument) error {
	type deletion struct {
		models.ContentIndexDeletionFence
		Status string
	}
	var fences []deletion
	if err := tx.Table("manager.content_index_deletion_fences AS f").
		Select("f.*, COALESCE(d.status, '') AS status").Joins("LEFT JOIN manager.content_index_deliveries AS d ON d.id = f.operation_id").
		Where("f.index_name = ? AND f.tenant_id = ? AND f.document_id IN ?", r.index, op.TenantID, op.DocumentIDs).
		Scan(&fences).Error; err != nil {
		return err
	}
	byID := make(map[string]deletion, len(fences))
	for _, fence := range fences {
		byID[fence.DocumentID] = fence
	}
	for _, doc := range documents {
		fence, exists := byID[doc.DocumentID]
		if doc.AfterDeleteOperationID != fence.OperationID || (exists && (fence.EngineID != doc.EngineID || fence.Status != IndexDeliverySucceeded)) {
			return ErrContentIndexDeliveryConflict
		}
	}
	return nil
}

// Unknown submissions are only discoverable with pre-submission correlation.
// Legacy no-UID records remain fenced, never retroactively marked or resent.
func (r *ContentIndexDeliveryRepository) Recoverable(ctx context.Context) ([]models.ContentIndexDelivery, error) {
	var ops []models.ContentIndexDelivery
	// In-flight work blocks queued maintenance. Recover its receipt before the
	// page limit, otherwise a full page of older queued rows can starve it forever.
	err := r.db.WithContext(ctx).Where("index_name = ? AND (status IN ? OR (status IN ? AND task_correlation <> ''))", r.index, []string{IndexDeliverySubmitted, IndexDeliveryQueued}, []string{IndexDeliverySubmitting, IndexDeliveryUnknown}).Order("CASE WHEN status = 'queued' THEN 1 ELSE 0 END, created_at, id").Limit(100).Find(&ops).Error
	return ops, err
}

func (r *ContentIndexDeliveryRepository) ClaimMaintenance(ctx context.Context, epoch string, op *models.ContentIndexDelivery) (bool, error) {
	claimed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, err := r.lock(tx)
		if err != nil {
			return err
		}
		if state.Epoch != epoch || !state.Configured || state.CleanupHoldID != "" || op.CleanupOperationID != "" {
			return ErrContentIndexIsolated
		}
		var pending int64
		if err := tx.Model(&models.ContentIndexDelivery{}).Where("index_name = ? AND ((kind = ? AND status IN ?) OR (kind <> ? AND status IN ?))", r.index, IndexDeliveryWrite, unfinishedIndexDeliveries, IndexDeliveryWrite, []string{IndexDeliverySubmitting, IndexDeliverySubmitted, IndexDeliveryUnknown}).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return nil
		}
		result := tx.Model(&models.ContentIndexDelivery{}).Where("id = ? AND index_name = ? AND cleanup_operation_id = '' AND kind IN ? AND status = ?", op.ID, r.index, []string{IndexDeliveryDelete, IndexDeliveryPurge}, IndexDeliveryQueued).Updates(map[string]any{"status": IndexDeliverySubmitting, "endpoint_id": state.EndpointID, "task_correlation": op.ID})
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
		if actual.CleanupOperationID != op.CleanupOperationID || !slices.Equal(actual.EngineIDs, op.EngineIDs) || actual.Kind != op.Kind || actual.Filter != op.Filter || actual.EndpointID != op.EndpointID || actual.TaskEnqueuedAt != op.TaskEnqueuedAt || actual.TaskCorrelation != op.TaskCorrelation || !slices.Equal(actual.DocumentIDs, op.DocumentIDs) {
			return ErrContentIndexDeliveryConflict
		}
		result := tx.Model(&models.ContentIndexDelivery{}).Where("id = ? AND index_name = ? AND tenant_id = ? AND status = ? AND task_uid = ?", op.ID, r.index, op.TenantID, IndexDeliverySubmitted, *op.TaskUID).Update("status", status)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrContentIndexDeliveryConflict
		}
		if op.Kind != IndexDeliveryWrite && actual.CleanupOperationID == "" && status != IndexDeliverySucceeded {
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
		if state.Epoch != epoch || !state.Configured || state.CleanupHoldID != "" {
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
