package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MaterializationBatchRepository struct{ db *gorm.DB }

func NewMaterializationBatchRepository(db *gorm.DB) *MaterializationBatchRepository {
	return &MaterializationBatchRepository{db: db}
}

func (r *MaterializationBatchRepository) LockByLogicalTable(
	ctx context.Context,
	tenantID, logicalTableID int64,
) ([]models.MaterializationBatch, error) {
	var batches []models.MaterializationBatch
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND logical_table_id = ?", tenantID, logicalTableID).
		Order("created_at ASC, id ASC").Find(&batches).Error
	return batches, err
}

type ResolveMaterializationReadInput struct {
	TenantID          int64
	ParentExecutionID string
	ReaderExecutionID string
	ReaderAttempt     int
	ReaderLeaseToken  string
	ReaderModule      string
	LogicalTableIDs   []int64
}

// ResolveMaterializationRead validates the live reader lease and returns the
// completed staging batches for the exact requested logical tables. It never
// returns a partial result.
func (r *MaterializationBatchRepository) ResolveMaterializationRead(
	ctx context.Context,
	input ResolveMaterializationReadInput,
) ([]models.MaterializationBatch, error) {
	items := make([]models.MaterializationBatch, 0, len(input.LogicalTableIDs))
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id = ? AND execution_id = ? AND module = ? AND status = ?",
				input.TenantID, input.ParentExecutionID, commonExecution.ModuleOrchestrator, commonExecution.ExecutionStatusRunning).
			First(&parent).Error; err != nil {
			return err
		}
		var reader commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where(`tenant_id = ? AND execution_id = ? AND parent_execution_id = ? AND module = ?
				AND status = ? AND attempt = ? AND lease_token = ? AND lease_expires_at > ?`,
				input.TenantID, input.ReaderExecutionID, input.ParentExecutionID, input.ReaderModule,
				commonExecution.ExecutionStatusRunning, input.ReaderAttempt, input.ReaderLeaseToken, time.Now().UTC()).
			First(&reader).Error; err != nil {
			return err
		}
		if !sameExecutionActor(&parent, &reader) {
			return fmt.Errorf("%w: reader execution authorization lineage does not match parent", commonAPI.ErrConflict)
		}

		if err := tx.Table("model.materialization_batches AS batch").
			Select("batch.*").
			Joins("JOIN common.task_executions AS prepare_execution ON prepare_execution.execution_id = batch.prepare_execution_id").
			Where(`batch.tenant_id = ? AND batch.logical_table_id IN ? AND batch.status = ?
				AND prepare_execution.tenant_id = batch.tenant_id
				AND prepare_execution.parent_execution_id = ? AND prepare_execution.status = ?`,
				input.TenantID, input.LogicalTableIDs, models.MaterializationBatchSealed,
				input.ParentExecutionID, commonExecution.ExecutionStatusSuccess).
			Order("batch.logical_table_id ASC").Find(&items).Error; err != nil {
			return err
		}
		if len(items) != len(input.LogicalTableIDs) {
			return fmt.Errorf("%w: one or more materialization inputs are not completed", commonAPI.ErrConflict)
		}
		seen := make(map[int64]struct{}, len(items))
		for i := range items {
			if _, exists := seen[items[i].LogicalTableID]; exists {
				return fmt.Errorf("%w: materialization input is ambiguous", commonAPI.ErrConflict)
			}
			seen[items[i].LogicalTableID] = struct{}{}
		}
		return nil
	})
	return items, err
}

func (r *MaterializationBatchRepository) CreatePrepareExecution(
	ctx context.Context,
	batch *models.MaterializationBatch,
	execution *commonExecution.TaskExecution,
	tableName string,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if execution.ParentExecutionID == nil {
			return fmt.Errorf("%w: materialization prepare requires parent execution", commonAPI.ErrConflict)
		}
		var parent commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id = ? AND execution_id = ? AND module = ? AND status = ?",
				batch.TenantID, *execution.ParentExecutionID, commonExecution.ModuleOrchestrator, commonExecution.ExecutionStatusRunning).
			First(&parent).Error; err != nil {
			return err
		}
		if parent.ActorPrincipalID == nil || parent.ActorTenantMembershipID == nil || parent.IssuedAuthorizationVersion == nil {
			return fmt.Errorf("%w: orchestration parent has no authorization lineage", commonAPI.ErrConflict)
		}
		var table models.LogicalTable
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("id = ? AND tenant_id = ?", batch.LogicalTableID, batch.TenantID).
			First(&table).Error; err != nil {
			return err
		}
		if table.Status != "approved" || table.Version != batch.LogicalTableVersion {
			return fmt.Errorf("%w: logical table approval or version changed", commonAPI.ErrConflict)
		}
		if err := abortSupersededMaterializationBatch(tx, batch); err != nil {
			return err
		}
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		sourceTaskID := fmt.Sprintf("%d", batch.LogicalTableID)
		execution.SourceTaskID = &sourceTaskID
		execution.SourceTaskName = &tableName
		execution.ActorPrincipalID = parent.ActorPrincipalID
		execution.ActorTenantMembershipID = parent.ActorTenantMembershipID
		execution.IssuedAuthorizationVersion = parent.IssuedAuthorizationVersion
		return tx.Create(execution).Error
	})
}

