package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *TaskService) GetSchemaChange(ctx context.Context, taskID, tenantID uint) (*models.SchemaChangeRequestView, error) {
	task, err := s.GetTask(ctx, taskID, tenantID)
	if err != nil {
		return nil, err
	}
	request, err := s.schemaChangeRepo.GetPending(ctx, taskID, tenantID)
	if errors.Is(err, repository.ErrSchemaChangeRequestNotFound) {
		request, err = s.schemaChangeRepo.GetLatest(ctx, taskID, tenantID)
	}
	if errors.Is(err, repository.ErrSchemaChangeRequestNotFound) {
		return nil, ErrSchemaChangeNotFound
	}
	if err != nil {
		return nil, err
	}
	if request.Status != models.SchemaChangeRequestPending {
		return schemaChangeRequestView(request)
	}
	view, err := schemaChangeRequestView(request)
	if err != nil {
		return nil, err
	}
	unexpected, eligible, err := additiveUnexpectedFields(request.Diff)
	if err != nil {
		return nil, err
	}
	if !eligible {
		view.ApprovalBlockedReason = "non_additive_change"
		return view, nil
	}
	if s.schemaInspector == nil {
		view.ApprovalBlockedReason = "control_unavailable"
		return view, nil
	}
	fields, err := s.schemaInspector.InspectAdditiveFields(ctx, task, unexpected)
	if err != nil {
		view.ApprovalBlockedReason = "source_validation_failed"
		return view, nil
	}
	view.Approvable = true
	view.SuggestedFields = fields
	return view, nil
}

