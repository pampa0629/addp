package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	materializationAuthorizationTTL = int64(3600)
	materializationWorkerLease      = 2 * time.Minute
	materializationWorkerPoll       = 500 * time.Millisecond
	materializationMarkerPrefix     = "addp:model-materialization:v1:"
)

type MaterializationService struct {
	systemClient        *commonClient.SystemServiceClient
	authorizationIssuer *commonClient.SystemExecutionAuthorizationClient
	repo                *repository.MaterializationBatchRepository
	logicalTableRepo    *repository.LogicalTableRepository
	logicalTableSvc     *LogicalTableService
	groupService        *MaterializationGroupService
	workerID            string
	workerCancel        context.CancelFunc
	workerDone          chan struct{}
	startOnce           sync.Once
}

func (s *MaterializationService) SetExecutionAuthorizationIssuer(issuer *commonClient.SystemExecutionAuthorizationClient) {
	s.authorizationIssuer = issuer
}

func (s *MaterializationService) SetGroupService(groupService *MaterializationGroupService) {
	s.groupService = groupService
}

func (s *MaterializationService) DecommissionMaterializedTarget(
	ctx context.Context,
	logicalTableID, tenantID int64,
	request models.MaterializedTargetDecommissionRequest,
	userAccessToken string,
) error {
	if logicalTableID <= 0 || tenantID <= 0 || request.Version <= 0 || s.authorizationIssuer == nil || s.systemClient == nil {
		return apperrors.Validation("materialized_target_request_invalid", modeli18n.MsgMaterializationInvalid)
	}
	requestedLocator, err := resourcetree.ParseURI(strings.TrimSpace(request.TargetParentLocator))
	if err != nil || requestedLocator.EngineID == 0 || requestedLocator.Type != resourcetree.TypeSchema || len(requestedLocator.Path) == 0 ||
		!identifierPattern.MatchString(strings.TrimSpace(request.TargetName)) {
		return apperrors.Validation("materialized_target_confirmation_invalid", modeli18n.MsgMaterializationInvalid)
	}

	table, err := s.logicalTableRepo.GetByID(logicalTableID, tenantID)
	if err != nil {
		return materializationResourceError(err)
	}
	if err := requireVersion(table.Version, request.Version); err != nil {
		return err
	}
	if !materializedTargetConfirmationMatches(table, request) {
		return apperrors.Conflict("materialized_target_confirmation_mismatch", modeli18n.MsgMaterializedTargetConflict)
	}

	executionID := uuid.NewString()
	issued, err := s.authorizationIssuer.Issue(ctx, strings.TrimSpace(userAccessToken), commonClient.IssueExecutionAuthorizationRequest{
		Audience: commonExecution.AudienceModel, ExecutionID: executionID,
		Accesses: []commonClient.ExecutionEngineAccessScope{{
			EngineID: strconv.FormatUint(uint64(requestedLocator.EngineID), 10), Effects: []string{"read", "ddl"},
		}},
		ExpiresIn: materializationAuthorizationTTL,
	})
	if err != nil {
		return materializedTargetAuthorizationError(err)
	}

	return s.logicalTableRepo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateMaterializedTargetDecommissionState(tx, logicalTableID, tenantID, request); err != nil {
			return err
		}

		access, err := s.systemClient.WithTenantID(uint(tenantID)).GetExecutionEngineAccess(ctx, issued.ID, commonClient.ExecutionEngineAccessRequest{
			ExecutionID: executionID, EngineID: strconv.FormatUint(uint64(requestedLocator.EngineID), 10),
			RequiredEffects: []string{"read", "ddl"},
		})
		if err != nil {
			return materializedTargetAuthorizationError(err)
		}
		engineType := strings.ToLower(strings.TrimSpace(access.Engine.EngineType))
		if engineType != "postgres" && engineType != "postgresql" && engineType != "postgis" {
			return apperrors.Conflict("materialized_target_engine_unsupported", modeli18n.MsgMaterializedTargetConflict)
		}
		pool, err := materializationPool(access.Engine)
		if err != nil {
			return apperrors.Wrap(apperrors.KindUnavailable, "materialized_target_engine_unavailable", modeli18n.MsgMaterializedTargetUnavailable, err)
		}
		schemaName := requestedLocator.Path[len(requestedLocator.Path)-1]
		if err := pool.WithContext(ctx).Transaction(func(physicalTx *gorm.DB) error {
			return dropOwnedMaterializedTarget(physicalTx, schemaName, strings.TrimSpace(request.TargetName), logicalTableID)
		}); err != nil {
			if errors.Is(err, errMaterializedTargetOwnershipMismatch) {
				return apperrors.Conflict("materialized_target_ownership_mismatch", modeli18n.MsgMaterializedTargetConflict)
			}
			return apperrors.Wrap(apperrors.KindUnavailable, "materialized_target_drop_failed", modeli18n.MsgMaterializedTargetUnavailable, err)
		}
		return nil
	})
}

func validateMaterializedTargetDecommissionState(
	tx *gorm.DB,
	logicalTableID, tenantID int64,
	request models.MaterializedTargetDecommissionRequest,
) error {
	var locked models.LogicalTable
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", logicalTableID, tenantID).First(&locked).Error; err != nil {
		return materializationResourceError(err)
	}
	if err := requireVersion(locked.Version, request.Version); err != nil {
		return err
	}
	if !materializedTargetConfirmationMatches(&locked, request) {
		return apperrors.Conflict("materialized_target_confirmation_mismatch", modeli18n.MsgMaterializedTargetConflict)
	}
	var groupCount int64
	if err := tx.Model(&models.MaterializationGroupMember{}).
		Where("tenant_id = ? AND logical_table_id = ?", tenantID, logicalTableID).Count(&groupCount).Error; err != nil {
		return err
	}
	if groupCount != 0 {
		return apperrors.Conflict("materialized_target_group_member", modeli18n.MsgMaterializedTargetConflict)
	}
	var activeBatchCount int64
	if err := tx.Model(&models.MaterializationBatch{}).
		Where("tenant_id = ? AND logical_table_id = ? AND status IN ?", tenantID, logicalTableID, []string{
			models.MaterializationBatchPreparing, models.MaterializationBatchPrepared,
			models.MaterializationBatchSealed, models.MaterializationBatchPublishing,
		}).Count(&activeBatchCount).Error; err != nil {
		return err
	}
	if activeBatchCount != 0 {
		return apperrors.Conflict("materialized_target_batch_active", modeli18n.MsgMaterializedTargetConflict)
	}
	return nil
}

var errMaterializedTargetOwnershipMismatch = errors.New("materialized target ownership marker mismatch")