func abortSupersededMaterializationBatch(tx *gorm.DB, next *models.MaterializationBatch) error {
	var current models.MaterializationBatch
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(`tenant_id = ? AND engine_id = ? AND target_parent_locator = ? AND target_name = ?
			AND status IN ?`, next.TenantID, next.EngineID, next.TargetParentLocator, next.TargetName,
			[]string{
				models.MaterializationBatchPreparing,
				models.MaterializationBatchPrepared,
				models.MaterializationBatchSealed,
				models.MaterializationBatchPublishing,
			}).
		Order("created_at DESC").First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Status == models.MaterializationBatchPublishing {
		return fmt.Errorf("%w: publishing materialization batch cannot be superseded", commonAPI.ErrConflict)
	}

	var prepare commonExecution.TaskExecution
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id = ? AND execution_id = ? AND module = ? AND task_type = ?",
			current.TenantID, current.PrepareExecutionID, commonExecution.ModuleModel, commonExecution.TaskTypeMaterializationPrepare).
		First(&prepare).Error; err != nil {
		return fmt.Errorf("%w: current materialization prepare execution is unavailable", commonAPI.ErrConflict)
	}
	if prepare.ParentExecutionID == nil || strings.TrimSpace(*prepare.ParentExecutionID) == "" {
		return fmt.Errorf("%w: current materialization batch has no orchestration parent", commonAPI.ErrConflict)
	}

	var parent commonExecution.TaskExecution
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id = ? AND execution_id = ? AND module = ?",
			current.TenantID, strings.TrimSpace(*prepare.ParentExecutionID), commonExecution.ModuleOrchestrator).
		First(&parent).Error; err != nil {
		return fmt.Errorf("%w: current materialization parent execution is unavailable", commonAPI.ErrConflict)
	}
	if !isFailedOrCancelledExecutionStatus(parent.Status) {
		return fmt.Errorf("%w: current materialization parent execution is not reclaimable", commonAPI.ErrConflict)
	}

	var activeChildren int64
	if err := tx.Model(&commonExecution.TaskExecution{}).
		Where("tenant_id = ? AND parent_execution_id = ? AND status IN ?",
			current.TenantID, parent.ExecutionID,
			[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
		Count(&activeChildren).Error; err != nil {
		return err
	}
	if activeChildren != 0 {
		return fmt.Errorf("%w: current materialization parent still has active child executions", commonAPI.ErrConflict)
	}

	result := tx.Model(&models.MaterializationBatch{}).
		Where("id = ? AND tenant_id = ? AND status = ?", current.ID, current.TenantID, current.Status).
		Updates(map[string]interface{}{
			"status":     models.MaterializationBatchAborted,
			"updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: current materialization batch state changed", commonAPI.ErrConflict)
	}
	return nil
}

func isFailedOrCancelledExecutionStatus(status string) bool {
	switch status {
	case commonExecution.ExecutionStatusFailed,
		commonExecution.ExecutionStatusTimeout,
		commonExecution.ExecutionStatusCancelled:
		return true
	default:
		return false
	}
}

func (r *MaterializationBatchRepository) ListReclaimableStagingBatches(
	ctx context.Context,
	current models.MaterializationBatch,
) ([]models.MaterializationBatch, error) {
	var batches []models.MaterializationBatch
	err := r.db.WithContext(ctx).
		Where(`tenant_id = ? AND engine_id = ? AND target_parent_locator = ? AND target_name = ?
			AND id <> ? AND status IN ?`,
			current.TenantID, current.EngineID, current.TargetParentLocator, current.TargetName, current.ID,
			[]string{models.MaterializationBatchAborted, models.MaterializationBatchFailed}).
		Order("created_at ASC, id ASC").Find(&batches).Error
	return batches, err
}

func (r *MaterializationBatchRepository) CreatePublishExecution(
	ctx context.Context,
	tenantID, logicalTableID int64,
	parentExecutionID *string,
	execution *commonExecution.TaskExecution,
) (*models.MaterializationBatch, error) {
	var batch models.MaterializationBatch
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var groupedCount int64
		if err := tx.Model(&models.MaterializationGroupMember{}).
			Where("tenant_id = ? AND logical_table_id = ?", tenantID, logicalTableID).Count(&groupedCount).Error; err != nil {
			return err
		}
		if groupedCount > 0 {
			return fmt.Errorf("%w: grouped logical table requires materialization group publish", commonAPI.ErrConflict)
		}
		query := tx.Table("model.materialization_batches AS batch").
			Select("batch.*").
			Joins("JOIN common.task_executions AS prepare_execution ON prepare_execution.execution_id = batch.prepare_execution_id").
			Where("batch.tenant_id = ? AND batch.logical_table_id = ? AND batch.status = ?",
				tenantID, logicalTableID, models.MaterializationBatchSealed)
		if parentExecutionID == nil {
			return fmt.Errorf("%w: materialization publish requires an orchestration parent execution", commonAPI.ErrConflict)
		}
		query = query.Where("prepare_execution.parent_execution_id = ?", *parentExecutionID)
		if err := query.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "batch"}}).
			First(&batch).Error; err != nil {
			return err
		}
		var table models.LogicalTable
		if err := tx.Select("id", "name", "status", "version").
			Where("id = ? AND tenant_id = ?", logicalTableID, tenantID).First(&table).Error; err != nil {
			return err
		}
		if table.Status != "approved" || table.Version != batch.LogicalTableVersion {
			return fmt.Errorf("%w: logical table approval or version changed", commonAPI.ErrConflict)
		}
		if batch.WriterExecutionID == nil || batch.SealExecutionID == nil || strings.TrimSpace(batch.StagingName) == "" {
			return fmt.Errorf("%w: materialization batch is not sealed", commonAPI.ErrConflict)
		}
		var parent commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id = ? AND execution_id = ? AND module = ? AND status = ?",
				tenantID, *parentExecutionID, commonExecution.ModuleOrchestrator, commonExecution.ExecutionStatusRunning).
			First(&parent).Error; err != nil {
			return err
		}
		if parent.ActorPrincipalID == nil || parent.ActorTenantMembershipID == nil || parent.IssuedAuthorizationVersion == nil {
			return fmt.Errorf("%w: orchestration parent has no authorization lineage", commonAPI.ErrConflict)
		}
		sourceTaskID := fmt.Sprintf("%d", logicalTableID)
		execution.SourceTaskID = &sourceTaskID
		execution.SourceTaskName = &table.Name
		execution.ActorPrincipalID = parent.ActorPrincipalID
		execution.ActorTenantMembershipID = parent.ActorTenantMembershipID
		execution.IssuedAuthorizationVersion = parent.IssuedAuthorizationVersion
		execution.ExecutionConfig["batch_id"] = batch.ID
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		if result := tx.Model(&batch).Where("status = ?", models.MaterializationBatchSealed).
			Updates(map[string]interface{}{
				"status":               models.MaterializationBatchPublishing,
				"publish_execution_id": execution.ExecutionID,
				"updated_at":           time.Now().UTC(),
			}); result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return result.Error
			}
			return fmt.Errorf("%w: materialization batch state changed", commonAPI.ErrConflict)
		}
		batch.Status = models.MaterializationBatchPublishing
		batch.PublishExecutionID = &execution.ExecutionID
		return nil
	})
	return &batch, err
}

