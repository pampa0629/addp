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
	"strconv"
	"strings"
	"sync"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

const (
	materializationAuthorizationTTL = int64(3600)
	materializationWorkerLease      = 2 * time.Minute
	materializationWorkerPoll       = 500 * time.Millisecond
	materializationMarkerPrefix     = "addp:model-materialization:v1:"
)

type MaterializationService struct {
	systemClient           *commonClient.SystemServiceClient
	executionAuthorization *commonClient.SystemExecutionAuthorizationClient
	repo                   *repository.MaterializationBatchRepository
	logicalTableRepo       *repository.LogicalTableRepository
	logicalTableSvc        *LogicalTableService
	workerID               string
	workerCancel           context.CancelFunc
	workerDone             chan struct{}
	startOnce              sync.Once
}

func NewMaterializationService(
	systemClient *commonClient.SystemServiceClient,
	executionAuthorization *commonClient.SystemExecutionAuthorizationClient,
	repo *repository.MaterializationBatchRepository,
	logicalTableRepo *repository.LogicalTableRepository,
	logicalTableSvc *LogicalTableService,
) *MaterializationService {
	return &MaterializationService{
		systemClient: systemClient, executionAuthorization: executionAuthorization,
		repo: repo, logicalTableRepo: logicalTableRepo, logicalTableSvc: logicalTableSvc,
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
	userAccessToken, triggerType, source string,
	parentExecutionID *string,
) (string, string, error) {
	triggerType, source, err := normalizeMaterializationTrigger(triggerType, source)
	if err != nil {
		return "", "", err
	}
	table, fields, locator, targetName, fingerprint, err := s.loadApprovedDefinition(logicalTableID, tenantID)
	if err != nil {
		return "", "", err
	}
	if _, partitioned := table.Materialization["partition_by"]; partitioned {
		return "", "", apperrors.Validation("materialization_partition_unsupported", modeli18n.MsgMaterializationInvalid)
	}
	batchID := uuid.NewString()
	executionID := uuid.NewString()
	stagingName := materializationTemporaryName(targetName, "staging", batchID)
	now := time.Now().UTC()
	batch := &models.MaterializationBatch{
		ID: batchID, TenantID: tenantID, LogicalTableID: logicalTableID,
		LogicalTableVersion: table.Version, EngineID: int64(locator.EngineID),
		TargetParentLocator: table.Materialization["target_parent_locator"].(string),
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
	if err := s.authorizeExecution(ctx, tenantID, executionID, locator.EngineID, userAccessToken, parentExecutionID); err != nil {
		_ = s.repo.FailPendingExecution(ctx, tenantID, executionID, batchID, commonExecution.TaskTypeMaterializationPrepare, "model.materialization.authorization_issue_failed")
		return "", "", apperrors.Wrap(apperrors.KindUnavailable, "materialization_authorization_failed", modeli18n.MsgMaterializationUnavailable, err)
	}
	_ = fields // validation and fingerprint are intentionally completed before queueing.
	return executionID, batchID, nil
}

func (s *MaterializationService) Publish(
	ctx context.Context,
	logicalTableID, tenantID, userID int64,
	userAccessToken, triggerType, source string,
	parentExecutionID *string,
) (string, error) {
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
	if err := s.authorizeExecution(ctx, tenantID, executionID, uint(batch.EngineID), userAccessToken, parentExecutionID); err != nil {
		_ = s.repo.FailPendingExecution(ctx, tenantID, executionID, batch.ID, commonExecution.TaskTypeMaterializationPublish, "model.materialization.authorization_issue_failed")
		return "", apperrors.Wrap(apperrors.KindUnavailable, "materialization_authorization_failed", modeli18n.MsgMaterializationUnavailable, err)
	}
	return executionID, nil
}

func (s *MaterializationService) GetBatch(ctx context.Context, id string, tenantID int64) (*models.MaterializationBatch, error) {
	batch, err := s.repo.GetByID(ctx, id, tenantID)
	if err != nil {
		return nil, materializationResourceError(err)
	}
	return batch, nil
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
	userAccessToken string,
	parentExecutionID *string,
) error {
	engineIDs := []string{strconv.FormatUint(uint64(engineID), 10)}
	effects := []string{"read", "ddl"}
	var issued *commonClient.IssuedExecutionAuthorization
	var err error
	if parentExecutionID != nil {
		issued, err = s.systemClient.WithTenantID(uint(tenantID)).IssueExecutionAuthorizationFromExecution(ctx, commonClient.IssueExecutionAuthorizationFromExecutionRequest{
			ParentExecutionID: *parentExecutionID, Audience: commonExecution.AudienceModel,
			ExecutionID: executionID, EngineIDs: engineIDs, Effects: effects, ExpiresIn: materializationAuthorizationTTL,
		})
	} else {
		if s.executionAuthorization == nil {
			return errors.New("user execution authorization client is not configured")
		}
		issued, err = s.executionAuthorization.Issue(ctx, userAccessToken, commonClient.IssueExecutionAuthorizationRequest{
			Audience: commonExecution.AudienceModel, ExecutionID: executionID,
			EngineIDs: engineIDs, Effects: effects, ExpiresIn: materializationAuthorizationTTL,
		})
	}
	if err != nil {
		return err
	}
	fields, err := executionAuthorizationFields(issued)
	if err != nil {
		return err
	}
	if err := s.repo.AttachAuthorization(ctx, tenantID, executionID, fields); err != nil {
		return err
	}
	return nil
}

func executionAuthorizationFields(issued *commonClient.IssuedExecutionAuthorization) (map[string]interface{}, error) {
	if issued == nil {
		return nil, errors.New("execution authorization is empty")
	}
	parse := func(value string) (*int64, error) {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("invalid IAM ID %q", value)
		}
		return &parsed, nil
	}
	authorizationID, err := parse(issued.ID)
	if err != nil {
		return nil, err
	}
	actorID, err := parse(issued.ActorPrincipalID)
	if err != nil {
		return nil, err
	}
	membershipID, err := parse(issued.TenantMembershipID)
	if err != nil {
		return nil, err
	}
	version, err := parse(issued.IssuedAuthorizationVersion)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"execution_authorization_id":   authorizationID,
		"actor_principal_id":           actorID,
		"actor_tenant_membership_id":   membershipID,
		"issued_authorization_version": version,
		"authorization_effects":        pq.StringArray(append([]string(nil), issued.Effects...)),
		"authorization_expires_at":     issued.ExpiresAt,
	}, nil
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
			s.processPending(ctx, commonExecution.TaskTypeMaterializationPublish) {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case now := <-recoveryTicker.C:
			for _, taskType := range []string{commonExecution.TaskTypeMaterializationPrepare, commonExecution.TaskTypeMaterializationPublish} {
				if err := s.repo.RecoverExpiredExecutions(ctx, taskType, now.UTC()); err != nil && ctx.Err() == nil {
					log.Printf("model materialization lease recovery failed: %v", err)
				}
			}
		}
	}
}