func materializedTargetConfirmationMatches(table *models.LogicalTable, request models.MaterializedTargetDecommissionRequest) bool {
	if table == nil {
		return false
	}
	locator, locatorOK := materializationString(table.Materialization, "target_parent_locator")
	targetName, nameOK := materializationString(table.Materialization, "target_name")
	return locatorOK && nameOK && locator == strings.TrimSpace(request.TargetParentLocator) &&
		targetName == strings.TrimSpace(request.TargetName)
}

func dropOwnedMaterializedTarget(tx *gorm.DB, schemaName, tableName string, logicalTableID int64) error {
	comment, exists, err := physicalTableComment(tx, schemaName, tableName)
	if err != nil || !exists {
		return err
	}
	if !materializationMarkerOwnedBy(comment, logicalTableID) {
		return errMaterializedTargetOwnershipMismatch
	}
	return tx.Exec("DROP TABLE " + qualifiedIdentifier(schemaName, tableName)).Error
}

func materializedTargetAuthorizationError(err error) error {
	if status, ok := commonClient.SystemAPIStatusCode(err); ok && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
		return apperrors.Wrap(apperrors.KindForbidden, "materialized_target_engine_access_denied", modeli18n.MsgMaterializedTargetForbidden, err)
	}
	return apperrors.Wrap(apperrors.KindUnavailable, "materialized_target_authorization_unavailable", modeli18n.MsgMaterializedTargetUnavailable, err)
}

type MaterializationReadColumn struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Nullable bool   `json:"nullable"`
}

type MaterializationReadItem struct {
	LogicalTableID    int64                       `json:"logical_table_id"`
	BatchID           string                      `json:"batch_id"`
	EngineID          int64                       `json:"engine_id"`
	StagingLocator    string                      `json:"staging_locator"`
	Columns           []MaterializationReadColumn `json:"columns"`
	SchemaFingerprint string                      `json:"schema_fingerprint"`
}

type MaterializationReadContext struct {
	SchemaVersion string                    `json:"schema_version"`
	Items         []MaterializationReadItem `json:"items"`
}

func (s *MaterializationService) ResolveReadContext(
	ctx context.Context,
	tenantID int64,
	parentExecutionID, readerExecutionID string,
	readerAttempt int,
	readerLeaseToken string,
	logicalTableIDs []int64,
	serviceClientID string,
) (*MaterializationReadContext, error) {
	if tenantID <= 0 || readerAttempt <= 0 || len(logicalTableIDs) == 0 || len(logicalTableIDs) > 100 {
		return nil, apperrors.Validation("materialization_read_context_invalid", modeli18n.MsgMaterializationInvalid)
	}
	parentID, err := uuid.Parse(strings.TrimSpace(parentExecutionID))
	if err != nil {
		return nil, apperrors.Validation("materialization_parent_execution_invalid", modeli18n.MsgMaterializationInvalid)
	}
	readerExecutionID = strings.TrimSpace(readerExecutionID)
	if readerExecutionID == "" || len(readerExecutionID) > 255 {
		return nil, apperrors.Validation("materialization_reader_execution_invalid", modeli18n.MsgMaterializationInvalid)
	}
	leaseToken, err := uuid.Parse(strings.TrimSpace(readerLeaseToken))
	if err != nil {
		return nil, apperrors.Validation("materialization_reader_lease_invalid", modeli18n.MsgMaterializationInvalid)
	}
	readerModule, err := materializationReaderModule(serviceClientID)
	if err != nil {
		return nil, err
	}
	unique := make(map[int64]struct{}, len(logicalTableIDs))
	for _, id := range logicalTableIDs {
		if id <= 0 {
			return nil, apperrors.Validation("materialization_read_context_invalid", modeli18n.MsgMaterializationInvalid)
		}
		if _, exists := unique[id]; exists {
			return nil, apperrors.Validation("materialization_read_context_duplicate", modeli18n.MsgMaterializationInvalid)
		}
		unique[id] = struct{}{}
	}
	batches, err := s.repo.ResolveMaterializationRead(ctx, repository.ResolveMaterializationReadInput{
		TenantID: tenantID, ParentExecutionID: parentID.String(), ReaderExecutionID: readerExecutionID,
		ReaderAttempt: readerAttempt, ReaderLeaseToken: leaseToken.String(), ReaderModule: readerModule,
		LogicalTableIDs: logicalTableIDs,
	})
	if err != nil {
		return nil, materializationResourceError(err)
	}
	byID := make(map[int64]models.MaterializationBatch, len(batches))
	for _, batch := range batches {
		byID[batch.LogicalTableID] = batch
	}
	result := &MaterializationReadContext{SchemaVersion: "model.materialization-read-context/v1", Items: make([]MaterializationReadItem, 0, len(logicalTableIDs))}
	var engineID int64
	for _, logicalTableID := range logicalTableIDs {
		batch := byID[logicalTableID]
		table, fields, parentLocator, _, fingerprint, loadErr := s.loadApprovedDefinition(logicalTableID, tenantID)
		if loadErr != nil {
			return nil, loadErr
		}
		if table.Version != batch.LogicalTableVersion || fingerprint != batch.SchemaFingerprint ||
			int64(parentLocator.EngineID) != batch.EngineID || parentLocator.ToURI() != batch.TargetParentLocator ||
			batch.Status != models.MaterializationBatchSealed || batch.WriterExecutionID == nil ||
			batch.SealExecutionID == nil || strings.TrimSpace(batch.StagingName) == "" {
			return nil, apperrors.Conflict("materialization_read_context_stale", modeli18n.MsgMaterializationConflict)
		}
		if engineID == 0 {
			engineID = batch.EngineID
		} else if engineID != batch.EngineID {
			return nil, apperrors.Conflict("materialization_read_context_cross_engine", modeli18n.MsgMaterializationConflict)
		}
		columns := make([]MaterializationReadColumn, 0, len(fields))
		for _, field := range fields {
			columns = append(columns, MaterializationReadColumn{Name: field.ColumnName, DataType: field.DataType, Nullable: field.Nullable})
		}
		locator := (&resourcetree.ResourceLocator{EngineID: parentLocator.EngineID, Path: append(append([]string(nil), parentLocator.Path...), batch.StagingName), Type: resourcetree.TypeTable}).ToURI()
		result.Items = append(result.Items, MaterializationReadItem{
			LogicalTableID: logicalTableID, BatchID: batch.ID, EngineID: batch.EngineID,
			StagingLocator: locator, Columns: columns, SchemaFingerprint: batch.SchemaFingerprint,
		})
	}
	return result, nil
}

func materializationReaderModule(serviceClientID string) (string, error) {
	switch strings.TrimSpace(serviceClientID) {
	case "addp-quality":
		return commonExecution.ModuleQuality, nil
	default:
		return "", apperrors.Conflict("materialization_reader_client_invalid", modeli18n.MsgMaterializationConflict)
	}
}