func (r *MaterializationBatchRepository) CreateGroupPublishExecution(
	ctx context.Context,
	tenantID int64,
	groupID int64,
	expectedGroupVersion int64,
	parentExecutionID string,
	execution *commonExecution.TaskExecution,
) (*models.MaterializationGroup, []models.MaterializationBatch, error) {
	var group models.MaterializationGroup
	var batches []models.MaterializationBatch
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ?", tenantID, groupID).First(&group).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND group_id = ?", tenantID, groupID).
			Order("position ASC").Find(&group.Members).Error; err != nil {
			return err
		}
		if len(group.Members) == 0 {
			return fmt.Errorf("%w: materialization group is empty", commonAPI.ErrConflict)
		}
		if expectedGroupVersion <= 0 || group.Version != expectedGroupVersion {
			return fmt.Errorf("%w: materialization group version does not match publish expectation", commonAPI.ErrConflict)
		}
		var parent commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id = ? AND execution_id = ? AND module = ? AND status = ?",
				tenantID, parentExecutionID, commonExecution.ModuleOrchestrator, commonExecution.ExecutionStatusRunning).
			First(&parent).Error; err != nil {
			return err
		}
		if parent.ActorPrincipalID == nil || parent.ActorTenantMembershipID == nil || parent.IssuedAuthorizationVersion == nil {
			return fmt.Errorf("%w: orchestration parent has no authorization lineage", commonAPI.ErrConflict)
		}
		logicalTableIDs := make([]int64, 0, len(group.Members))
		for _, member := range group.Members {
			logicalTableIDs = append(logicalTableIDs, member.LogicalTableID)
		}
		if err := tx.Table("model.materialization_batches AS batch").
			Select("batch.*").
			Joins("JOIN common.task_executions AS prepare_execution ON prepare_execution.execution_id = batch.prepare_execution_id").
			Where(`batch.tenant_id = ? AND batch.logical_table_id IN ? AND batch.status = ?
				AND prepare_execution.parent_execution_id = ? AND prepare_execution.status = ?`, tenantID, logicalTableIDs, models.MaterializationBatchSealed,
				parentExecutionID, commonExecution.ExecutionStatusSuccess).
			Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "batch"}}).
			Find(&batches).Error; err != nil {
			return err
		}
		if len(batches) != len(logicalTableIDs) {
			return fmt.Errorf("%w: one or more materialization group batches are not completed", commonAPI.ErrConflict)
		}
		byTable := make(map[int64]models.MaterializationBatch, len(batches))
		for _, batch := range batches {
			if _, exists := byTable[batch.LogicalTableID]; exists {
				return fmt.Errorf("%w: materialization group batch is ambiguous", commonAPI.ErrConflict)
			}
			byTable[batch.LogicalTableID] = batch
		}
		ordered := make([]models.MaterializationBatch, 0, len(logicalTableIDs))
		batchIDs := make([]string, 0, len(logicalTableIDs))
		var engineID int64
		for _, logicalTableID := range logicalTableIDs {
			batch, exists := byTable[logicalTableID]
			if !exists || batch.WriterExecutionID == nil || batch.SealExecutionID == nil || strings.TrimSpace(batch.StagingName) == "" {
				return fmt.Errorf("%w: materialization group batch is incomplete", commonAPI.ErrConflict)
			}
			if engineID == 0 {
				engineID = batch.EngineID
			} else if engineID != batch.EngineID {
				return fmt.Errorf("%w: materialization group spans engines", commonAPI.ErrConflict)
			}
			ordered = append(ordered, batch)
			batchIDs = append(batchIDs, batch.ID)
		}
		batches = ordered
		sourceTaskID := fmt.Sprintf("%d", group.ID)
		execution.SourceTaskID = &sourceTaskID
		execution.SourceTaskName = &group.Name
		execution.ActorPrincipalID = parent.ActorPrincipalID
		execution.ActorTenantMembershipID = parent.ActorTenantMembershipID
		execution.IssuedAuthorizationVersion = parent.IssuedAuthorizationVersion
		execution.ExecutionConfig["group_id"] = group.ID
		execution.ExecutionConfig["group_version"] = group.Version
		execution.ExecutionConfig["batch_ids"] = batchIDs
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		ids := make([]string, 0, len(batches))
		for _, batch := range batches {
			ids = append(ids, batch.ID)
		}
		result := tx.Model(&models.MaterializationBatch{}).
			Where("tenant_id = ? AND id IN ? AND status = ?", tenantID, ids, models.MaterializationBatchSealed).
			Updates(map[string]interface{}{
				"status": models.MaterializationBatchPublishing, "publish_execution_id": execution.ExecutionID, "updated_at": time.Now().UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("%w: materialization group batch state changed", commonAPI.ErrConflict)
		}
		for index := range batches {
			batches[index].Status = models.MaterializationBatchPublishing
			batches[index].PublishExecutionID = &execution.ExecutionID
		}
		return nil
	})
	return &group, batches, err
}