func (s *MaterializationService) processPending(ctx context.Context, taskType string) bool {
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
	if taskType == commonExecution.TaskTypeMaterializationPrepare {
		metadata, err = s.executePrepare(ctx, execution, batch)
	} else {
		metadata, err = s.executePublish(ctx, execution, batch)
	}
	executionStatus := commonExecution.ExecutionStatusSuccess
	batchStatus := models.MaterializationBatchPrepared
	var errorDetails commonModels.JSONMap
	if taskType == commonExecution.TaskTypeMaterializationPublish {
		batchStatus = models.MaterializationBatchPublished
	}
	if err != nil {
		executionStatus = commonExecution.ExecutionStatusFailed
		batchStatus = models.MaterializationBatchFailed
		errorDetails = commonModels.JSONMap{"code": "model.materialization.execution_failed", "message": "controlled materialization failed"}
		log.Printf("model materialization execution %s failed: %v", execution.ExecutionID, err)
	}
	if completeErr := s.repo.CompleteExecution(ctx, lease, batch.ID, taskType, executionStatus, batchStatus, metadata, errorDetails); completeErr != nil {
		log.Printf("model materialization execution %s completion failed: %v", execution.ExecutionID, completeErr)
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
	table, fields, _, _, fingerprint, err := s.loadApprovedDefinition(batch.LogicalTableID, batch.TenantID)
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
	pool, err := dbbridge.GetOrCreatePool(engine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, err
	}
	err = pool.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		targetComment, targetExists, err := physicalTableComment(tx, schemaName, batch.TargetName)
		if err != nil {
			return err
		}
		if targetExists && materializationMarkerFingerprint(targetComment) != batch.SchemaFingerprint {
			return fmt.Errorf("target table is not managed by the same approved logical schema")
		}
		stagingComment, stagingExists, err := physicalTableComment(tx, schemaName, batch.StagingName)
		if err != nil {
			return err
		}
		expectedMarker := materializationMarker(batch.SchemaFingerprint, batch.ID)
		if stagingExists {
			if stagingComment == expectedMarker {
				return nil
			}
			return fmt.Errorf("staging table name is already occupied")
		}
		preview := *table
		preview.Materialization = models.JSONB{
			"target_parent_locator": batch.TargetParentLocator,
			"target_name":           batch.StagingName,
		}
		ddl := s.logicalTableSvc.generatePostgreSQLDDL(&preview, fields)
		if err := tx.Exec(ddl).Error; err != nil {
			return err
		}
		return tx.Exec("COMMENT ON TABLE " + qualifiedIdentifier(schemaName, batch.StagingName) + " IS " + quoteSQLLiteral(expectedMarker)).Error
	})
	if err != nil {
		return nil, err
	}
	return commonModels.JSONMap{
		"schema_version": "model.materialization/v1",
		"outputs": commonModels.JSONMap{
			"batch_id": batch.ID,
		},
	}, nil
}