func NewMaterializationService(
	systemClient *commonClient.SystemServiceClient,
	repo *repository.MaterializationBatchRepository,
	logicalTableRepo *repository.LogicalTableRepository,
	logicalTableSvc *LogicalTableService,
) *MaterializationService {
	return &MaterializationService{
		systemClient: systemClient,
		repo:         repo, logicalTableRepo: logicalTableRepo, logicalTableSvc: logicalTableSvc,
		workerID: "model-materialization-" + uuid.NewString(), workerDone: make(chan struct{}),
	}
}

func (s *MaterializationService) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		workerCtx, cancel := context.WithCancel(ctx)
		s.workerCancel = cancel
		go s.workerLoop(workerCtx)
	})
}

func (s *MaterializationService) Stop() {
	if s.workerCancel != nil {
		s.workerCancel()
		<-s.workerDone
	}
}

func (s *MaterializationService) Prepare(
	ctx context.Context,
	logicalTableID, tenantID, userID int64,
	triggerType, source string,
	parentExecutionID *string,
) (string, string, error) {
	if parentExecutionID == nil || strings.TrimSpace(*parentExecutionID) == "" {
		return "", "", apperrors.Validation("materialization_parent_execution_required", modeli18n.MsgMaterializationInvalid)
	}
	triggerType, source, err := normalizeMaterializationTrigger(triggerType, source)
	if err != nil {
		return "", "", err
	}
	table, fields, locator, targetName, fingerprint, err := s.loadApprovedDefinition(logicalTableID, tenantID)
	if err != nil {
		return "", "", err
	}
	if materializationHasPartitioning(table.Materialization) {
		return "", "", apperrors.Validation("materialization_partition_unsupported", modeli18n.MsgMaterializationInvalid)
	}
	batchID := uuid.NewString()
	executionID := uuid.NewString()
	stagingName := materializationTemporaryName(targetName, "staging", batchID)
	now := time.Now().UTC()
	batch := &models.MaterializationBatch{
		ID: batchID, TenantID: tenantID, LogicalTableID: logicalTableID,
		LogicalTableVersion: table.Version, EngineID: int64(locator.EngineID),
		TargetParentLocator: locator.ToURI(),
		TargetName:          targetName, StagingName: stagingName, SchemaFingerprint: fingerprint,
		Status: models.MaterializationBatchPreparing, PrepareExecutionID: executionID,
		CreatedAt: now, UpdatedAt: now,
	}
	execution := newMaterializationExecution(
		executionID, tenantID, userID, logicalTableID, table.Name,
		commonExecution.TaskTypeMaterializationPrepare, triggerType, source, parentExecutionID,
		commonModels.JSONMap{"schema_version": "model.materialization/v1", "batch_id": batchID},
	)
	if err := s.repo.CreatePrepareExecution(ctx, batch, execution, table.Name); err != nil {
		return "", "", materializationResourceError(err)
	}
	if err := s.authorizeExecution(ctx, tenantID, executionID, locator.EngineID, *parentExecutionID); err != nil {
		_ = s.repo.FailPendingExecution(ctx, tenantID, executionID, batchID, commonExecution.TaskTypeMaterializationPrepare, "model.materialization.authorization_issue_failed")
		return "", "", apperrors.Wrap(apperrors.KindUnavailable, "materialization_authorization_failed", modeli18n.MsgMaterializationUnavailable, err)
	}
	_ = fields // validation and fingerprint are intentionally completed before queueing.
	return executionID, batchID, nil
}

func (s *MaterializationService) Publish(
	ctx context.Context,
	logicalTableID, tenantID, userID int64,
	triggerType, source string,
	parentExecutionID *string,
) (string, error) {
	if parentExecutionID == nil || strings.TrimSpace(*parentExecutionID) == "" {
		return "", apperrors.Validation("materialization_parent_execution_required", modeli18n.MsgMaterializationInvalid)
	}
	triggerType, source, err := normalizeMaterializationTrigger(triggerType, source)
	if err != nil {
		return "", err
	}
	executionID := uuid.NewString()
	execution := newMaterializationExecution(
		executionID, tenantID, userID, logicalTableID, "",
		commonExecution.TaskTypeMaterializationPublish, triggerType, source, parentExecutionID,
		commonModels.JSONMap{"schema_version": "model.materialization/v1"},
	)
	batch, err := s.repo.CreatePublishExecution(ctx, tenantID, logicalTableID, parentExecutionID, execution)
	if err != nil {
		return "", materializationResourceError(err)
	}
	if err := s.authorizeExecution(ctx, tenantID, executionID, uint(batch.EngineID), *parentExecutionID); err != nil {
		_ = s.repo.FailPendingExecution(ctx, tenantID, executionID, batch.ID, commonExecution.TaskTypeMaterializationPublish, "model.materialization.authorization_issue_failed")
		return "", apperrors.Wrap(apperrors.KindUnavailable, "materialization_authorization_failed", modeli18n.MsgMaterializationUnavailable, err)
	}
	return executionID, nil
}

func (s *MaterializationService) PublishGroup(
	ctx context.Context,
	groupID, tenantID, userID int64,
	expectedGroupID, expectedGroupVersion int64,
	triggerType, source string,
	parentExecutionID *string,
) (string, error) {
	if groupID <= 0 || expectedGroupID != groupID || expectedGroupVersion <= 0 || parentExecutionID == nil || strings.TrimSpace(*parentExecutionID) == "" {
		return "", apperrors.Validation("materialization_parent_execution_required", modeli18n.MsgMaterializationInvalid)
	}
	parentID, err := uuid.Parse(strings.TrimSpace(*parentExecutionID))
	if err != nil {
		return "", apperrors.Validation("materialization_parent_execution_invalid", modeli18n.MsgMaterializationInvalid)
	}
	triggerType, source, err = normalizeMaterializationTrigger(triggerType, source)
	if err != nil {
		return "", err
	}
	executionID := uuid.NewString()
	parent := parentID.String()
	execution := newMaterializationExecution(
		executionID, tenantID, userID, groupID, "",
		commonExecution.TaskTypeMaterializationGroupPublish, triggerType, source, &parent,
		commonModels.JSONMap{"schema_version": "model.materialization-group/v1"},
	)
	_, batches, err := s.repo.CreateGroupPublishExecution(ctx, tenantID, groupID, expectedGroupVersion, parent, execution)
	if err != nil {
		return "", materializationResourceError(err)
	}
	batchIDs := make([]string, 0, len(batches))
	for _, batch := range batches {
		batchIDs = append(batchIDs, batch.ID)
	}
	if err := s.authorizeExecution(ctx, tenantID, executionID, uint(batches[0].EngineID), parent); err != nil {
		_ = s.repo.FailPendingGroupExecution(ctx, tenantID, executionID, batchIDs, "model.materialization.authorization_issue_failed")
		return "", apperrors.Wrap(apperrors.KindUnavailable, "materialization_authorization_failed", modeli18n.MsgMaterializationUnavailable, err)
	}
	return executionID, nil
}