func (r *MaterializationBatchRepository) FailPendingGroupExecution(
	ctx context.Context,
	tenantID int64,
	executionID string,
	batchIDs []string,
	errorCode string,
) error {
	if len(batchIDs) == 0 {
		return fmt.Errorf("%w: materialization group has no batches", commonAPI.ErrConflict)
	}
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("tenant_id = ? AND execution_id = ? AND status = ?", tenantID, executionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status": commonExecution.ExecutionStatusFailed, "completed_at": now, "updated_at": now,
				"error_details": commonModels.JSONMap{"code": errorCode, "message": "materialization group authorization could not be prepared"},
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: materialization group execution is not pending", commonAPI.ErrConflict)
		}
		return updateMaterializationGroupBatchStates(tx, tenantID, executionID, batchIDs, models.MaterializationBatchSealed, nil)
	})
}

type CreateSealExecutionInput struct {
	TenantID          int64
	LogicalTableID    int64
	BatchID           string
	ParentExecutionID string
	WriterExecutionID string
	TargetLocator     string
}

func (r *MaterializationBatchRepository) CreateSealExecution(
	ctx context.Context,
	input CreateSealExecutionInput,
	execution *commonExecution.TaskExecution,
) (*models.MaterializationBatch, error) {
	var batch models.MaterializationBatch
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id = ? AND execution_id = ? AND module = ? AND status = ?",
				input.TenantID, input.ParentExecutionID, commonExecution.ModuleOrchestrator, commonExecution.ExecutionStatusRunning).
			First(&commonExecution.TaskExecution{}).Error; err != nil {
			return err
		}
		var parent commonExecution.TaskExecution
		if err := tx.Where("tenant_id = ? AND execution_id = ?", input.TenantID, input.ParentExecutionID).
			First(&parent).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ? AND logical_table_id = ? AND status = ? AND seal_execution_id IS NULL",
				input.TenantID, input.BatchID, input.LogicalTableID, models.MaterializationBatchPrepared).
			First(&batch).Error; err != nil {
			return err
		}

		var prepare commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id = ? AND execution_id = ? AND parent_execution_id = ? AND status = ?",
				input.TenantID, batch.PrepareExecutionID, input.ParentExecutionID, commonExecution.ExecutionStatusSuccess).
			First(&prepare).Error; err != nil {
			return err
		}
		var writer commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id = ? AND execution_id = ? AND parent_execution_id = ? AND status = ?",
				input.TenantID, input.WriterExecutionID, input.ParentExecutionID, commonExecution.ExecutionStatusSuccess).
			First(&writer).Error; err != nil {
			return err
		}
		if !sameExecutionActor(&parent, &prepare) || !sameExecutionActor(&parent, &writer) {
			return fmt.Errorf("%w: materialization execution authorization lineage does not match parent", commonAPI.ErrConflict)
		}
		outputs, ok := writer.Metadata["outputs"].(map[string]interface{})
		if !ok {
			if typed, typedOK := writer.Metadata["outputs"].(commonModels.JSONMap); typedOK {
				outputs = map[string]interface{}(typed)
				ok = true
			}
		}
		if !ok || strings.TrimSpace(outputString(outputs, "execution_id")) != input.WriterExecutionID ||
			strings.TrimSpace(outputString(outputs, "target_locator")) != strings.TrimSpace(input.TargetLocator) {
			return fmt.Errorf("%w: writer execution outputs do not match seal input", commonAPI.ErrConflict)
		}

		var table models.LogicalTable
		if err := tx.Select("id", "name", "status", "version").
			Where("id = ? AND tenant_id = ?", input.LogicalTableID, input.TenantID).First(&table).Error; err != nil {
			return err
		}
		if table.Status != "approved" || table.Version != batch.LogicalTableVersion {
			return fmt.Errorf("%w: logical table approval or version changed", commonAPI.ErrConflict)
		}
		sourceTaskID := fmt.Sprintf("%d", input.LogicalTableID)
		execution.SourceTaskID = &sourceTaskID
		execution.SourceTaskName = &table.Name
		execution.ActorPrincipalID = parent.ActorPrincipalID
		execution.ActorTenantMembershipID = parent.ActorTenantMembershipID
		execution.IssuedAuthorizationVersion = parent.IssuedAuthorizationVersion
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		result := tx.Model(&models.MaterializationBatch{}).
			Where("tenant_id = ? AND id = ? AND status = ? AND seal_execution_id IS NULL",
				input.TenantID, input.BatchID, models.MaterializationBatchPrepared).
			Updates(map[string]interface{}{
				"writer_execution_id": input.WriterExecutionID,
				"seal_execution_id":   execution.ExecutionID,
				"updated_at":          now,
			})
		if result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return result.Error
			}
			return fmt.Errorf("%w: materialization batch state changed", commonAPI.ErrConflict)
		}
		batch.WriterExecutionID = &input.WriterExecutionID
		batch.SealExecutionID = &execution.ExecutionID
		return nil
	})
	return &batch, err
}