func (s *TaskService) ApproveSchemaChange(ctx context.Context, taskID, tenantID, userID uint, approval models.ApproveSchemaChangeRequest) (*models.SchemaChangeRequestView, error) {
	if s == nil || s.db == nil || s.schemaInspector == nil || s.engineResolver == nil {
		return nil, ErrSchemaChangeControlUnavailable
	}
	var applied models.SchemaChangeRequest
	var updatedSpec planner.DatabaseCDCTaskSpec
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", taskID, tenantID).
			First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonAPI.ErrNotFound
			}
			return err
		}

		var request models.SchemaChangeRequest
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_id = ? AND tenant_id = ? AND status = ?", taskID, tenantID, models.SchemaChangeRequestPending).
			Order("detected_at DESC, id DESC").First(&request).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("task_id = ? AND tenant_id = ? AND status = ?", taskID, tenantID, models.SchemaChangeRequestApplied).
				Order("applied_at DESC, id DESC").First(&request).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrSchemaChangeNotFound
				}
				return err
			}
			stored, err := decodeApprovedSchemaFields(request.ApprovedMappings)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(canonicalSchemaFields(stored), canonicalSchemaFields(approval.Fields)) {
				return ErrSchemaChangeApprovalConflict
			}
			updatedSpec, err = planner.ParseDatabaseCDCTaskSpec(task.Config)
			if err != nil {
				return err
			}
			applied = request
			return nil
		}
		if err != nil {
			return err
		}
		if task.Status != models.TaskStatusBlocked || !planner.IsDatabaseCDCTaskConfig(task.Config) {
			return ErrSchemaChangeApprovalConflict
		}
		unexpected, eligible, err := additiveUnexpectedFields(request.Diff)
		if err != nil {
			return err
		}
		if !eligible {
			return ErrSchemaChangeNotAdditive
		}
		inspected, err := s.schemaInspector.InspectAdditiveFields(ctx, &task, unexpected)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrSchemaChangeApprovalConflict, err)
		}
		fields, err := validateSchemaChangeApproval(task.Config, inspected, approval.Fields)
		if err != nil {
			return err
		}
		updatedSpec, err = appendSchemaChangeMappings(task.Config, fields)
		if err != nil {
			return err
		}
		updatedConfig, err := databaseCDCSpecConfig(updatedSpec)
		if err != nil {
			return err
		}

		var resource models.CaptureResource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND task_id = ? AND tenant_id = ?", request.CaptureResourceID, task.ID, tenantID).
			First(&resource).Error; err != nil {
			return err
		}
		if resource.Generation != request.Generation || resource.SchemaRevision != request.FromRevision ||
			request.ToRevision != request.FromRevision+1 || resource.Status == models.CaptureStatusStopped {
			return ErrSchemaChangeApprovalConflict
		}
		continuousPlan, err := planner.BuildDatabaseCDCContinuousPlan(updatedSpec, s.engineResolver, planner.DatabaseCDCStreamBinding{
			Provider: string(resource.SourceType), ConsumerGroup: resource.ConsumerGroup,
			SourceIdentity: resource.SourceIdentity, Database: resource.SourceDatabase,
			Schema: resource.SourceSchema, Table: resource.SourceTable,
			SpatialInfo: datatype.SpatialInfoFromPayload(map[string]interface{}(resource.SourceSpatialInfo)),
		}, task.BatchSize)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrSchemaChangeApprovalConflict, err)
		}
		targetPlugin, err := engineplugin.Get(continuousPlan.TargetType)
		if err != nil {
			return err
		}
		preparer, ok := targetPlugin.(engineplugin.PartitionedTableChangeApplyProvider)
		if !ok {
			return fmt.Errorf("target engine does not implement partitioned table change apply")
		}
		if err := preparer.PreparePartitionedTableChangeApply(ctx, continuousPlan.Target.ConnInfo, continuousPlan.Target.Path, engineplugin.PartitionedTableChangeApplyOptions{
			ApplyIdentity: task.ApplyIdentity, SourceIdentity: resource.SourceIdentity,
			Fields: continuousPlan.Target.Fields, SpatialInfo: continuousPlan.Target.SpatialInfo,
			Keys: continuousPlan.Target.Keys, RequireTargetAbsent: false,
		}); err != nil {
			return fmt.Errorf("%w: prepare target additive schema: %v", ErrSchemaChangeApprovalConflict, err)
		}

		now := time.Now()
		approvedMappings, err := encodeApprovedSchemaFields(fields)
		if err != nil {
			return err
		}
		if err := tx.Model(&request).Updates(map[string]interface{}{
			"approved_mappings": approvedMappings, "status": models.SchemaChangeRequestApplied,
			"applied_by": userID, "applied_at": now,
			"metadata_scan_status":      models.SchemaChangeMetadataScanPending,
			"metadata_scan_claim_token": "", "metadata_scan_lease_until": nil,
			"metadata_scan_attempt": 0, "metadata_scan_execution_id": "", "metadata_scan_error": "",
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		resourceUpdate := tx.Model(&resource).Where("schema_revision = ?", request.FromRevision).
			Updates(map[string]interface{}{"schema_revision": request.ToRevision, "resource_version": gorm.Expr("resource_version + 1"), "updated_at": now})
		if resourceUpdate.Error != nil {
			return resourceUpdate.Error
		}
		if resourceUpdate.RowsAffected != 1 {
			return ErrSchemaChangeApprovalConflict
		}
		result := tx.Model(&task).Where("status = ?", models.TaskStatusBlocked).Updates(map[string]interface{}{
			"config": updatedConfig, "status": models.TaskStatusIdle,
			"desired_state": models.TaskDesiredStatePaused, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSchemaChangeApprovalConflict
		}
		request.ApprovedMappings = approvedMappings
		request.Status = models.SchemaChangeRequestApplied
		request.AppliedBy = &userID
		request.AppliedAt = &now
		request.MetadataScanStatus = models.SchemaChangeMetadataScanPending
		request.MetadataScanClaimToken = ""
		request.MetadataScanLeaseUntil = nil
		request.MetadataScanAttempt = 0
		request.MetadataScanExecutionID = ""
		request.MetadataScanError = ""
		request.UpdatedAt = now
		if err := repository.UpdateSchemaChangeExecutionProjectionTx(tx, &request); err != nil {
			return err
		}
		applied = request
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.reconcileSchemaChangeMetaScanWithSpec(ctx, tenantID, &applied, updatedSpec)
	return schemaChangeRequestView(&applied)
}

func additiveUnexpectedFields(diff models.JSONMap) ([]string, bool, error) {
	missing, err := schemaChangeStringSlice(diff, "missing_fields")
	if err != nil {
		return nil, false, err
	}
	unexpected, err := schemaChangeStringSlice(diff, "unexpected_fields")
	if err != nil {
		return nil, false, err
	}
	incompatible, err := schemaChangeStringSlice(diff, "incompatible_fields")
	if err != nil {
		return nil, false, err
	}
	return unexpected, len(missing) == 0 && len(incompatible) == 0 && len(unexpected) > 0, nil
}

func schemaChangeStringSlice(value models.JSONMap, key string) ([]string, error) {
	raw, ok := value[key]
	if !ok || raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result []string
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, fmt.Errorf("schema change diff %s is invalid: %w", key, err)
	}
	return result, nil
}

func validateSchemaChangeApproval(config models.JSONMap, inspected, submitted []models.SchemaChangeField) ([]models.SchemaChangeField, error) {
	if len(inspected) == 0 || len(inspected) != len(submitted) {
		return nil, ErrSchemaChangeApprovalConflict
	}
	spec, err := planner.ParseDatabaseCDCTaskSpec(config)
	if err != nil {
		return nil, err
	}
	existingSource := map[string]struct{}{}
	existingTarget := map[string]struct{}{}
	for _, field := range spec.Transforms[0].Fields {
		existingSource[strings.TrimSpace(field.Source)] = struct{}{}
		existingTarget[strings.TrimSpace(field.Target)] = struct{}{}
	}
	actualBySource := make(map[string]models.SchemaChangeField, len(inspected))
	for _, field := range inspected {
		actualBySource[field.Source] = field
	}
	result := make([]models.SchemaChangeField, 0, len(submitted))
	seenSource := map[string]struct{}{}
	seenTarget := map[string]struct{}{}
	for _, field := range submitted {
		field.Source = strings.TrimSpace(field.Source)
		field.Target = strings.TrimSpace(field.Target)
		field.TargetType = strings.TrimSpace(field.TargetType)
		actual, exists := actualBySource[field.Source]
		if !exists || field.Target == "" || !field.Nullable || field.TargetType != actual.TargetType {
			return nil, ErrSchemaChangeApprovalConflict
		}
		if _, exists := existingSource[field.Source]; exists {
			return nil, ErrSchemaChangeApprovalConflict
		}
		if _, exists := existingTarget[field.Target]; exists {
			return nil, ErrSchemaChangeApprovalConflict
		}
		if _, exists := seenSource[field.Source]; exists {
			return nil, ErrSchemaChangeApprovalConflict
		}
		if _, exists := seenTarget[field.Target]; exists {
			return nil, ErrSchemaChangeApprovalConflict
		}
		seenSource[field.Source] = struct{}{}
		seenTarget[field.Target] = struct{}{}
		result = append(result, field)
	}
	if len(seenSource) != len(actualBySource) {
		return nil, ErrSchemaChangeApprovalConflict
	}
	return canonicalSchemaFields(result), nil
}

func appendSchemaChangeMappings(config models.JSONMap, fields []models.SchemaChangeField) (planner.DatabaseCDCTaskSpec, error) {
	spec, err := planner.ParseDatabaseCDCTaskSpec(config)
	if err != nil {
		return planner.DatabaseCDCTaskSpec{}, err
	}
	for _, field := range fields {
		nullable := true
		spec.Transforms[0].Fields = append(spec.Transforms[0].Fields, planner.FieldMappingSpec{
			Source: field.Source, Target: field.Target, TargetType: field.TargetType, Nullable: &nullable,
		})
	}
	encoded, err := databaseCDCSpecConfig(spec)
	if err != nil {
		return planner.DatabaseCDCTaskSpec{}, err
	}
	return planner.ParseDatabaseCDCTaskSpec(encoded)
}

func databaseCDCSpecConfig(spec planner.DatabaseCDCTaskSpec) (models.JSONMap, error) {
	b, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var result models.JSONMap
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func canonicalSchemaFields(fields []models.SchemaChangeField) []models.SchemaChangeField {
	result := append([]models.SchemaChangeField(nil), fields...)
	for index := range result {
		result[index].Source = strings.TrimSpace(result[index].Source)
		result[index].Target = strings.TrimSpace(result[index].Target)
		result[index].TargetType = strings.TrimSpace(result[index].TargetType)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Source < result[j].Source })
	return result
}

func encodeApprovedSchemaFields(fields []models.SchemaChangeField) (models.JSONMap, error) {
	b, err := json.Marshal(map[string]interface{}{"fields": canonicalSchemaFields(fields)})
	if err != nil {
		return nil, err
	}
	var result models.JSONMap
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func decodeApprovedSchemaFields(value models.JSONMap) ([]models.SchemaChangeField, error) {
	if len(value) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(value["fields"])
	if err != nil {
		return nil, err
	}
	var result []models.SchemaChangeField
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func schemaChangeRequestView(request *models.SchemaChangeRequest) (*models.SchemaChangeRequestView, error) {
	if request == nil {
		return nil, ErrSchemaChangeNotFound
	}
	approved, err := decodeApprovedSchemaFields(request.ApprovedMappings)
	if err != nil {
		return nil, err
	}
	return &models.SchemaChangeRequestView{
		ID: request.ID, TaskID: request.TaskID, Generation: request.Generation,
		ExecutionID: request.ExecutionID, SourcePartition: request.SourcePartition, SourceOffset: request.SourceOffset,
		Scope: request.Scope, Diff: request.Diff, ApprovedMappings: approved,
		FromRevision: request.FromRevision, ToRevision: request.ToRevision, Status: request.Status,
		DetectedAt: request.DetectedAt, AppliedAt: request.AppliedAt,
		MetadataScanStatus: request.MetadataScanStatus, MetadataScanAttempt: request.MetadataScanAttempt,
		MetadataScanLeaseUntil: request.MetadataScanLeaseUntil, MetadataScanExecutionID: request.MetadataScanExecutionID,
	}, nil
}

func (s *TaskService) reconcileSchemaChangeMetaScanWithSpec(ctx context.Context, tenantID uint, request *models.SchemaChangeRequest, spec planner.DatabaseCDCTaskSpec) {
	if !s.claimSchemaChangeMetaScan(ctx, request) {
		return
	}
	s.runClaimedSchemaChangeMetaScan(ctx, tenantID, request, spec)
}

func (s *TaskService) claimSchemaChangeMetaScan(ctx context.Context, request *models.SchemaChangeRequest) bool {
	if request == nil || s.schemaChangeRepo == nil {
		return false
	}
	claimed, ownsClaim, err := s.schemaChangeRepo.ClaimMetadataScan(ctx, request.ID, time.Now(), s.schemaChangeMetaScanClaimTTL())
	if err != nil {
		s.logger.Error("failed to claim schema change Meta scan", "request_id", request.ID, "error", err)
		return false
	}
	*request = *claimed
	return ownsClaim
}

func (s *TaskService) runClaimedSchemaChangeMetaScan(ctx context.Context, tenantID uint, request *models.SchemaChangeRequest, spec planner.DatabaseCDCTaskSpec) {
	claimToken := request.MetadataScanClaimToken
	if s.metaClient == nil {
		s.reconcileSchemaChangeMetaScanResult(ctx, request, claimToken, models.SchemaChangeMetadataScanFailed, "", "meta client is unavailable")
		return
	}
	target, err := spec.Target.EngineRef()
	if err != nil {
		s.reconcileSchemaChangeMetaScanResult(ctx, request, claimToken, models.SchemaChangeMetadataScanFailed, "", err.Error())
		return
	}
	run, err := s.metaClient.WithTenantID(tenantID).CreateManualScanRun(commonClient.MetaScanOptions{
		EngineID: target.ID, CatalogPaths: nativeTargetCatalogPaths(spec.Target),
		ScanDepth: "deep", Force: true, TriggerType: commonExecution.TriggerTypeManual, Source: commonExecution.ModuleTransfer,
	})
	if err != nil {
		s.reconcileSchemaChangeMetaScanResult(ctx, request, claimToken, models.SchemaChangeMetadataScanFailed, "", err.Error())
		return
	}
	s.reconcileSchemaChangeMetaScanResult(ctx, request, claimToken, models.SchemaChangeMetadataScanSuccess, run.ExecutionID, "")
}

func (s *TaskService) reconcileSchemaChangeMetaScanResult(
	ctx context.Context,
	request *models.SchemaChangeRequest,
	claimToken string,
	status models.SchemaChangeMetadataScanStatus,
	executionID, errorMessage string,
) {
	if request == nil || claimToken == "" {
		return
	}
	completionCtx := context.WithoutCancel(ctx)
	completed, owned, err := s.schemaChangeRepo.CompleteMetadataScan(
		completionCtx, request.ID, claimToken, status, executionID, errorMessage, time.Now(),
	)
	if err != nil {
		s.logger.Error("failed to record schema change Meta scan", "request_id", request.ID, "error", err)
		return
	}
	*request = *completed
	if !owned {
		s.logger.Warn("ignored stale schema change Meta scan result", "request_id", request.ID)
	}
}

func (s *TaskService) schemaChangeMetaScanClaimTTL() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.SchemaChangeMetaScanClaimTTL > 0 {
		return s.cfg.SchemaChangeMetaScanClaimTTL
	}
	return 2 * time.Minute
}