func (s *MaterializationService) Seal(
	ctx context.Context,
	logicalTableID, tenantID, userID int64,
	batchID, writerExecutionID, targetLocator string,
	triggerType, source string,
	parentExecutionID *string,
) (string, error) {
	if logicalTableID <= 0 || tenantID <= 0 || parentExecutionID == nil {
		return "", apperrors.Validation("materialization_seal_invalid", modeli18n.MsgMaterializationInvalid)
	}
	parsedParent, err := uuid.Parse(strings.TrimSpace(*parentExecutionID))
	if err != nil {
		return "", apperrors.Validation("materialization_parent_execution_invalid", modeli18n.MsgMaterializationInvalid)
	}
	parsedBatch, err := uuid.Parse(strings.TrimSpace(batchID))
	if err != nil {
		return "", apperrors.Validation("materialization_batch_invalid", modeli18n.MsgMaterializationInvalid)
	}
	parsedWriter, err := uuid.Parse(strings.TrimSpace(writerExecutionID))
	if err != nil {
		return "", apperrors.Validation("materialization_writer_execution_invalid", modeli18n.MsgMaterializationInvalid)
	}
	target, err := resourcetree.ParseURI(strings.TrimSpace(targetLocator))
	if err != nil || target.Type != resourcetree.TypeTable {
		return "", apperrors.Validation("materialization_target_invalid", modeli18n.MsgMaterializationInvalid)
	}
	triggerType, source, err = normalizeMaterializationTrigger(triggerType, source)
	if err != nil {
		return "", err
	}
	batch, err := s.repo.GetByID(ctx, parsedBatch.String(), tenantID)
	if err != nil {
		return "", materializationResourceError(err)
	}
	if batch.LogicalTableID != logicalTableID || batch.Status != models.MaterializationBatchPrepared {
		return "", apperrors.Conflict("materialization_batch_not_prepared", modeli18n.MsgMaterializationConflict)
	}
	parentLocator, err := resourcetree.ParseURI(batch.TargetParentLocator)
	if err != nil {
		return "", apperrors.Conflict("materialization_batch_target_invalid", modeli18n.MsgMaterializationConflict)
	}
	expectedTarget := (&resourcetree.ResourceLocator{
		EngineID: parentLocator.EngineID,
		Path:     append(append([]string(nil), parentLocator.Path...), batch.StagingName),
		Type:     resourcetree.TypeTable,
	}).ToURI()
	if target.ToURI() != expectedTarget {
		return "", apperrors.Conflict("materialization_seal_target_mismatch", modeli18n.MsgMaterializationConflict)
	}
	executionID := uuid.NewString()
	parent := parsedParent.String()
	execution := newMaterializationExecution(
		executionID, tenantID, userID, logicalTableID, "",
		commonExecution.TaskTypeMaterializationSeal, triggerType, source, &parent,
		commonModels.JSONMap{
			"schema_version":      "model.materialization/v1",
			"batch_id":            batch.ID,
			"writer_execution_id": parsedWriter.String(),
			"target_locator":      expectedTarget,
		},
	)
	batch, err = s.repo.CreateSealExecution(ctx, repository.CreateSealExecutionInput{
		TenantID: tenantID, LogicalTableID: logicalTableID, BatchID: batch.ID,
		ParentExecutionID: parent, WriterExecutionID: parsedWriter.String(), TargetLocator: expectedTarget,
	}, execution)
	if err != nil {
		return "", materializationResourceError(err)
	}
	if err := s.authorizeExecution(ctx, tenantID, executionID, uint(batch.EngineID), parent); err != nil {
		_ = s.repo.FailPendingExecution(ctx, tenantID, executionID, batch.ID, commonExecution.TaskTypeMaterializationSeal, "model.materialization.authorization_issue_failed")
		return "", apperrors.Wrap(apperrors.KindUnavailable, "materialization_authorization_failed", modeli18n.MsgMaterializationUnavailable, err)
	}
	return executionID, nil
}

func newMaterializationExecution(
	executionID string,
	tenantID, userID, logicalTableID int64,
	tableName, taskType, triggerType, source string,
	parentExecutionID *string,
	config commonModels.JSONMap,
) *commonExecution.TaskExecution {
	now := time.Now().UTC()
	sourceTaskID := strconv.FormatInt(logicalTableID, 10)
	execution := &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: int(tenantID), Module: commonExecution.ModuleModel,
		TaskType: taskType, Source: source, SourceTaskID: &sourceTaskID,
		ParentExecutionID: parentExecutionID, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status: commonExecution.ExecutionStatusPending, TriggerType: triggerType,
		ExecutionConfig: config, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
	}
	if tableName != "" {
		execution.SourceTaskName = &tableName
	}
	if userID > 0 {
		value := int(userID)
		execution.TriggeredBy = &value
	}
	return execution
}

func normalizeMaterializationTrigger(triggerType, source string) (string, string, error) {
	normalized, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil || (normalized != commonExecution.TriggerTypeManual && normalized != commonExecution.TriggerTypeScheduled) {
		return "", "", apperrors.Validation("materialization_trigger_invalid", modeli18n.MsgMaterializationInvalid)
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = commonExecution.ModuleModel
	}
	return normalized, source, nil
}

func (s *MaterializationService) authorizeExecution(
	ctx context.Context,
	tenantID int64,
	executionID string,
	engineID uint,
	parentExecutionID string,
) error {
	issued, err := s.systemClient.WithTenantID(uint(tenantID)).IssueExecutionAuthorizationFromExecution(ctx, commonClient.IssueExecutionAuthorizationFromExecutionRequest{
		ParentExecutionID: parentExecutionID, Audience: commonExecution.AudienceModel,
		ExecutionID: executionID, Accesses: []commonClient.ExecutionEngineAccessScope{{
			EngineID: strconv.FormatUint(uint64(engineID), 10), Effects: []string{"read", "ddl"},
		}}, ExpiresIn: materializationAuthorizationTTL,
	})
	if err != nil {
		return err
	}
	fields, err := commonClient.TaskExecutionAuthorizationFields(issued)
	if err != nil {
		return err
	}
	if err := s.repo.AttachAuthorization(ctx, tenantID, executionID, fields); err != nil {
		return err
	}
	return nil
}