func sameExecutionActor(left, right *commonExecution.TaskExecution) bool {
	return left != nil && right != nil && left.ActorPrincipalID != nil && right.ActorPrincipalID != nil &&
		left.ActorTenantMembershipID != nil && right.ActorTenantMembershipID != nil &&
		left.IssuedAuthorizationVersion != nil && right.IssuedAuthorizationVersion != nil &&
		*left.ActorPrincipalID == *right.ActorPrincipalID &&
		*left.ActorTenantMembershipID == *right.ActorTenantMembershipID &&
		*left.IssuedAuthorizationVersion == *right.IssuedAuthorizationVersion
}

func outputString(outputs map[string]interface{}, key string) string {
	value, _ := outputs[key].(string)
	return value
}

func (r *MaterializationBatchRepository) AttachAuthorization(
	ctx context.Context,
	tenantID int64,
	executionID string,
	fields map[string]interface{},
) error {
	fields["updated_at"] = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).
		Where("tenant_id = ? AND execution_id = ? AND status = ? AND execution_authorization_id IS NULL",
			tenantID, executionID, commonExecution.ExecutionStatusPending).
		Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: materialization execution cannot attach authorization", commonAPI.ErrConflict)
	}
	return nil
}

func (r *MaterializationBatchRepository) FailPendingExecution(
	ctx context.Context,
	tenantID int64,
	executionID, batchID, taskType, errorCode string,
) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("tenant_id = ? AND execution_id = ? AND status = ?", tenantID, executionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status":        commonExecution.ExecutionStatusFailed,
				"completed_at":  now,
				"updated_at":    now,
				"error_details": commonModels.JSONMap{"code": errorCode, "message": "materialization authorization could not be prepared"},
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: materialization execution is not pending", commonAPI.ErrConflict)
		}
		batchStatus := models.MaterializationBatchFailed
		if taskType == commonExecution.TaskTypeMaterializationPublish {
			batchStatus = models.MaterializationBatchSealed
		} else if taskType == commonExecution.TaskTypeMaterializationSeal {
			batchStatus = models.MaterializationBatchPrepared
		}
		return tx.Model(&models.MaterializationBatch{}).
			Where("id = ? AND tenant_id = ?", batchID, tenantID).
			Updates(map[string]interface{}{"status": batchStatus, "updated_at": now}).Error
	})
}