func (s *MaterializationService) executePublish(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	batch *models.MaterializationBatch,
) (commonModels.JSONMap, error) {
	engine, err := s.executionEngine(ctx, execution, batch)
	if err != nil {
		return nil, err
	}
	schemaName, err := materializationSchemaName(batch.TargetParentLocator)
	if err != nil {
		return nil, err
	}
	pool, err := dbbridge.GetOrCreatePool(engine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, err
	}
	expectedMarker := materializationMarker(batch.SchemaFingerprint, batch.ID)
	err = pool.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		targetComment, targetExists, err := physicalTableComment(tx, schemaName, batch.TargetName)
		if err != nil {
			return err
		}
		stagingComment, stagingExists, err := physicalTableComment(tx, schemaName, batch.StagingName)
		if err != nil {
			return err
		}
		if !stagingExists {
			if targetExists && targetComment == expectedMarker {
				return nil
			}
			return fmt.Errorf("prepared staging table is missing")
		}
		if stagingComment != expectedMarker {
			return fmt.Errorf("staging table ownership marker does not match batch")
		}
		if targetExists && materializationMarkerFingerprint(targetComment) != batch.SchemaFingerprint {
			return fmt.Errorf("target table schema marker does not match approved logical schema")
		}
		if targetExists {
			backupName := materializationTemporaryName(batch.TargetName, "backup", batch.ID)
			if _, backupExists, err := physicalTableComment(tx, schemaName, backupName); err != nil {
				return err
			} else if backupExists {
				return fmt.Errorf("materialization backup table name is already occupied")
			}
			if err := tx.Exec("ALTER TABLE " + qualifiedIdentifier(schemaName, batch.TargetName) + " RENAME TO " + quoteIdentifier(backupName)).Error; err != nil {
				return err
			}
			if err := tx.Exec("ALTER TABLE " + qualifiedIdentifier(schemaName, batch.StagingName) + " RENAME TO " + quoteIdentifier(batch.TargetName)).Error; err != nil {
				return err
			}
			return tx.Exec("DROP TABLE " + qualifiedIdentifier(schemaName, backupName)).Error
		}
		return tx.Exec("ALTER TABLE " + qualifiedIdentifier(schemaName, batch.StagingName) + " RENAME TO " + quoteIdentifier(batch.TargetName)).Error
	})
	if err != nil {
		return nil, err
	}
	parentLocator, err := resourcetree.ParseURI(batch.TargetParentLocator)
	if err != nil {
		return nil, err
	}
	targetLocator := (&resourcetree.ResourceLocator{
		EngineID: parentLocator.EngineID,
		Path:     append(append([]string(nil), parentLocator.Path...), batch.TargetName),
		Type:     resourcetree.TypeTable,
	}).ToURI()
	return commonModels.JSONMap{
		"schema_version": "model.materialization/v1",
		"outputs": commonModels.JSONMap{
			"batch_id":       batch.ID,
			"target_locator": targetLocator,
		},
	}, nil
}

func materializationSchemaName(locatorText string) (string, error) {
	locator, err := resourcetree.ParseURI(locatorText)
	if err != nil || locator.Type != resourcetree.TypeSchema || len(locator.Path) == 0 {
		return "", fmt.Errorf("materialization target parent locator is invalid")
	}
	return locator.Path[len(locator.Path)-1], nil
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

func materializationMarker(fingerprint, batchID string) string {
	return materializationMarkerPrefix + fingerprint + ":" + batchID
}

func materializationMarkerFingerprint(marker string) string {
	if !strings.HasPrefix(marker, materializationMarkerPrefix) {
		return ""
	}
	remainder := strings.TrimPrefix(marker, materializationMarkerPrefix)
	parts := strings.SplitN(remainder, ":", 2)
	if len(parts) != 2 || len(parts[0]) != 64 {
		return ""
	}
	return parts[0]
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