func (s *MaterializationService) loadApprovedDefinition(
	logicalTableID, tenantID int64,
) (*models.LogicalTable, []models.LogicalField, *resourcetree.ResourceLocator, string, string, error) {
	table, err := s.logicalTableRepo.GetByID(logicalTableID, tenantID)
	if err != nil {
		return nil, nil, nil, "", "", materializationResourceError(err)
	}
	if table.Status != "approved" {
		return nil, nil, nil, "", "", apperrors.Conflict("materialization_table_not_approved", modeli18n.MsgMaterializationConflict)
	}
	fields, err := s.logicalTableRepo.GetFields(logicalTableID)
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	if len(fields) == 0 || validateMaterialization(table, fields) != nil {
		return nil, nil, nil, "", "", apperrors.Validation("materialization_definition_invalid", modeli18n.MsgMaterializationInvalid)
	}
	locatorText, ok := materializationString(table.Materialization, "target_parent_locator")
	if !ok || locatorText == "" {
		return nil, nil, nil, "", "", apperrors.Validation("materialization_target_missing", modeli18n.MsgMaterializationInvalid)
	}
	targetName, ok := materializationString(table.Materialization, "target_name")
	if !ok || targetName == "" {
		return nil, nil, nil, "", "", apperrors.Validation("materialization_target_missing", modeli18n.MsgMaterializationInvalid)
	}
	locator, err := resourcetree.ParseURI(locatorText)
	if err != nil || locator.EngineID == 0 || locator.Type != resourcetree.TypeSchema || len(locator.Path) == 0 {
		return nil, nil, nil, "", "", apperrors.Validation("materialization_target_invalid", modeli18n.MsgMaterializationInvalid)
	}
	fingerprint, err := materializationSchemaFingerprint(table, fields)
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	return table, fields, locator, targetName, fingerprint, nil
}

func materializationSchemaFingerprint(table *models.LogicalTable, fields []models.LogicalField) (string, error) {
	type fieldShape struct {
		ColumnName   string `json:"column_name"`
		DataType     string `json:"data_type"`
		Length       *int   `json:"length,omitempty"`
		Nullable     bool   `json:"nullable"`
		PrimaryKey   bool   `json:"primary_key"`
		DefaultValue string `json:"default_value,omitempty"`
	}
	shape := struct {
		Fields        []fieldShape `json:"fields"`
		PartitionBy   string       `json:"partition_by,omitempty"`
		PartitionType string       `json:"partition_type,omitempty"`
	}{Fields: make([]fieldShape, 0, len(fields))}
	for _, field := range fields {
		shape.Fields = append(shape.Fields, fieldShape{
			ColumnName: field.ColumnName, DataType: field.DataType, Length: field.Length,
			Nullable: field.Nullable, PrimaryKey: field.IsPK, DefaultValue: field.DefaultValue,
		})
	}
	shape.PartitionBy, _ = materializationString(table.Materialization, "partition_by")
	shape.PartitionType, _ = materializationString(table.Materialization, "partition_type")
	encoded, err := json.Marshal(shape)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func materializationTemporaryName(targetName, purpose, batchID string) string {
	suffix := "__addp_" + purpose + "_" + strings.ReplaceAll(batchID, "-", "")[:12]
	maxPrefix := 63 - len(suffix)
	if len(targetName) > maxPrefix {
		targetName = targetName[:maxPrefix]
	}
	return targetName + suffix
}

func (s *MaterializationService) workerLoop(ctx context.Context) {
	defer close(s.workerDone)
	ticker := time.NewTicker(materializationWorkerPoll)
	defer ticker.Stop()
	recoveryTicker := time.NewTicker(materializationWorkerLease / 2)
	defer recoveryTicker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if s.processPending(ctx, commonExecution.TaskTypeMaterializationPrepare) ||
			s.processPending(ctx, commonExecution.TaskTypeMaterializationSeal) ||
			s.processPending(ctx, commonExecution.TaskTypeMaterializationPublish) ||
			s.processPending(ctx, commonExecution.TaskTypeMaterializationGroupPublish) {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case now := <-recoveryTicker.C:
			for _, taskType := range []string{
				commonExecution.TaskTypeMaterializationPrepare,
				commonExecution.TaskTypeMaterializationSeal,
				commonExecution.TaskTypeMaterializationPublish,
				commonExecution.TaskTypeMaterializationGroupPublish,
			} {
				if err := s.repo.RecoverExpiredExecutions(ctx, taskType, now.UTC()); err != nil && ctx.Err() == nil {
					log.Printf("model materialization lease recovery failed: %v", err)
				}
			}
		}
	}
}

func (s *MaterializationService) processPending(ctx context.Context, taskType string) bool {
	if taskType == commonExecution.TaskTypeMaterializationGroupPublish {
		return s.processPendingGroup(ctx)
	}
	execution, batch, err := s.repo.ClaimPendingExecution(ctx, taskType, s.workerID, time.Now().UTC(), materializationWorkerLease)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("model materialization execution claim failed: %v", err)
		}
		return false
	}
	if execution == nil || batch == nil {
		return false
	}
	lease, err := commonExecution.LeaseFromExecution(*execution)
	if err != nil {
		log.Printf("model materialization execution %s has invalid lease: %v", execution.ExecutionID, err)
		return true
	}
	var metadata commonModels.JSONMap
	switch taskType {
	case commonExecution.TaskTypeMaterializationPrepare:
		metadata, err = s.executePrepare(ctx, execution, batch)
	case commonExecution.TaskTypeMaterializationSeal:
		metadata, err = s.executeSeal(ctx, execution, batch)
	default:
		metadata, err = s.executePublish(ctx, execution, batch)
	}
	executionStatus := commonExecution.ExecutionStatusSuccess
	batchStatus := models.MaterializationBatchPrepared
	var errorDetails commonModels.JSONMap
	if taskType == commonExecution.TaskTypeMaterializationSeal {
		batchStatus = models.MaterializationBatchSealed
	} else if taskType == commonExecution.TaskTypeMaterializationPublish {
		batchStatus = models.MaterializationBatchPublished
	}
	if err != nil {
		executionStatus = commonExecution.ExecutionStatusFailed
		switch taskType {
		case commonExecution.TaskTypeMaterializationPrepare:
			batchStatus = models.MaterializationBatchFailed
		case commonExecution.TaskTypeMaterializationSeal:
			batchStatus = models.MaterializationBatchPrepared
		case commonExecution.TaskTypeMaterializationPublish:
			batchStatus = models.MaterializationBatchSealed
		}
		errorDetails = commonModels.JSONMap{"code": "model.materialization.execution_failed", "message": "controlled materialization failed"}
		log.Printf("model materialization execution %s failed: %v", execution.ExecutionID, err)
	}
	if completeErr := s.repo.CompleteExecution(ctx, lease, batch.ID, taskType, executionStatus, batchStatus, metadata, errorDetails); completeErr != nil {
		log.Printf("model materialization execution %s completion failed: %v", execution.ExecutionID, completeErr)
	}
	return true
}