func (r *MaterializationBatchRepository) ClaimPendingExecution(
	ctx context.Context,
	taskType, workerID string,
	now time.Time,
	lease time.Duration,
) (*commonExecution.TaskExecution, *models.MaterializationBatch, error) {
	var execution *commonExecution.TaskExecution
	var batch models.MaterializationBatch
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, _, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleModel, TaskType: taskType, WorkerID: workerID,
			Now: now, LeaseDuration: lease, RequireAuthorization: true,
		})
		if err != nil || execution == nil {
			return err
		}
		batchID, ok := execution.ExecutionConfig["batch_id"].(string)
		if !ok || batchID == "" {
			return fmt.Errorf("materialization execution %s has no batch_id", execution.ExecutionID)
		}
		return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", batchID, execution.TenantID).First(&batch).Error
	})
	if err != nil || execution == nil {
		return execution, nil, err
	}
	return execution, &batch, nil
}

func (r *MaterializationBatchRepository) ClaimPendingGroupExecution(
	ctx context.Context,
	workerID string,
	now time.Time,
	lease time.Duration,
) (*commonExecution.TaskExecution, []models.MaterializationBatch, error) {
	var execution *commonExecution.TaskExecution
	var batches []models.MaterializationBatch
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, _, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleModel, TaskType: commonExecution.TaskTypeMaterializationGroupPublish,
			WorkerID: workerID, Now: now, LeaseDuration: lease, RequireAuthorization: true,
		})
		if err != nil || execution == nil {
			return err
		}
		batchIDs, err := materializationExecutionBatchIDs(execution)
		if err != nil {
			return err
		}
		var unordered []models.MaterializationBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id IN ? AND publish_execution_id = ?", execution.TenantID, batchIDs, execution.ExecutionID).
			Find(&unordered).Error; err != nil {
			return err
		}
		if len(unordered) != len(batchIDs) {
			return fmt.Errorf("materialization group execution %s has an incomplete batch set", execution.ExecutionID)
		}
		byID := make(map[string]models.MaterializationBatch, len(unordered))
		for _, batch := range unordered {
			byID[batch.ID] = batch
		}
		batches = make([]models.MaterializationBatch, 0, len(batchIDs))
		for _, batchID := range batchIDs {
			batch, ok := byID[batchID]
			if !ok {
				return fmt.Errorf("materialization group execution %s is missing batch %s", execution.ExecutionID, batchID)
			}
			batches = append(batches, batch)
		}
		return nil
	})
	if err != nil || execution == nil {
		return execution, nil, err
	}
	return execution, batches, nil
}

func (r *MaterializationBatchRepository) CompleteExecution(
	ctx context.Context,
	lease commonExecution.Lease,
	batchID, taskType, executionStatus, batchStatus string,
	metadata, errorDetails commonModels.JSONMap,
) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fields := map[string]interface{}{"progress": 100}
		if metadata != nil {
			fields["metadata"] = metadata
		}
		if errorDetails != nil {
			fields["error_details"] = errorDetails
		}
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, executionStatus, now, fields); err != nil {
			return err
		}
		updates := map[string]interface{}{"status": batchStatus, "updated_at": now}
		if taskType == commonExecution.TaskTypeMaterializationPrepare && executionStatus == commonExecution.ExecutionStatusSuccess {
			marker, exists := metadata["expected_target_marker"]
			if !exists {
				return fmt.Errorf("%w: materialization prepare result has no target predecessor state", commonAPI.ErrConflict)
			}
			switch typed := marker.(type) {
			case nil:
				updates["expected_target_marker"] = nil
			case string:
				if strings.TrimSpace(typed) == "" {
					return fmt.Errorf("%w: materialization prepare target predecessor marker is empty", commonAPI.ErrConflict)
				}
				updates["expected_target_marker"] = typed
			default:
				return fmt.Errorf("%w: materialization prepare target predecessor marker is invalid", commonAPI.ErrConflict)
			}
		}
		if batchStatus == models.MaterializationBatchPublished {
			updates["published_at"] = now
		}
		if taskType == commonExecution.TaskTypeMaterializationPublish && executionStatus != commonExecution.ExecutionStatusSuccess {
			updates["status"] = models.MaterializationBatchSealed
		} else if taskType == commonExecution.TaskTypeMaterializationSeal && executionStatus != commonExecution.ExecutionStatusSuccess {
			updates["status"] = models.MaterializationBatchPrepared
		}
		return tx.Model(&models.MaterializationBatch{}).
			Where("id = ? AND tenant_id = ?", batchID, lease.TenantID).Updates(updates).Error
	})
}

func (r *MaterializationBatchRepository) CompleteGroupExecution(
	ctx context.Context,
	lease commonExecution.Lease,
	batchIDs []string,
	executionStatus string,
	metadata, errorDetails commonModels.JSONMap,
) error {
	if len(batchIDs) == 0 {
		return fmt.Errorf("%w: materialization group has no batches", commonAPI.ErrConflict)
	}
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fields := map[string]interface{}{"progress": 100}
		if metadata != nil {
			fields["metadata"] = metadata
		}
		if errorDetails != nil {
			fields["error_details"] = errorDetails
		}
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, executionStatus, now, fields); err != nil {
			return err
		}
		status := models.MaterializationBatchSealed
		var publishedAt *time.Time
		if executionStatus == commonExecution.ExecutionStatusSuccess {
			status = models.MaterializationBatchPublished
			publishedAt = &now
		}
		return updateMaterializationGroupBatchStates(tx, int64(lease.TenantID), lease.ExecutionID, batchIDs, status, publishedAt)
	})
}

func (r *MaterializationBatchRepository) GetByID(ctx context.Context, id string, tenantID int64) (*models.MaterializationBatch, error) {
	var batch models.MaterializationBatch
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&batch).Error
	return &batch, err
}