func (s *MaterializationService) processPendingGroup(ctx context.Context) bool {
	execution, batches, err := s.repo.ClaimPendingGroupExecution(ctx, s.workerID, time.Now().UTC(), materializationWorkerLease)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("model materialization group execution claim failed: %v", err)
		}
		return false
	}
	if execution == nil {
		return false
	}
	lease, err := commonExecution.LeaseFromExecution(*execution)
	if err != nil {
		log.Printf("model materialization group execution %s has invalid lease: %v", execution.ExecutionID, err)
		return true
	}
	metadata, err := s.executeGroupPublish(ctx, execution, batches)
	executionStatus := commonExecution.ExecutionStatusSuccess
	var errorDetails commonModels.JSONMap
	if err != nil {
		executionStatus = commonExecution.ExecutionStatusFailed
		errorDetails = commonModels.JSONMap{
			"code": "model.materialization_group.execution_failed", "message": "controlled materialization group publish failed",
		}
		log.Printf("model materialization group execution %s failed: %v", execution.ExecutionID, err)
	}
	batchIDs := make([]string, 0, len(batches))
	for _, batch := range batches {
		batchIDs = append(batchIDs, batch.ID)
	}
	if completeErr := s.repo.CompleteGroupExecution(ctx, lease, batchIDs, executionStatus, metadata, errorDetails); completeErr != nil {
		log.Printf("model materialization group execution %s completion failed: %v", execution.ExecutionID, completeErr)
	}
	return true
}

func (s *MaterializationService) executionEngine(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	batch *models.MaterializationBatch,
) (*commonModels.Engine, error) {
	if execution.ExecutionAuthorizationID == nil {
		return nil, errors.New("execution authorization is missing")
	}
	access, err := s.systemClient.WithTenantID(uint(batch.TenantID)).GetExecutionEngineAccess(
		ctx,
		strconv.FormatInt(*execution.ExecutionAuthorizationID, 10),
		commonClient.ExecutionEngineAccessRequest{
			ExecutionID:     execution.ExecutionID,
			EngineID:        strconv.FormatInt(batch.EngineID, 10),
			RequiredEffects: []string{"read", "ddl"},
		},
	)
	if err != nil {
		return nil, err
	}
	engineType := strings.ToLower(strings.TrimSpace(access.Engine.EngineType))
	if engineType != "postgres" && engineType != "postgresql" && engineType != "postgis" {
		return nil, fmt.Errorf("controlled logical-table materialization only supports PostgreSQL/PostGIS")
	}
	return access.Engine, nil
}

func (s *MaterializationService) executePrepare(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	batch *models.MaterializationBatch,
) (commonModels.JSONMap, error) {
	engine, err := s.executionEngine(ctx, execution, batch)
	if err != nil {
		return nil, err
	}
	table, fields, parentLocator, _, fingerprint, err := s.loadApprovedDefinition(batch.LogicalTableID, batch.TenantID)
	if err != nil {
		return nil, err
	}
	if table.Version != batch.LogicalTableVersion || fingerprint != batch.SchemaFingerprint {
		return nil, fmt.Errorf("logical table changed after materialization was queued")
	}
	schemaName, err := materializationSchemaName(batch.TargetParentLocator)
	if err != nil {
		return nil, err
	}
	pool, err := materializationPool(engine)
	if err != nil {
		return nil, err
	}
	reclaimable, err := s.repo.ListReclaimableStagingBatches(ctx, *batch)
	if err != nil {
		return nil, err
	}
	var expectedTargetMarker *string
	err = pool.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reclaimMaterializationStaging(tx, schemaName, reclaimable); err != nil {
			return err
		}
		targetComment, targetExists, err := physicalTableComment(tx, schemaName, batch.TargetName)
		if err != nil {
			return err
		}
		if targetExists {
			if !materializationMarkerOwnedBy(targetComment, batch.LogicalTableID) {
				return fmt.Errorf("target table is not managed by the same logical table")
			}
			marker := targetComment
			expectedTargetMarker = &marker
		}
		stagingComment, stagingExists, err := physicalTableComment(tx, schemaName, batch.StagingName)
		if err != nil {
			return err
		}
		expectedMarker := materializationMarker(batch.LogicalTableID, batch.SchemaFingerprint, batch.ID)
		if stagingExists {
			if stagingComment == expectedMarker {
				return nil
			}
			return fmt.Errorf("materialization staging table name is already occupied")
		}
		preview := *table
		preview.Materialization = models.JSONB{
			"target_parent_locator": batch.TargetParentLocator,
			"target_name":           batch.StagingName,
		}
		if err := tx.Exec(s.logicalTableSvc.generatePostgreSQLDDL(&preview, fields)).Error; err != nil {
			return err
		}
		return tx.Exec("COMMENT ON TABLE " + qualifiedIdentifier(schemaName, batch.StagingName) + " IS " + quoteSQLLiteral(expectedMarker)).Error
	})
	if err != nil {
		return nil, err
	}
	var expectedTargetMarkerValue interface{}
	if expectedTargetMarker != nil {
		expectedTargetMarkerValue = *expectedTargetMarker
	}
	return commonModels.JSONMap{
		"schema_version":         "model.materialization/v1",
		"expected_target_marker": expectedTargetMarkerValue,
		"outputs": commonModels.JSONMap{
			"batch_id": batch.ID,
			"staging_locator": (&resourcetree.ResourceLocator{
				EngineID: parentLocator.EngineID,
				Path:     append(append([]string(nil), parentLocator.Path...), batch.StagingName),
				Type:     resourcetree.TypeTable,
			}).ToURI(),
		},
	}, nil
}

func reclaimMaterializationStaging(
	tx *gorm.DB,
	schemaName string,
	batches []models.MaterializationBatch,
) error {
	for index := range batches {
		batch := batches[index]
		if strings.TrimSpace(batch.StagingName) == "" {
			return errors.New("reclaimable materialization batch has no staging table")
		}
		comment, exists, err := physicalTableComment(tx, schemaName, batch.StagingName)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		expectedMarker := materializationMarker(batch.LogicalTableID, batch.SchemaFingerprint, batch.ID)
		if comment != expectedMarker {
			return errors.New("reclaimable materialization staging ownership marker is invalid")
		}
		if err := tx.Exec("DROP TABLE " + qualifiedIdentifier(schemaName, batch.StagingName)).Error; err != nil {
			return err
		}
	}
	return nil
}

type materializationPhysicalColumn struct {
	ColumnName   string `gorm:"column:column_name"`
	DataType     string `gorm:"column:data_type"`
	Nullable     bool   `gorm:"column:nullable"`
	IsPrimaryKey bool   `gorm:"column:is_primary_key"`
}