func (r *MaterializationBatchRepository) RecoverExpiredExecutions(ctx context.Context, taskType string, now time.Time) error {
	if taskType == commonExecution.TaskTypeMaterializationGroupPublish {
		return r.recoverExpiredGroupExecutions(ctx, now)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleModel, TaskType: taskType, Now: now, Limit: 100,
		})
		if err != nil {
			return err
		}
		for _, item := range items {
			lease, err := commonExecution.LeaseFromExecution(item)
			if err != nil {
				return err
			}
			if item.Attempt < item.MaxAttempts {
				if err := commonExecution.RetryExpired(ctx, tx, lease, now, "retrying controlled materialization"); err != nil {
					return err
				}
				continue
			}
			if err := commonExecution.FailExpired(ctx, tx, lease, now, map[string]interface{}{
				"error_details": commonModels.JSONMap{"code": "model.materialization.lease_expired", "message": "materialization worker lease expired"},
			}); err != nil {
				return err
			}
			batchID, _ := item.ExecutionConfig["batch_id"].(string)
			batchStatus := models.MaterializationBatchFailed
			if taskType == commonExecution.TaskTypeMaterializationPublish {
				batchStatus = models.MaterializationBatchSealed
			} else if taskType == commonExecution.TaskTypeMaterializationSeal {
				batchStatus = models.MaterializationBatchPrepared
			}
			if err := tx.Model(&models.MaterializationBatch{}).
				Where("id = ? AND tenant_id = ?", batchID, item.TenantID).
				Updates(map[string]interface{}{"status": batchStatus, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *MaterializationBatchRepository) recoverExpiredGroupExecutions(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleModel, TaskType: commonExecution.TaskTypeMaterializationGroupPublish, Now: now, Limit: 100,
		})
		if err != nil {
			return err
		}
		for _, item := range items {
			lease, err := commonExecution.LeaseFromExecution(item)
			if err != nil {
				return err
			}
			if item.Attempt < item.MaxAttempts {
				if err := commonExecution.RetryExpired(ctx, tx, lease, now, "retrying controlled materialization group publish"); err != nil {
					return err
				}
				continue
			}
			if err := commonExecution.FailExpired(ctx, tx, lease, now, map[string]interface{}{
				"error_details": commonModels.JSONMap{"code": "model.materialization_group.lease_expired", "message": "materialization group worker lease expired"},
			}); err != nil {
				return err
			}
			batchIDs, err := materializationExecutionBatchIDs(&item)
			if err != nil {
				return err
			}
			if err := updateMaterializationGroupBatchStates(tx, int64(item.TenantID), item.ExecutionID, batchIDs, models.MaterializationBatchSealed, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func materializationExecutionBatchIDs(execution *commonExecution.TaskExecution) ([]string, error) {
	if execution == nil || execution.ExecutionConfig == nil {
		return nil, errors.New("materialization group execution config is missing")
	}
	raw, ok := execution.ExecutionConfig["batch_ids"]
	if !ok {
		return nil, errors.New("materialization group execution batch_ids are missing")
	}
	var values []interface{}
	switch typed := raw.(type) {
	case []interface{}:
		values = typed
	case []string:
		values = make([]interface{}, len(typed))
		for index := range typed {
			values[index] = typed[index]
		}
	default:
		return nil, errors.New("materialization group execution batch_ids are invalid")
	}
	if len(values) == 0 {
		return nil, errors.New("materialization group execution batch_ids are empty")
	}
	batchIDs := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		batchID, ok := value.(string)
		batchID = strings.TrimSpace(batchID)
		if !ok || batchID == "" {
			return nil, errors.New("materialization group execution batch_id is invalid")
		}
		if _, duplicate := seen[batchID]; duplicate {
			return nil, errors.New("materialization group execution batch_ids contain duplicates")
		}
		seen[batchID] = struct{}{}
		batchIDs = append(batchIDs, batchID)
	}
	return batchIDs, nil
}

func updateMaterializationGroupBatchStates(
	tx *gorm.DB,
	tenantID int64,
	executionID string,
	batchIDs []string,
	status string,
	publishedAt *time.Time,
) error {
	updates := map[string]interface{}{"status": status, "updated_at": time.Now().UTC()}
	if publishedAt != nil {
		updates["published_at"] = *publishedAt
	}
	result := tx.Model(&models.MaterializationBatch{}).
		Where("tenant_id = ? AND id IN ? AND publish_execution_id = ? AND status = ?", tenantID, batchIDs, executionID, models.MaterializationBatchPublishing).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != int64(len(batchIDs)) {
		return fmt.Errorf("%w: materialization group batch state changed", commonAPI.ErrConflict)
	}
	return nil
}