func (s *MaterializationService) executeSeal(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	batch *models.MaterializationBatch,
) (commonModels.JSONMap, error) {
	engine, err := s.executionEngine(ctx, execution, batch)
	if err != nil {
		return nil, err
	}
	table, fields, parentLocator, _, fingerprint, err := s.loadApprovedDefinition(batch.LogicalTableID, batch.TenantID)
	if err != nil {
		return nil, err
	}
	if table.Version != batch.LogicalTableVersion || fingerprint != batch.SchemaFingerprint ||
		int64(parentLocator.EngineID) != batch.EngineID || parentLocator.ToURI() != batch.TargetParentLocator {
		return nil, errors.New("logical table changed before materialization seal")
	}
	schemaName, err := materializationSchemaName(batch.TargetParentLocator)
	if err != nil {
		return nil, err
	}
	pool, err := materializationPool(engine)
	if err != nil {
		return nil, err
	}
	expectedMarker := materializationMarker(batch.LogicalTableID, batch.SchemaFingerprint, batch.ID)
	var physical []materializationPhysicalColumn
	err = pool.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		comment, exists, err := physicalTableComment(tx, schemaName, batch.StagingName)
		if err != nil {
			return err
		}
		if !exists || comment != expectedMarker {
			return errors.New("materialization staging ownership marker is invalid")
		}
		return tx.Raw(`
			SELECT a.attname AS column_name,
			       pg_catalog.format_type(a.atttypid, a.atttypmod) AS data_type,
			       NOT a.attnotnull AS nullable,
			       EXISTS (
			         SELECT 1 FROM pg_catalog.pg_index i
			         WHERE i.indrelid = c.oid AND i.indisprimary AND a.attnum = ANY(i.indkey)
			       ) AS is_primary_key
			FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
			WHERE n.nspname = ? AND c.relname = ? AND c.relkind IN ('r', 'p')
			  AND a.attnum > 0 AND NOT a.attisdropped
			ORDER BY a.attnum`, schemaName, batch.StagingName).Scan(&physical).Error
	})
	if err != nil {
		return nil, err
	}
	if len(physical) != len(fields) {
		return nil, errors.New("materialization staging column count does not match logical schema")
	}
	for index, field := range fields {
		column := physical[index]
		expectedType := normalizePostgreSQLType(s.logicalTableSvc.mapDataTypeToPostgreSQL(field.DataType, field.Length))
		if column.ColumnName != field.ColumnName || normalizePostgreSQLType(column.DataType) != expectedType ||
			column.Nullable != field.Nullable || column.IsPrimaryKey != field.IsPK {
			return nil, fmt.Errorf("materialization staging column %q does not match logical schema", field.ColumnName)
		}
	}
	stagingLocator := (&resourcetree.ResourceLocator{
		EngineID: parentLocator.EngineID,
		Path:     append(append([]string(nil), parentLocator.Path...), batch.StagingName),
		Type:     resourcetree.TypeTable,
	}).ToURI()
	return commonModels.JSONMap{
		"schema_version": "model.materialization/v1",
		"outputs": commonModels.JSONMap{
			"batch_id":           batch.ID,
			"staging_locator":    stagingLocator,
			"schema_fingerprint": batch.SchemaFingerprint,
		},
	}, nil
}

func normalizePostgreSQLType(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	value = strings.ReplaceAll(value, "varchar(", "character varying(")
	switch value {
	case "timestamp":
		return "timestamp without time zone"
	case "double precision":
		return value
	}
	return value
}

func (s *MaterializationService) executePublish(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	batch *models.MaterializationBatch,
) (commonModels.JSONMap, error) {
	targetLocators, err := s.publishBatches(ctx, execution, []models.MaterializationBatch{*batch})
	if err != nil {
		return nil, err
	}
	return commonModels.JSONMap{
		"schema_version": "model.materialization/v1",
		"outputs": commonModels.JSONMap{
			"batch_id":       batch.ID,
			"target_locator": targetLocators[0],
		},
	}, nil
}

func (s *MaterializationService) executeGroupPublish(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	batches []models.MaterializationBatch,
) (commonModels.JSONMap, error) {
	if s.groupService == nil || execution == nil || len(batches) == 0 || execution.SourceTaskID == nil {
		return nil, errors.New("materialization group execution is incomplete")
	}
	groupID, err := strconv.ParseInt(strings.TrimSpace(*execution.SourceTaskID), 10, 64)
	if err != nil || groupID <= 0 {
		return nil, errors.New("materialization group execution has an invalid task identity")
	}
	groupVersion, err := executionConfigPositiveInt64(execution.ExecutionConfig, "group_version")
	if err != nil {
		return nil, err
	}
	group, err := s.groupService.Get(ctx, int64(execution.TenantID), groupID)
	if err != nil {
		return nil, err
	}
	if group.Version != groupVersion || len(group.Members) != len(batches) {
		return nil, errors.New("materialization group definition changed after publish was queued")
	}
	for index, member := range group.Members {
		if member.LogicalTableID != batches[index].LogicalTableID {
			return nil, errors.New("materialization group members changed after publish was queued")
		}
	}
	targetLocators, err := s.publishBatches(ctx, execution, batches)
	if err != nil {
		return nil, err
	}
	batchIDs := make([]string, 0, len(batches))
	for _, batch := range batches {
		batchIDs = append(batchIDs, batch.ID)
	}
	return commonModels.JSONMap{
		"schema_version": "model.materialization-group/v1",
		"outputs":        commonModels.JSONMap{"batch_ids": batchIDs, "target_locators": targetLocators},
	}, nil
}

type materializationPublishCandidate struct {
	batch          models.MaterializationBatch
	schemaName     string
	expectedMarker string
	backupName     string
	targetExists   bool
	alreadyDone    bool
}

func (s *MaterializationService) publishBatches(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	batches []models.MaterializationBatch,
) ([]string, error) {
	if len(batches) == 0 {
		return nil, errors.New("materialization publish has no batches")
	}
	engine, err := s.executionEngine(ctx, execution, &batches[0])
	if err != nil {
		return nil, err
	}
	pool, err := materializationPool(engine)
	if err != nil {
		return nil, err
	}
	candidates := make([]materializationPublishCandidate, 0, len(batches))
	targetLocators := make([]string, 0, len(batches))
	for _, batch := range batches {
		if batch.EngineID != batches[0].EngineID || batch.Status != models.MaterializationBatchPublishing ||
			batch.WriterExecutionID == nil || batch.SealExecutionID == nil || strings.TrimSpace(batch.StagingName) == "" {
			return nil, errors.New("materialization publish batch set is incomplete or spans engines")
		}
		table, _, parentLocator, targetName, fingerprint, err := s.loadApprovedDefinition(batch.LogicalTableID, batch.TenantID)
		if err != nil {
			return nil, err
		}
		if table.Version != batch.LogicalTableVersion || fingerprint != batch.SchemaFingerprint ||
			int64(parentLocator.EngineID) != batch.EngineID || parentLocator.ToURI() != batch.TargetParentLocator || targetName != batch.TargetName {
			return nil, errors.New("logical table changed after materialization publish was queued")
		}
		schemaName, err := materializationSchemaName(batch.TargetParentLocator)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, materializationPublishCandidate{
			batch:          batch,
			schemaName:     schemaName,
			expectedMarker: materializationMarker(batch.LogicalTableID, batch.SchemaFingerprint, batch.ID),
			backupName:     materializationTemporaryName(batch.TargetName, "backup", batch.ID),
		})
		targetLocators = append(targetLocators, (&resourcetree.ResourceLocator{
			EngineID: parentLocator.EngineID,
			Path:     append(append([]string(nil), parentLocator.Path...), batch.TargetName),
			Type:     resourcetree.TypeTable,
		}).ToURI())
	}
	if err := publishMaterializationCandidates(ctx, pool, candidates); err != nil {
		return nil, err
	}
	return targetLocators, nil
}

func publishMaterializationCandidates(
	ctx context.Context,
	pool *gorm.DB,
	candidates []materializationPublishCandidate,
) error {
	if pool == nil || len(candidates) == 0 {
		return errors.New("materialization publish candidates are missing")
	}
	return pool.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		alreadyDone := 0
		for index := range candidates {
			candidate := &candidates[index]
			targetComment, targetExists, err := physicalTableComment(tx, candidate.schemaName, candidate.batch.TargetName)
			if err != nil {
				return err
			}
			candidate.targetExists = targetExists
			stagingComment, stagingExists, err := physicalTableComment(tx, candidate.schemaName, candidate.batch.StagingName)
			if err != nil {
				return err
			}
			if !stagingExists {
				if targetExists && targetComment == candidate.expectedMarker {
					candidate.alreadyDone = true
					alreadyDone++
					continue
				}
				return errors.New("prepared staging table is missing")
			}
			if stagingComment != candidate.expectedMarker {
				return errors.New("staging table ownership marker does not match batch")
			}
			if candidate.batch.ExpectedTargetMarker == nil {
				if targetExists {
					return errors.New("materialization target appeared after prepare")
				}
			} else if !targetExists || targetComment != *candidate.batch.ExpectedTargetMarker {
				return errors.New("materialization target changed after prepare")
			}
			if targetExists {
				if _, backupExists, err := physicalTableComment(tx, candidate.schemaName, candidate.backupName); err != nil {
					return err
				} else if backupExists {
					return errors.New("materialization backup table name is already occupied")
				}
			}
		}
		if alreadyDone == len(candidates) {
			return nil
		}
		if alreadyDone != 0 {
			return errors.New("materialization group physical state is partially published")
		}
		for index := range candidates {
			candidate := &candidates[index]
			if candidate.targetExists {
				if err := tx.Exec("ALTER TABLE " + qualifiedIdentifier(candidate.schemaName, candidate.batch.TargetName) + " RENAME TO " + quoteIdentifier(candidate.backupName)).Error; err != nil {
					return err
				}
			}
		}
		for index := range candidates {
			candidate := &candidates[index]
			if err := tx.Exec("ALTER TABLE " + qualifiedIdentifier(candidate.schemaName, candidate.batch.StagingName) + " RENAME TO " + quoteIdentifier(candidate.batch.TargetName)).Error; err != nil {
				return err
			}
		}
		for index := range candidates {
			candidate := &candidates[index]
			if candidate.targetExists {
				if err := tx.Exec("DROP TABLE " + qualifiedIdentifier(candidate.schemaName, candidate.backupName)).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func executionConfigPositiveInt64(config commonModels.JSONMap, key string) (int64, error) {
	if config == nil {
		return 0, fmt.Errorf("materialization execution config %s is missing", key)
	}
	var value int64
	switch typed := config[key].(type) {
	case int:
		value = int64(typed)
	case int64:
		value = typed
	case float64:
		if typed == float64(int64(typed)) {
			value = int64(typed)
		}
	case json.Number:
		value, _ = typed.Int64()
	}
	if value <= 0 {
		return 0, fmt.Errorf("materialization execution config %s is invalid", key)
	}
	return value, nil
}

func materializationSchemaName(locatorText string) (string, error) {
	locator, err := resourcetree.ParseURI(locatorText)
	if err != nil || locator.Type != resourcetree.TypeSchema || len(locator.Path) == 0 {
		return "", fmt.Errorf("materialization target parent locator is invalid")
	}
	return locator.Path[len(locator.Path)-1], nil
}

func materializationPool(engine *commonModels.Engine) (*gorm.DB, error) {
	if engine == nil {
		return nil, errors.New("materialization engine is missing")
	}
	return plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
		ID: engine.ID, EngineType: engine.EngineType, ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}, plugin.DefaultPoolConfig())
}

func physicalTableComment(tx *gorm.DB, schemaName, tableName string) (string, bool, error) {
	qualified := schemaName + "." + tableName
	var relation sql.NullString
	if err := tx.Raw("SELECT to_regclass(?)::text", qualified).Scan(&relation).Error; err != nil {
		return "", false, err
	}
	if !relation.Valid || relation.String == "" {
		return "", false, nil
	}
	var comment sql.NullString
	if err := tx.Raw("SELECT obj_description(to_regclass(?), 'pg_class')", qualified).Scan(&comment).Error; err != nil {
		return "", false, err
	}
	return comment.String, true, nil
}

func materializationMarker(logicalTableID int64, fingerprint, batchID string) string {
	return materializationMarkerPrefix + strconv.FormatInt(logicalTableID, 10) + ":" + fingerprint + ":" + batchID
}

type materializationOwnershipMarker struct {
	LogicalTableID    int64
	SchemaFingerprint string
	BatchID           string
}

func parseMaterializationMarker(marker string) (materializationOwnershipMarker, bool) {
	if !strings.HasPrefix(marker, materializationMarkerPrefix) {
		return materializationOwnershipMarker{}, false
	}
	remainder := strings.TrimPrefix(marker, materializationMarkerPrefix)
	parts := strings.SplitN(remainder, ":", 3)
	if len(parts) != 3 || len(parts[1]) != 64 || strings.TrimSpace(parts[2]) == "" {
		return materializationOwnershipMarker{}, false
	}
	logicalTableID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || logicalTableID <= 0 {
		return materializationOwnershipMarker{}, false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return materializationOwnershipMarker{}, false
	}
	return materializationOwnershipMarker{
		LogicalTableID: logicalTableID, SchemaFingerprint: parts[1], BatchID: parts[2],
	}, true
}

func materializationMarkerOwnedBy(marker string, logicalTableID int64) bool {
	parsed, ok := parseMaterializationMarker(marker)
	return ok && parsed.LogicalTableID == logicalTableID
}

func qualifiedIdentifier(schemaName, tableName string) string {
	return quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)
}

func quoteSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func materializationResourceError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, commonAPI.ErrNotFound) {
		return apperrors.NotFound("materialization_resource_not_found", modeli18n.MsgMaterializationNotFound)
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || errors.Is(err, commonAPI.ErrConflict) {
		return apperrors.Conflict("materialization_state_conflict", modeli18n.MsgMaterializationConflict)
	}
	return err
}
