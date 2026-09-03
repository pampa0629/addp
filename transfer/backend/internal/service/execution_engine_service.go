package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/instanceprovider"
	engineplugin "github.com/addp/common/engine/plugin"
	supermapworkflow "github.com/addp/common/engine/plugins/supermap_workflow"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
)

// ExecutionEngineService executes Transfer tasks through common engine/format.
type ExecutionEngineService struct {
	taskRepo         *repository.TaskRepository
	syncStateRepo    *repository.SyncStateRepository
	executionService *ExecutionService
	systemClient     *commonClient.SystemClient
	systemRuntime    *commonClient.SystemServiceClient
	metaClient       *commonClient.MetaClient
	replayRuntime    BoundedReplayRuntime
	cfg              *config.Config
	logger           *slog.Logger
	protectionGate   sourceProtectionGate
}

type sourceProtectionGate interface {
	RequireSourceConfig(context.Context, uint, map[string]interface{}) error
	PrepareBoundedTableProtection(context.Context, uint, map[string]interface{}) (executor.TableSourceProtector, error)
	PrepareBoundedEncodedRecordProtection(context.Context, uint, map[string]interface{}, []datatype.FieldInfo) (engineplugin.EncodedRecordTransform, error)
}

func NewExecutionEngineService(
	taskRepo *repository.TaskRepository,
	syncStateRepo *repository.SyncStateRepository,
	executionService *ExecutionService,
	systemClient *commonClient.SystemClient,
	systemRuntime *commonClient.SystemServiceClient,
	metaClient *commonClient.MetaClient,
) *ExecutionEngineService {
	return &ExecutionEngineService{
		taskRepo:         taskRepo,
		syncStateRepo:    syncStateRepo,
		executionService: executionService,
		systemClient:     systemClient,
		systemRuntime:    systemRuntime,
		metaClient:       metaClient,
		logger:           logger.With("component", "execution_engine_service"),
	}
}

func (s *ExecutionEngineService) SetConfig(cfg *config.Config) {
	s.cfg = cfg
}

func (s *ExecutionEngineService) SetReplayRuntime(runtime BoundedReplayRuntime) {
	s.replayRuntime = runtime
}

func (s *ExecutionEngineService) SetProtectionGate(gate sourceProtectionGate) {
	s.protectionGate = gate
}

// ExecuteTask 执行任务（由 Worker 调用）
func (s *ExecutionEngineService) ExecuteTask(ctx context.Context, taskID, executionID uint) error {
	s.logger.Info("executing task", "task_id", taskID, "execution_id", executionID)

	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}
	execution, err := s.executionService.taskExecutionRepo.GetByID(ctx, int64(executionID), int(task.TenantID))
	if err != nil {
		return fmt.Errorf("failed to get execution: %w", err)
	}
	executionTaskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
	if err != nil || execution.Module != commonExecution.ModuleTransfer || execution.TaskType != commonExecution.TaskTypeSync || executionTaskID != task.ID {
		return fmt.Errorf("execution does not belong to transfer task %d", task.ID)
	}
	if isReplayExecutionConfig(execution.ExecutionConfig) {
		if s.protectionGate == nil {
			return fmt.Errorf("transfer source protection gate is not configured")
		}
		if err := s.protectionGate.RequireSourceConfig(ctx, task.TenantID, task.Config); err != nil {
			return err
		}
		return s.executeBoundedReplay(ctx, task, executionID, execution.ExecutionConfig)
	}
	runtimeTask := *task
	runtimeTask.Config = execution.ExecutionConfig
	return s.executeCommonTransferTask(ctx, &runtimeTask, execution, executionID)
}

func (s *ExecutionEngineService) executeCommonTransferTask(ctx context.Context, task *models.TransferTask, execution *commonExecution.TaskExecution, executionID uint) error {
	if s.systemClient == nil {
		err := fmt.Errorf("system client is required for common engine/format transfer task")
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}

	if err := s.executionService.UpdateStatus(ctx, executionID, models.ExecutionStatusRunning); err != nil {
		return fmt.Errorf("assert bounded execution ownership: %w", err)
	}

	rawSpec, rawErr := planner.ParseRawCopyTaskSpec(task.Config)
	if rawErr == nil {
		if s.protectionGate == nil {
			return fmt.Errorf("transfer source protection gate is not configured")
		}
		if err := s.protectionGate.RequireSourceConfig(ctx, task.TenantID, task.Config); err != nil {
			return err
		}
		if err := s.attachRawCopySourceMetaAttributes(task, &rawSpec); err != nil {
			wrapped := fmt.Errorf("load source meta item attributes: %w", err)
			s.updateExecutionError(ctx, task, executionID, wrapped)
			return wrapped
		}
		resolver := planner.NewHybridEngineResolver(planner.BindEngineResolver(planner.NewSystemEngineResolver(s.systemClient), task.TenantID), s.infraEngineResolver())
		return s.executeCommonRawCopyTask(ctx, task, executionID, rawSpec, resolver)
	}

	recordSpec, recordErr := planner.ParseEncodedRecordExportTaskSpec(task.Config, task.BatchSize)
	if recordErr == nil {
		if err := s.attachEncodedRecordSourceMetaAttributes(task, &recordSpec); err != nil {
			wrapped := fmt.Errorf("load encoded record source meta item attributes: %w", err)
			s.updateExecutionError(ctx, task, executionID, wrapped)
			return wrapped
		}
		resolver := planner.NewHybridEngineResolver(planner.BindEngineResolver(planner.NewSystemEngineResolver(s.systemClient), task.TenantID), s.infraEngineResolver())
		return s.executeCommonEncodedRecordExportTask(ctx, task, executionID, recordSpec, resolver)
	}

	spec, err := planner.ParseTableExportTaskSpec(task.Config, task.BatchSize)
	if err != nil {
		wrapped := fmt.Errorf("parse common transfer task config: table=%v; encoded_record_export=%v; raw_copy=%v", err, recordErr, rawErr)
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	if err := s.attachSourceMetaAttributes(task, &spec); err != nil {
		wrapped := fmt.Errorf("load source meta item attributes: %w", err)
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	if planner.IsRuntimeExistingTargetTaskConfig(task.Config) {
		return s.executeRuntimeTargetTableTransferTask(ctx, task, execution, executionID, spec)
	}

	resolver := planner.NewHybridEngineResolver(planner.BindEngineResolver(planner.NewSystemEngineResolver(s.systemClient), task.TenantID), s.infraEngineResolver())
	if planner.IsWatermarkIncrementalSpec(spec) {
		if s.protectionGate == nil {
			return fmt.Errorf("transfer source protection gate is not configured")
		}
		if err := s.protectionGate.RequireSourceConfig(ctx, task.TenantID, task.Config); err != nil {
			return err
		}
		return s.executeWatermarkIncrementalTask(ctx, task, executionID, spec, resolver)
	}
	return s.executeCommonTableTransferTask(ctx, task, executionID, spec, resolver)
}

func (s *ExecutionEngineService) executeWatermarkIncrementalTask(ctx context.Context, task *models.TransferTask, executionID uint, spec planner.TableExportTaskSpec, resolver planner.EngineResolver) error {
	if s.syncStateRepo == nil {
		err := fmt.Errorf("sync state repository is required for watermark incremental task")
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}
	execution, err := s.executionService.taskExecutionRepo.GetByID(ctx, int64(executionID), 0)
	if err != nil {
		return err
	}
	stateID := uint(metadataUint64(execution.Metadata, "sync_state_id"))
	fencingToken := metadataUint64(execution.Metadata, "fencing_token")
	if stateID == 0 || fencingToken == 0 {
		err := fmt.Errorf("watermark execution is missing sync state fencing metadata")
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}
	state, err := s.syncStateRepo.GetByID(ctx, stateID)
	if err != nil {
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}
	start, err := watermarkCursorFromPosition(state.Position)
	if err != nil {
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}
	build, err := planner.BuildWatermarkIncrementalPlan(spec, resolver)
	if err != nil {
		wrapped := fmt.Errorf("build watermark incremental plan: %w", err)
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	build.Plan.Start = start
	expectedVersion := state.StateVersion
	build.Plan.BeforeApply = func(ctx context.Context) error {
		return s.syncStateRepo.AssertFence(ctx, state.ID, fencingToken)
	}
	build.Plan.AfterApply = func(ctx context.Context, cursor *engineplugin.WatermarkCursor, recordsRead, recordsWritten int64) error {
		position := models.JSONMap{
			"type": "watermark", "version": "v1",
			"cursor": map[string]interface{}{"values": append([]string(nil), cursor.Values...)},
		}
		if err := s.syncStateRepo.CommitPosition(ctx, state.ID, expectedVersion, fencingToken, position, execution.ExecutionID); err != nil {
			return err
		}
		expectedVersion++
		if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{"records_read": recordsRead, "records_written": recordsWritten}); err != nil {
			return err
		}
		return s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{
			"checkpoint_offset": recordsRead,
			"checkpoint_state": map[string]interface{}{
				"version": "watermark/v1", "committed_position": position,
				"state_version": expectedVersion, "fencing_token": fencingToken, "target_committed": true,
			},
		})
	}
	incrementalExecutor, err := executor.NewWatermarkIncrementalExecutor(build.SourceEngineType, build.TargetEngineType)
	if err != nil {
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}
	metrics, err := incrementalExecutor.Execute(ctx, build.Plan)
	if err != nil {
		wrapped := fmt.Errorf("execute watermark incremental plan: %w", err)
		if metrics != nil {
			s.updateTableTransferMetrics(executionID, metrics.RecordsRead, metrics.RecordsWritten)
		}
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	s.updateTableTransferMetrics(executionID, metrics.RecordsRead, metrics.RecordsWritten)
	if err := s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{"metadata": map[string]interface{}{"execution_upper_bound": metrics.UpperBound}}); err != nil {
		s.logger.Warn("failed to persist watermark upper bound", "error", err, "execution_id", executionID)
	}
	if task.AutoScanMetadata && metrics.RecordsWritten > 0 {
		s.triggerMetadataScan(task, executionID, spec, build.Plan.Target, nil)
	}
	if err := s.writeTransferLineageFacts(ctx, task, executionID, spec.Source.Locator, spec.Target.Locator, spec.Target.ParentLocator, spec.Target.Name, spec.Target.Policy); err != nil {
		s.logger.Warn("failed to persist transfer lineage facts", "error", err, "execution_id", executionID)
	}
	if err := s.writeBoundedExecutionOutputs(ctx, executionID, spec.Target.Locator, spec.Target.ParentLocator, spec.Target.Name, metrics.RecordsWritten); err != nil {
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}
	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusSuccess, ""); err != nil {
		return err
	}
	return nil
}

func metadataUint64(metadata map[string]interface{}, key string) uint64 {
	switch value := metadata[key].(type) {
	case uint64:
		return value
	case uint:
		return uint64(value)
	case int64:
		return uint64(value)
	case float64:
		return uint64(value)
	default:
		return 0
	}
}

func watermarkCursorFromPosition(position models.JSONMap) (*engineplugin.WatermarkCursor, error) {
	if len(position) == 0 {
		return nil, nil
	}
	if position["type"] != "watermark" || position["version"] != "v1" {
		return nil, fmt.Errorf("unsupported sync position type/version")
	}
	cursor, ok := position["cursor"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("watermark sync position cursor is invalid")
	}
	rawValues, ok := cursor["values"].([]interface{})
	if !ok {
		if values, ok := cursor["values"].([]string); ok {
			return &engineplugin.WatermarkCursor{Values: values}, nil
		}
		return nil, fmt.Errorf("watermark sync position values are invalid")
	}
	values := make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		values = append(values, fmt.Sprint(value))
	}
	return &engineplugin.WatermarkCursor{Values: values}, nil
}

func (s *ExecutionEngineService) infraEngineResolver() *planner.InfraEngineResolver {
	if s == nil || s.cfg == nil {
		return nil
	}
	return planner.NewInfraEngineResolver(planner.InfraEngineConfig{
		MinioEndpoint:  s.cfg.BuiltinMinioEndpoint,
		MinioAccessKey: s.cfg.BuiltinMinioAccessKey,
		MinioSecretKey: s.cfg.BuiltinMinioSecretKey,
		MinioUseSSL:    s.cfg.BuiltinMinioUseSSL,
	})
}

func (s *ExecutionEngineService) attachRawCopySourceMetaAttributes(task *models.TransferTask, spec *planner.RawCopyTaskSpec) error {
	if spec == nil {
		return nil
	}
	return s.attachEndpointSourceMetaAttributes(task, &spec.Source)
}

func (s *ExecutionEngineService) attachEncodedRecordSourceMetaAttributes(task *models.TransferTask, spec *planner.EncodedRecordExportTaskSpec) error {
	if spec == nil {
		return nil
	}
	return s.attachEndpointSourceMetaAttributes(task, &spec.Source)
}

func (s *ExecutionEngineService) attachEndpointSourceMetaAttributes(task *models.TransferTask, endpoint *planner.EndpointSpec) error {
	if task == nil || endpoint == nil || endpoint.LocatorItemID() == 0 {
		return nil
	}
	if s.metaClient == nil {
		return fmt.Errorf("meta client is required when source locator item_id is set")
	}
	item, err := s.metaClient.WithTenantID(task.TenantID).GetItemByID(endpoint.LocatorItemID())
	if err != nil {
		return err
	}
	if item.EngineID != endpoint.LocatorEngineID() {
		return fmt.Errorf("source meta item engine_id %d does not match source locator engine id %d", item.EngineID, endpoint.LocatorEngineID())
	}
	endpoint.Attributes = item.Attributes
	return nil
}

func (s *ExecutionEngineService) attachSourceMetaAttributes(task *models.TransferTask, spec *planner.TableExportTaskSpec) error {
	if task == nil || spec == nil || spec.Source.LocatorItemID() == 0 {
		return s.attachSourceInspectAttributes(task, spec)
	}
	if s.metaClient == nil {
		return fmt.Errorf("meta client is required when source locator item_id is set")
	}
	item, err := s.metaClient.WithTenantID(task.TenantID).GetItemByID(spec.Source.LocatorItemID())
	if err != nil {
		return err
	}
	if item.EngineID != spec.Source.LocatorEngineID() {
		return fmt.Errorf("source meta item engine_id %d does not match source locator engine id %d", item.EngineID, spec.Source.LocatorEngineID())
	}
	spec.Source.Attributes = item.Attributes
	return nil
}

func (s *ExecutionEngineService) attachSourceInspectAttributes(task *models.TransferTask, spec *planner.TableExportTaskSpec) error {
	if task == nil || spec == nil || !planner.IsInfraLocatorURI(spec.Source.Locator) {
		return nil
	}
	if s.metaClient == nil {
		return fmt.Errorf("meta client is required when source locator is infra")
	}
	result, err := s.metaClient.WithTenantID(task.TenantID).InspectAttributes(commonClient.MetaInspectRequest{
		Locator:   spec.Source.Locator,
		ScanDepth: "deep",
	})
	if err != nil {
		return err
	}
	if result == nil || len(result.Attributes) == 0 {
		return fmt.Errorf("meta inspect returned empty attributes")
	}
	spec.Source.Attributes = result.Attributes
	return nil
}

func (s *ExecutionEngineService) executeCommonTableTransferTask(ctx context.Context, task *models.TransferTask, executionID uint, spec planner.TableExportTaskSpec, resolver planner.EngineResolver) error {
	buildResult, metrics, err := s.runCommonTableTransferData(ctx, task, executionID, spec, resolver)
	if err != nil {
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}

	if task.AutoScanMetadata {
		s.triggerMetadataScan(task, executionID, spec, buildResult.Plan.Target, metrics.TargetRefs)
	}
	if err := s.writeTransferLineageFacts(ctx, task, executionID, spec.Source.Locator, spec.Target.Locator, spec.Target.ParentLocator, spec.Target.Name, spec.Target.Policy); err != nil {
		s.logger.Warn("failed to persist transfer lineage facts", "error", err, "execution_id", executionID)
	}
	if err := s.writeBoundedExecutionOutputs(ctx, executionID, spec.Target.Locator, spec.Target.ParentLocator, spec.Target.Name, metrics.RecordsWritten); err != nil {
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}
	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusSuccess, ""); err != nil {
		return err
	}
	return nil
}

func (s *ExecutionEngineService) runCommonTableTransferData(
	ctx context.Context,
	task *models.TransferTask,
	executionID uint,
	spec planner.TableExportTaskSpec,
	resolver planner.EngineResolver,
) (*planner.TableTransferBuildResult, *executor.TablePipelineMetrics, error) {
	buildResult, err := planner.BuildTableTransferPlan(spec, resolver)
	if err != nil {
		return nil, nil, fmt.Errorf("build common table transfer plan: %w", err)
	}
	buildResult.Plan.ProgressCallback = s.tableProgressCallback(task, executionID)

	tableExecutor, err := executor.NewTableTransferExecutor(
		buildResult.SourceEngineType,
		buildResult.TargetEngineType,
		buildResult.Plan.Source.Format,
		buildResult.Plan.Target.Format,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create common table transfer executor: %w", err)
	}
	if err := s.configureInstanceTableProviders(ctx, task.TenantID, spec, buildResult, tableExecutor); err != nil {
		return nil, nil, fmt.Errorf("configure instance table providers: %w", err)
	}
	tableExecutor.SourceProtector, err = prepareBoundedTableSourceProtection(
		ctx, s.protectionGate, task.TenantID, task.Config, buildResult.SourceEngineType, buildResult.Plan.Source.Kind,
	)
	if err != nil {
		return nil, nil, err
	}
	if hasSpatialReprojectTransform(buildResult.Plan.Transforms) {
		workflowEngine, workflowOperator, err := s.selectDirectWorkflowRuntime(ctx, task.TenantID, "vector_reproject")
		if err != nil {
			return nil, nil, fmt.Errorf("resolve vector_reproject workflow runtime: %w", err)
		}
		tableExecutor.GeometryBatchReprojecter = newWorkflowGeometryBatchReprojectProvider(workflowEngine, workflowOperator.Name)
	}
	if tableExecutor.TargetCRSRequirements != nil {
		tableExecutor.CRSDefinitionConverter = newWorkflowCRSDefinitionConverter(func(resolveCtx context.Context) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
			return s.selectDirectWorkflowRuntime(resolveCtx, task.TenantID, "crs_to_projjson")
		})
	}

	metrics, err := tableExecutor.Execute(ctx, buildResult.Plan)
	if err != nil {
		wrapped := fmt.Errorf("execute common table transfer plan: %w", err)
		if metrics != nil {
			s.updateTableTransferMetrics(executionID, metrics.RecordsRead, metrics.RecordsWritten)
		}
		return buildResult, metrics, wrapped
	}
	s.updateTableTransferMetrics(executionID, metrics.RecordsRead, metrics.RecordsWritten)
	s.updateTableTargetRefs(executionID, metrics.TargetRefs)

	return buildResult, metrics, nil
}

func prepareBoundedTableSourceProtection(
	ctx context.Context,
	gate sourceProtectionGate,
	tenantID uint,
	config map[string]interface{},
	sourceEngineType string,
	sourceKind executor.TableEndpointKind,
) (executor.TableSourceProtector, error) {
	if gate == nil {
		return nil, fmt.Errorf("transfer source protection gate is not configured")
	}
	switch sourceKind {
	case executor.TableEndpointEncoded:
		if err := gate.RequireSourceConfig(ctx, tenantID, config); err != nil {
			return nil, err
		}
		return nil, nil
	case executor.TableEndpointNative:
		protector, err := gate.PrepareBoundedTableProtection(ctx, tenantID, config)
		if err != nil {
			return nil, fmt.Errorf("prepare bounded table source protection: %w", err)
		}
		return protector, nil
	case executor.TableEndpointQuery:
		if sourceEngineType != "postgresql" {
			if err := gate.RequireSourceConfig(ctx, tenantID, config); err != nil {
				return nil, err
			}
			return nil, nil
		}
		protector, err := gate.PrepareBoundedTableProtection(ctx, tenantID, config)
		if err != nil {
			return nil, fmt.Errorf("prepare bounded table source protection: %w", err)
		}
		return protector, nil
	default:
		return nil, fmt.Errorf("unsupported protected table source kind %q", sourceKind)
	}
}

func (s *ExecutionEngineService) executeRuntimeTargetTableTransferTask(
	ctx context.Context,
	task *models.TransferTask,
	execution *commonExecution.TaskExecution,
	executionID uint,
	spec planner.TableExportTaskSpec,
) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || execution == nil || execution.ParentExecutionID == nil || strings.TrimSpace(*execution.ParentExecutionID) == "" {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, fmt.Errorf("runtime-target transfer requires a current bounded lease and orchestration parent"))
	}
	if s.systemRuntime == nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, fmt.Errorf("System runtime client is required for runtime-target transfer"))
	}
	targetLocator, err := runtimeTargetLocator(execution.Metadata)
	if err != nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
	}
	resolvedSpec, err := planner.ResolveRuntimeExistingTarget(spec, targetLocator)
	if err != nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
	}
	sourceRef, err := resolvedSpec.Source.EngineRef()
	if err != nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
	}
	targetRef, err := resolvedSpec.Target.EngineRef()
	if err != nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
	}
	effectsByEngine := map[uint][]string{sourceRef.ID: []string{"read"}}
	if sourceRef.ID == targetRef.ID {
		effectsByEngine[sourceRef.ID] = []string{"read", "write"}
	} else {
		effectsByEngine[targetRef.ID] = []string{"write"}
	}
	orderedEngineIDs := make([]uint, 0, len(effectsByEngine))
	for engineID := range effectsByEngine {
		orderedEngineIDs = append(orderedEngineIDs, engineID)
	}
	sort.Slice(orderedEngineIDs, func(i, j int) bool { return orderedEngineIDs[i] < orderedEngineIDs[j] })
	accesses := make([]commonClient.ExecutionEngineAccessScope, 0, len(orderedEngineIDs))
	for _, engineID := range orderedEngineIDs {
		accesses = append(accesses, commonClient.ExecutionEngineAccessScope{
			EngineID: strconv.FormatUint(uint64(engineID), 10), Effects: append([]string(nil), effectsByEngine[engineID]...),
		})
	}
	issued, err := s.systemRuntime.WithTenantID(task.TenantID).IssueExecutionAuthorizationFromExecution(ctx, commonClient.IssueExecutionAuthorizationFromExecutionRequest{
		ParentExecutionID: *execution.ParentExecutionID,
		Audience:          commonExecution.AudienceTransfer,
		ExecutionID:       execution.ExecutionID,
		Attempt:           lease.Attempt,
		LeaseToken:        lease.Token,
		Accesses:          accesses,
		ExpiresIn:         int64(time.Hour / time.Second),
	})
	if err != nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
	}
	authorizationFields, err := commonClient.TaskExecutionAuthorizationFields(issued)
	if err != nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
	}
	if err := s.taskRepo.AttachBoundedExecutionAuthorization(ctx, lease, authorizationFields); err != nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
	}

	resolver := planner.StaticEngineResolver{}
	for _, engineID := range orderedEngineIDs {
		requiredEffects := effectsByEngine[engineID]
		access, err := s.systemRuntime.WithTenantID(task.TenantID).GetExecutionEngineAccess(ctx, issued.ID, commonClient.ExecutionEngineAccessRequest{
			ExecutionID: execution.ExecutionID, EngineID: strconv.FormatUint(uint64(engineID), 10),
			RequiredEffects: requiredEffects,
		})
		if err != nil {
			return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
		}
		binding, err := planner.EngineBindingFromEngine(access.Engine)
		if err != nil {
			return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
		}
		resolver[engineID] = binding
	}
	_, metrics, err := s.runCommonTableTransferData(ctx, task, executionID, resolvedSpec, resolver)
	if err != nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
	}
	if err := s.writeTransferLineageFacts(ctx, task, executionID, resolvedSpec.Source.Locator, resolvedSpec.Target.Locator, resolvedSpec.Target.ParentLocator, resolvedSpec.Target.Name, resolvedSpec.Target.Policy); err != nil {
		s.logger.Warn("failed to persist runtime-target transfer lineage facts", "error", err, "execution_id", executionID)
	}
	if err := s.writeBoundedExecutionOutputs(ctx, executionID, targetLocator, "", "", metrics.RecordsWritten); err != nil {
		return s.failRuntimeTargetTransfer(ctx, task, executionID, err)
	}
	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusSuccess, ""); err != nil {
		return err
	}
	return nil
}

func runtimeTargetLocator(metadata commonModels.JSONMap) (string, error) {
	inputs, ok := metadata["runtime_inputs"].(map[string]interface{})
	if !ok {
		if typed, typedOK := metadata["runtime_inputs"].(commonModels.JSONMap); typedOK {
			inputs = map[string]interface{}(typed)
			ok = true
		}
	}
	value, valueOK := inputs["target_locator"].(string)
	locator, err := resourcetree.ParseURI(strings.TrimSpace(value))
	if !ok || !valueOK || err != nil || locator.Type != resourcetree.TypeTable {
		return "", fmt.Errorf("runtime target_locator must identify a table")
	}
	return locator.ToURI(), nil
}

func (s *ExecutionEngineService) failRuntimeTargetTransfer(
	ctx context.Context,
	task *models.TransferTask,
	executionID uint,
	err error,
) error {
	wrapped := fmt.Errorf("execute runtime existing-table transfer: %w", err)
	s.updateExecutionError(ctx, task, executionID, wrapped)
	return wrapped
}

func (s *ExecutionEngineService) configureInstanceTableProviders(
	ctx context.Context,
	tenantID uint,
	spec planner.TableExportTaskSpec,
	build *planner.TableTransferBuildResult,
	tableExecutor *executor.TableTransferExecutor,
) error {
	if build == nil || tableExecutor == nil {
		return fmt.Errorf("table transfer build result and executor are required")
	}
	sourceProvider, err := s.superMapSDXPostgreSQLTableProvider(ctx, tenantID, build.SourceEngine)
	if err != nil {
		return fmt.Errorf("resolve source SuperMap table provider: %w", err)
	}
	if sourceProvider != nil {
		tableExecutor.SourceNativeReader = nil
		tableExecutor.SourceTableSessionProvider = sourceProvider
	}
	targetProvider, err := s.superMapSDXPostgreSQLTableProvider(ctx, tenantID, build.TargetEngine)
	if err != nil {
		return fmt.Errorf("resolve target SuperMap table provider: %w", err)
	}
	if targetProvider != nil {
		tableExecutor.TargetDeleteProvider = targetProvider
		tableExecutor.TargetNativePreparer = targetProvider
		tableExecutor.TargetNativeWriter = nil
		tableExecutor.TargetTableSessionProvider = targetProvider
	}
	if err := s.configureRuntimeFormatProviders(ctx, tenantID, spec, build, tableExecutor); err != nil {
		return err
	}
	return nil
}

func (s *ExecutionEngineService) configureRuntimeFormatProviders(
	ctx context.Context,
	tenantID uint,
	spec planner.TableExportTaskSpec,
	build *planner.TableTransferBuildResult,
	tableExecutor *executor.TableTransferExecutor,
) error {
	if factory, err := format.GetRuntimeScopeTableReaderFactory(build.Plan.Source.Format); err == nil {
		runtimeEngine, runtimeProvider, runtimeConn, err := s.resolveDirectWorkflowRuntime(ctx, tenantID, factory.RequiredScopeTableReadOperators())
		if err != nil {
			return fmt.Errorf("resolve %s reader runtime: %w", build.Plan.Source.Format, err)
		}
		locator, err := spec.Source.ResourceLocator()
		if err != nil {
			return fmt.Errorf("parse runtime format source locator: %w", err)
		}
		source, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
			Engine: engineFromBinding(build.SourceEngine), Locator: locator,
			Kind: runtimeFormatResourceKind(build.Plan.Source.Format), Format: string(build.Plan.Source.Format),
		})
		if err != nil {
			return fmt.Errorf("resolve %s source access: %w", build.Plan.Source.Format, err)
		}
		plan, err := workflowaccess.NewSourcePlan(source)
		if err != nil {
			return err
		}
		provider, err := factory.BindScopeTableReader(runtimeProvider, runtimeConn, plan)
		if err != nil {
			return fmt.Errorf("bind %s reader to runtime %d: %w", build.Plan.Source.Format, runtimeEngine.ID, err)
		}
		tableExecutor.SourceScopeReadProvider = provider
	}

	if factory, err := format.GetRuntimeScopeTableWriterFactory(build.Plan.Target.Format); err == nil {
		if !build.Plan.Target.DeleteBeforeWrite {
			return fmt.Errorf("runtime whole-scope target format %s requires replace apply mode", build.Plan.Target.Format)
		}
		runtimeEngine, runtimeProvider, runtimeConn, err := s.resolveDirectWorkflowRuntime(ctx, tenantID, factory.RequiredScopeTableWriteOperators())
		if err != nil {
			return fmt.Errorf("resolve %s writer runtime: %w", build.Plan.Target.Format, err)
		}
		parent, err := spec.Target.ParentResourceLocator()
		if err != nil {
			return fmt.Errorf("parse runtime format target parent locator: %w", err)
		}
		targetName := filepath.Base(build.Plan.Target.Path.StringPath())
		target, _, err := workflowaccess.ResolveTarget(workflowaccess.ResourceSpec{
			Engine: engineFromBinding(build.TargetEngine), Locator: parent,
			Kind: runtimeFormatResourceKind(build.Plan.Target.Format), Format: string(build.Plan.Target.Format),
			Name: targetName, WriteMode: workflowaccess.WriteModeReplace,
		})
		if err != nil {
			return fmt.Errorf("resolve %s target access: %w", build.Plan.Target.Format, err)
		}
		plan, err := workflowaccess.NewTargetPlan(target)
		if err != nil {
			return err
		}
		provider, err := factory.BindScopeTableWriter(runtimeProvider, runtimeConn, plan)
		if err != nil {
			return fmt.Errorf("bind %s writer to runtime %d: %w", build.Plan.Target.Format, runtimeEngine.ID, err)
		}
		tableExecutor.TargetScopeWriterProvider = provider
	}
	return nil
}

func engineFromBinding(binding planner.EngineBinding) *commonModels.Engine {
	return &commonModels.Engine{
		ID: binding.EngineID, EngineType: binding.Type,
		ConnectionInfo: commonModels.ConnectionInfo(binding.ConnInfo),
	}
}

func runtimeFormatResourceKind(formatType format.FormatType) string {
	descriptor, ok := format.GetFormatDescriptor(formatType)
	if ok {
		for _, layout := range descriptor.Layouts {
			if layout == format.LayoutWhole {
				return workflowaccess.KindDirectory
			}
		}
	}
	return workflowaccess.KindFile
}

func (s *ExecutionEngineService) resolveDirectWorkflowRuntime(
	ctx context.Context,
	tenantID uint,
	operators []string,
) (commonModels.Engine, engineplugin.WorkflowRuntimeProvider, engineplugin.ConnectionInfo, error) {
	if s.systemRuntime == nil {
		return commonModels.Engine{}, nil, nil, fmt.Errorf("system client is required to resolve workflow runtime")
	}
	descriptors, err := s.systemRuntime.WithTenantID(tenantID).ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return commonModels.Engine{}, nil, nil, fmt.Errorf("list workflow runtime descriptors: %w", err)
	}
	failures := make([]string, 0)
	for index := range descriptors {
		engine := descriptors[index].AsEngine()
		if !engineselection.IsAvailableForComputeEntrypoint(engine, "workflow") {
			continue
		}
		if err := dbbridge.RequireDirectWorkflowOperators(ctx, engine, operators...); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", engine.Name, err))
			continue
		}
		provider, err := dbbridge.WorkflowRuntimeProviderForEngine(engine)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", engine.Name, err))
			continue
		}
		return *engine, provider, engineplugin.ConnectionInfo(engine.ConnectionInfo), nil
	}
	return commonModels.Engine{}, nil, nil, fmt.Errorf("no active workflow runtime provides all required direct operators %s; %s", strings.Join(operators, ", "), strings.Join(failures, "; "))
}

func (s *ExecutionEngineService) superMapSDXPostgreSQLTableProvider(
	ctx context.Context,
	tenantID uint,
	binding planner.EngineBinding,
) (*supermapworkflow.SDXPostgreSQLTableProvider, error) {
	_, ok := planner.SuperMapSDXPostgreSQLWorkspace(binding)
	if !ok {
		return nil, nil
	}
	if s.systemRuntime == nil {
		return nil, fmt.Errorf("system client is required to resolve the bound SuperMap workflow runtime")
	}
	engine := &commonModels.Engine{
		ID:             binding.EngineID,
		EngineType:     binding.Type,
		Capabilities:   nil,
		ConnectionInfo: commonModels.ConnectionInfo(binding.ConnInfo),
	}
	if binding.Capabilities != nil {
		payload, err := engineplugin.MarshalEngineCapabilities(*binding.Capabilities)
		if err != nil {
			return nil, fmt.Errorf("serialize engine capabilities: %w", err)
		}
		jsonPayload := commonModels.JSONString(payload)
		engine.Capabilities = &jsonPayload
	}
	resolved, err := instanceprovider.Resolve(ctx, s.systemRuntime.WithTenantID(tenantID), engine, supermapworkflow.RequiredTableOperators()...)
	if err != nil {
		return nil, err
	}
	provider, ok := resolved.(*supermapworkflow.SDXPostgreSQLTableProvider)
	if !ok {
		return nil, fmt.Errorf("engine %d SuperMap workspace resolved to %T", binding.EngineID, resolved)
	}
	return provider, nil
}

func (s *ExecutionEngineService) selectDirectWorkflowRuntime(ctx context.Context, tenantID uint, operatorName string) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
	if s.systemRuntime == nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("system client is required to resolve workflow runtime")
	}
	descriptors, err := s.systemRuntime.WithTenantID(tenantID).ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("list workflow runtime descriptors: %w", err)
	}
	engines := make([]commonModels.Engine, 0, len(descriptors))
	for index := range descriptors {
		if engine := descriptors[index].AsEngine(); engine != nil {
			engines = append(engines, *engine)
		}
	}
	return dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{
		OperatorName: operatorName,
	})
}

func hasSpatialReprojectTransform(transforms []executor.TableTransformPlan) bool {
	for _, transform := range transforms {
		if strings.EqualFold(strings.TrimSpace(transform.Type), "spatial_reproject") {
			return true
		}
	}
	return false
}

func (s *ExecutionEngineService) executeCommonRawCopyTask(ctx context.Context, task *models.TransferTask, executionID uint, spec planner.RawCopyTaskSpec, resolver planner.EngineResolver) error {
	buildResult, err := planner.BuildRawCopyPlan(spec, resolver)
	if err != nil {
		wrapped := fmt.Errorf("build common raw copy plan: %w", err)
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	buildResult.Plan.ProgressCallback = s.rawCopyProgressCallback(task, executionID)

	rawCopyExecutor, err := executor.NewRawCopyExecutor(buildResult.SourceEngineType, buildResult.TargetEngineType)
	if err != nil {
		wrapped := fmt.Errorf("create common raw copy executor: %w", err)
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}

	metrics, err := rawCopyExecutor.Execute(ctx, buildResult.Plan)
	if err != nil {
		wrapped := fmt.Errorf("execute common raw copy plan: %w", err)
		if metrics != nil {
			s.updateRawCopyMetrics(executionID, metrics)
		}
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	s.updateRawCopyMetrics(executionID, metrics)

	if task.AutoScanMetadata {
		s.triggerRawCopyMetadataScan(task, executionID, spec, buildResult.Plan.Target)
	}
	if err := s.writeTransferLineageFacts(ctx, task, executionID, spec.Source.Locator, spec.Target.Locator, spec.Target.ParentLocator, spec.Target.Name, spec.Target.Policy); err != nil {
		s.logger.Warn("failed to persist raw copy lineage facts", "error", err, "execution_id", executionID)
	}
	if err := s.writeBoundedExecutionOutputs(ctx, executionID, spec.Target.Locator, spec.Target.ParentLocator, spec.Target.Name, metrics.RecordsWritten); err != nil {
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}
	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusSuccess, ""); err != nil {
		return err
	}
	return nil
}

func (s *ExecutionEngineService) executeCommonEncodedRecordExportTask(ctx context.Context, task *models.TransferTask, executionID uint, spec planner.EncodedRecordExportTaskSpec, resolver planner.EngineResolver) error {
	buildResult, err := planner.BuildEncodedRecordExportPlan(spec, resolver)
	if err != nil {
		wrapped := fmt.Errorf("build encoded record export plan: %w", err)
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	buildResult.Plan.ProgressCallback = s.encodedRecordExportProgressCallback(task, executionID)
	if s.protectionGate == nil {
		wrapped := fmt.Errorf("transfer source protection gate is not configured")
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	buildResult.Plan.BeforeEncode, err = s.protectionGate.PrepareBoundedEncodedRecordProtection(
		ctx, task.TenantID, task.Config, planner.EncodedRecordSourceFields(spec),
	)
	if err != nil {
		wrapped := fmt.Errorf("prepare encoded record source protection: %w", err)
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}

	recordExecutor, err := executor.NewEncodedRecordExportExecutor(buildResult.SourceEngineType, buildResult.TargetEngineType)
	if err != nil {
		wrapped := fmt.Errorf("create encoded record export executor: %w", err)
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	metrics, err := recordExecutor.Execute(ctx, buildResult.Plan)
	if err != nil {
		wrapped := fmt.Errorf("execute encoded record export plan: %w", err)
		if metrics != nil {
			s.updateEncodedRecordExportMetrics(executionID, metrics)
		}
		s.updateExecutionError(ctx, task, executionID, wrapped)
		return wrapped
	}
	s.updateEncodedRecordExportMetrics(executionID, metrics)
	if err := s.writeTransferLineageFacts(ctx, task, executionID, spec.Source.Locator, spec.Target.Locator, spec.Target.ParentLocator, spec.Target.Name, spec.Target.Policy); err != nil {
		s.logger.Warn("failed to persist encoded record export lineage facts", "error", err, "execution_id", executionID)
	}
	if err := s.writeBoundedExecutionOutputs(ctx, executionID, spec.Target.Locator, spec.Target.ParentLocator, spec.Target.Name, metrics.RecordsWritten); err != nil {
		s.updateExecutionError(ctx, task, executionID, err)
		return err
	}
	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusSuccess, ""); err != nil {
		return err
	}
	return nil
}

func (s *ExecutionEngineService) writeTransferLineageFacts(ctx context.Context, task *models.TransferTask, executionID uint, sourceLocator, targetLocator, targetParentLocator, targetName string, targetPolicy map[string]interface{}) error {
	if s == nil || s.executionService == nil || task == nil {
		return nil
	}
	input := commonExecution.LineageResourceRef{Port: "source", Locator: strings.TrimSpace(sourceLocator)}
	if locator, err := resourcetree.ParseURI(input.Locator); err == nil && locator.ItemID != nil {
		input.ItemID = locator.ItemID
	}
	outputLocator := strings.TrimSpace(targetLocator)
	if outputLocator == "" {
		outputLocator = targetLineageLocator(targetParentLocator, targetName)
	}
	output := commonExecution.LineageResourceRef{Port: "target", Locator: outputLocator}
	if locator, err := resourcetree.ParseURI(output.Locator); err == nil && locator.ItemID != nil {
		output.ItemID = locator.ItemID
	}
	writeMode := ""
	if targetPolicy != nil {
		if value, ok := targetPolicy["apply_mode"].(string); ok {
			writeMode = strings.ToLower(strings.TrimSpace(value))
		}
	}
	output.WriteMode = writeMode
	facts := commonExecution.LineageFacts{
		SchemaVersion:      commonExecution.LineageFactsSchemaVersion,
		Inputs:             []commonExecution.LineageResourceRef{input},
		Outputs:            []commonExecution.LineageResourceRef{output},
		Operations:         []commonExecution.LineageOperation{{Kind: "derive", Operator: "transfer", InputPorts: []string{"source"}, OutputPorts: []string{"target"}}},
		RuntimeExecutionID: executionIDString(s, ctx, executionID),
	}
	return s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{"metadata": map[string]interface{}{"lineage_facts": facts}})
}

func targetLineageLocator(parentURI, name string) string {
	if planner.IsInfraLocatorURI(parentURI) {
		parent, err := planner.ParseInfraLocatorURI(parentURI)
		if err != nil || parent == nil {
			return ""
		}
		switch parent.Type {
		case resourcetree.TypePrefix, resourcetree.TypeDirectory:
		default:
			return ""
		}
		child, err := parent.Child(name, resourcetree.TypeObject)
		if err != nil {
			return ""
		}
		return child.ToURI()
	}
	parent, err := resourcetree.ParseURI(strings.TrimSpace(parentURI))
	if err != nil || parent == nil || parent.EngineID == 0 || strings.TrimSpace(name) == "" {
		return ""
	}
	path := append(append([]string(nil), parent.Path...), strings.TrimSpace(name))
	resourceType := resourcetree.TypeObject
	switch parent.Type {
	case resourcetree.TypeSchema, resourcetree.TypeDatabase:
		resourceType = resourcetree.TypeTable
	case resourcetree.TypeDirectory, resourcetree.TypeDir, resourcetree.TypeRoot:
		resourceType = resourcetree.TypeFile
	}
	return (&resourcetree.ResourceLocator{EngineID: parent.EngineID, Path: path, Type: resourceType}).ToURI()
}

func (s *ExecutionEngineService) writeBoundedExecutionOutputs(ctx context.Context, executionID uint, targetLocator, targetParentLocator, targetName string, rowCount int64) error {
	locator := strings.TrimSpace(targetLocator)
	if locator == "" {
		locator = targetLineageLocator(targetParentLocator, targetName)
	}
	if locator == "" {
		return fmt.Errorf("bounded Transfer execution target locator is empty")
	}
	runtimeExecutionID := executionIDString(s, ctx, executionID)
	if runtimeExecutionID == "" {
		return fmt.Errorf("bounded Transfer execution identity is empty")
	}
	return s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{
		"metadata": commonModels.JSONMap{"outputs": commonModels.JSONMap{
			"execution_id":   runtimeExecutionID,
			"target_locator": locator,
			"row_count":      rowCount,
		}},
	})
}

func executionIDString(s *ExecutionEngineService, ctx context.Context, id uint) string {
	if s == nil || s.executionService == nil {
		return ""
	}
	execution, err := s.executionService.taskExecutionRepo.GetByID(ctx, int64(id), 0)
	if err != nil || execution == nil {
		return ""
	}
	return execution.ExecutionID
}

func (s *ExecutionEngineService) updateExecutionError(ctx context.Context, task *models.TransferTask, executionID uint, execErr error) {
	if execErr == nil {
		return
	}

	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusFailed, execErr.Error()); err != nil {
		s.logger.Error("CRITICAL: failed to mark execution as failed - status inconsistency may occur",
			"error", err,
			"execution_id", executionID,
			"task_id", task.ID)
	} else {
		s.logger.Info("execution marked as failed", "execution_id", executionID)
	}

	// FinishExecution advances the execution and owner-task summary atomically
	// when the call carries a bounded execution lease.
}

func (s *ExecutionEngineService) updateTableTransferMetrics(executionID uint, recordsRead, recordsWritten int64) {
	ctx := context.Background()
	if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
		"records_read":    recordsRead,
		"records_written": recordsWritten,
	}); err != nil {
		s.logger.Error("failed to update common transfer execution metrics", "error", err, "execution_id", executionID)
	}
}

func (s *ExecutionEngineService) updateTableTargetRefs(executionID uint, refs []format.RelatedRef) {
	if len(refs) == 0 {
		return
	}
	payload := make([]map[string]interface{}, 0, len(refs))
	for _, ref := range refs {
		refPath := strings.TrimSpace(ref.Ref.Path)
		if refPath == "" {
			continue
		}
		payload = append(payload, map[string]interface{}{
			"path":      refPath,
			"role":      strings.TrimSpace(ref.Ref.Role),
			"required":  ref.Required,
			"primary":   ref.Primary,
			"extension": format.NormalizeExtension(filepath.Ext(refPath)),
		})
	}
	if len(payload) == 0 {
		return
	}
	ctx := context.Background()
	if err := s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{
		"metadata": map[string]interface{}{
			"target_refs": payload,
		},
	}); err != nil {
		s.logger.Error("failed to update table target refs", "error", err, "execution_id", executionID)
	}
}

func (s *ExecutionEngineService) updateRawCopyMetrics(executionID uint, metrics *executor.RawCopyMetrics) {
	if metrics == nil {
		return
	}
	ctx := context.Background()
	if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
		"records_read":    metrics.RecordsRead,
		"records_written": metrics.RecordsWritten,
		"bytes_read":      metrics.BytesRead,
		"bytes_written":   metrics.BytesWritten,
	}); err != nil {
		s.logger.Error("failed to update raw copy execution metrics", "error", err, "execution_id", executionID)
	}
}

func (s *ExecutionEngineService) updateEncodedRecordExportMetrics(executionID uint, metrics *executor.EncodedRecordExportMetrics) {
	if metrics == nil {
		return
	}
	ctx := context.Background()
	if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
		"records_read": metrics.RecordsRead, "records_written": metrics.RecordsWritten,
		"bytes_read": metrics.BytesRead, "bytes_written": metrics.BytesWritten,
	}); err != nil {
		s.logger.Error("failed to update encoded record export metrics", "error", err, "execution_id", executionID)
	}
}

func (s *ExecutionEngineService) tableProgressCallback(task *models.TransferTask, executionID uint) executor.TableProgressCallback {
	return func(ctx context.Context, event executor.TableProgressEvent) error {
		if s.executionService == nil {
			return nil
		}
		if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
			"records_read":    event.RecordsRead,
			"records_written": event.RecordsWritten,
		}); err != nil {
			return fmt.Errorf("update metrics: %w", err)
		}
		checkpointState := map[string]interface{}{
			"version":          "v1",
			"batch_index":      event.BatchIndex,
			"source_offset":    event.SourceOffset,
			"records_read":     event.RecordsRead,
			"records_written":  event.RecordsWritten,
			"target_committed": true,
			"updated_at":       time.Now().Format(time.RFC3339),
		}
		if event.ResumeMarker != nil {
			checkpointState["resume_marker"] = event.ResumeMarker.Clone()
		}
		if event.CommitMarker != nil {
			checkpointState["commit_marker"] = event.CommitMarker.Clone()
		}
		if event.Final {
			checkpointState["final"] = true
		}
		if err := s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{
			"checkpoint_offset": event.RecordsRead,
			"checkpoint_state":  checkpointState,
		}); err != nil {
			return fmt.Errorf("update checkpoint: %w", err)
		}
		if task != nil {
			progress := runningProgress(event.BatchIndex)
			if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{"progress": progress}); err != nil {
				s.logger.Warn("failed to update task progress", "error", err, "task_id", task.ID, "progress", progress)
			}
		}
		logLine := fmt.Sprintf(
			"%s batch=%d source_offset=%d batch_rows=%d records_read=%d records_written=%d target_committed=true final=%t resume_marker=%t commit_marker=%t",
			time.Now().Format(time.RFC3339),
			event.BatchIndex,
			event.SourceOffset,
			event.BatchRows,
			event.RecordsRead,
			event.RecordsWritten,
			event.Final,
			event.ResumeMarker != nil,
			event.CommitMarker != nil,
		)
		if err := s.executionService.AppendLog(ctx, executionID, logLine); err != nil {
			return fmt.Errorf("append progress log: %w", err)
		}
		return nil
	}
}

func (s *ExecutionEngineService) rawCopyProgressCallback(task *models.TransferTask, executionID uint) executor.RawCopyProgressCallback {
	return func(ctx context.Context, event executor.RawCopyProgressEvent) error {
		if s.executionService == nil {
			return nil
		}
		if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
			"records_read":    event.RecordsRead,
			"records_written": event.RecordsWritten,
			"bytes_read":      event.BytesRead,
			"bytes_written":   event.BytesWritten,
		}); err != nil {
			return fmt.Errorf("update raw copy metrics: %w", err)
		}
		checkpointState := map[string]interface{}{
			"version":          "v1",
			"records_read":     event.RecordsRead,
			"records_written":  event.RecordsWritten,
			"bytes_read":       event.BytesRead,
			"bytes_written":    event.BytesWritten,
			"target_committed": true,
			"updated_at":       time.Now().Format(time.RFC3339),
		}
		if event.Final {
			checkpointState["final"] = true
		}
		if err := s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{
			"checkpoint_offset": event.RecordsRead,
			"checkpoint_state":  checkpointState,
		}); err != nil {
			return fmt.Errorf("update raw copy checkpoint: %w", err)
		}
		if task != nil {
			progress := 99.0
			if event.Final {
				progress = 100.0
			}
			if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{"progress": progress}); err != nil {
				s.logger.Warn("failed to update raw copy task progress", "error", err, "task_id", task.ID, "progress", progress)
			}
		}
		logLine := fmt.Sprintf(
			"%s raw_copy bytes_read=%d bytes_written=%d records_read=%d records_written=%d target_committed=true final=%t",
			time.Now().Format(time.RFC3339),
			event.BytesRead,
			event.BytesWritten,
			event.RecordsRead,
			event.RecordsWritten,
			event.Final,
		)
		if err := s.executionService.AppendLog(ctx, executionID, logLine); err != nil {
			return fmt.Errorf("append raw copy progress log: %w", err)
		}
		return nil
	}
}

func (s *ExecutionEngineService) encodedRecordExportProgressCallback(task *models.TransferTask, executionID uint) executor.EncodedRecordExportProgressCallback {
	return func(ctx context.Context, event executor.EncodedRecordExportProgressEvent) error {
		if s.executionService == nil {
			return nil
		}
		if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
			"records_read": event.RecordsRead, "records_written": event.RecordsWritten,
			"bytes_read": event.BytesRead, "bytes_written": event.BytesWritten,
		}); err != nil {
			return fmt.Errorf("update encoded record export metrics: %w", err)
		}
		checkpointState := map[string]interface{}{
			"version": "v1", "batch_index": event.BatchIndex, "source_offset": event.SourceOffset,
			"records_read": event.RecordsRead, "records_written": event.RecordsWritten,
			"bytes_read": event.BytesRead, "bytes_written": event.BytesWritten,
			"target_committed": event.Final, "updated_at": time.Now().Format(time.RFC3339),
		}
		if event.Final {
			checkpointState["final"] = true
		}
		if err := s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{
			"checkpoint_offset": event.RecordsRead,
			"checkpoint_state":  checkpointState,
		}); err != nil {
			return fmt.Errorf("update encoded record export checkpoint: %w", err)
		}
		if task != nil {
			progress := runningProgress(event.BatchIndex)
			if event.Final {
				progress = 100
			}
			if err := s.taskRepo.UpdateFields(task.ID, map[string]interface{}{"progress": progress}); err != nil {
				s.logger.Warn("failed to update encoded record export task progress", "error", err, "task_id", task.ID)
			}
		}
		return s.executionService.AppendLog(ctx, executionID, fmt.Sprintf(
			"%s batch=%d source_offset=%d batch_records=%d records_read=%d records_written=%d bytes_written=%d target_committed=%t final=%t",
			time.Now().Format(time.RFC3339), event.BatchIndex, event.SourceOffset, event.BatchRecords,
			event.RecordsRead, event.RecordsWritten, event.BytesWritten, event.Final, event.Final,
		))
	}
}

func runningProgress(batchIndex int64) float64 {
	if batchIndex <= 0 {
		return 0
	}
	progress := float64(batchIndex)
	if progress > 99 {
		return 99
	}
	return progress
}

func (s *ExecutionEngineService) triggerMetadataScan(task *models.TransferTask, executionID uint, spec planner.TableExportTaskSpec, targetPlan executor.TableTargetPlan, targetRefs []format.RelatedRef) {
	if s.metaClient == nil {
		s.logger.Warn("meta client not available, skipping metadata scan", "task_id", task.ID)
		return
	}

	targetEngineID := spec.Target.LocatorEngineID()
	if targetEngineID == 0 {
		s.logger.Warn("no target engine id found, skipping metadata scan", "task_id", task.ID)
		return
	}

	metaClient := s.metaClient.WithTenantID(task.TenantID)
	refGroups := tableTargetRefGroups(targetPlan, targetRefs)
	catalogPaths := []string(nil)
	if len(refGroups) == 0 {
		catalogPaths = nativeTargetCatalogPaths(spec.Target)
	}
	if len(refGroups) == 0 && len(catalogPaths) == 0 {
		s.logger.Warn("metadata scan scope is empty, skipping metadata scan",
			"task_id", task.ID,
			"engine_id", targetEngineID,
			"target_format", targetPlan.Format)
		return
	}

	s.logger.Info("triggering metadata scan",
		"task_id", task.ID,
		"engine_id", targetEngineID,
		"catalog_paths", catalogPaths,
		"ref_groups", len(refGroups))

	run, err := metaClient.CreateManualScanRun(commonClient.MetaScanOptions{
		EngineID:     targetEngineID,
		CatalogPaths: catalogPaths,
		RefGroups:    refGroups,
		ScanDepth:    "deep",
		Force:        true,
		TriggerType:  "manual",
		Source:       commonExecution.ModuleTransfer,
	})
	if err != nil {
		s.logger.Error("failed to trigger metadata scan",
			"error", err,
			"task_id", task.ID,
			"engine_id", targetEngineID)
		return
	}
	s.logger.Info("metadata scan triggered successfully",
		"task_id", task.ID,
		"engine_id", targetEngineID,
		"execution_id", run.ExecutionID)
	s.updateMetadataScanExecution(executionID, run.ExecutionID, targetEngineID, catalogPaths, len(refGroups))
}

func (s *ExecutionEngineService) triggerRawCopyMetadataScan(task *models.TransferTask, executionID uint, spec planner.RawCopyTaskSpec, targetPlan executor.RawCopyEndpointPlan) {
	if s.metaClient == nil {
		s.logger.Warn("meta client not available, skipping metadata scan", "task_id", task.ID)
		return
	}
	targetEngineID := spec.Target.LocatorEngineID()
	if targetEngineID == 0 {
		s.logger.Warn("no target engine id found, skipping metadata scan", "task_id", task.ID)
		return
	}
	refGroups := singlePathRefGroups(targetPlan.Path.StringPath())
	run, err := s.metaClient.WithTenantID(task.TenantID).CreateManualScanRun(commonClient.MetaScanOptions{
		EngineID:    targetEngineID,
		RefGroups:   refGroups,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: "manual",
		Source:      commonExecution.ModuleTransfer,
	})
	if err != nil {
		s.logger.Error("failed to trigger metadata scan",
			"error", err,
			"task_id", task.ID,
			"engine_id", targetEngineID)
		return
	}
	s.logger.Info("metadata scan triggered successfully",
		"task_id", task.ID,
		"engine_id", targetEngineID,
		"execution_id", run.ExecutionID)
	s.updateMetadataScanExecution(executionID, run.ExecutionID, targetEngineID, nil, len(refGroups))
}

func (s *ExecutionEngineService) updateMetadataScanExecution(executionID uint, scanExecutionID string, engineID uint, catalogPaths []string, refGroupCount int) {
	if executionID == 0 || s.executionService == nil || strings.TrimSpace(scanExecutionID) == "" {
		return
	}
	payload := map[string]interface{}{
		"execution_id": scanExecutionID,
		"engine_id":    engineID,
	}
	if len(catalogPaths) > 0 {
		payload["catalog_paths"] = catalogPaths
	}
	if refGroupCount > 0 {
		payload["ref_group_count"] = refGroupCount
	}
	ctx := context.Background()
	if err := s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{
		"metadata": map[string]interface{}{
			"metadata_scan": payload,
		},
	}); err != nil {
		s.logger.Error("failed to update metadata scan execution", "error", err, "execution_id", executionID, "scan_execution_id", scanExecutionID)
	}
}

func tableTargetRefGroups(target executor.TableTargetPlan, targetRefs []format.RelatedRef) []commonClient.MetaScanRefGroup {
	if target.Kind != executor.TableEndpointEncoded {
		return nil
	}
	if len(targetRefs) > 0 {
		return relatedRefsToMetaScanRefGroups(targetRefs)
	}
	if target.Format == "" {
		return singlePathRefGroups(target.Path.StringPath())
	}
	if _, err := format.GetTableWriterProvider(target.Format); err == nil {
		return singlePathRefGroups(target.Path.StringPath())
	}
	if _, err := format.GetMultiTableWriterProvider(target.Format); err != nil {
		return singlePathRefGroups(target.Path.StringPath())
	}
	return nil
}

func singlePathRefGroups(path string) []commonClient.MetaScanRefGroup {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return []commonClient.MetaScanRefGroup{
		{
			Primary: path,
			Refs: []commonClient.MetaScanRef{
				{Path: path, Role: contentio.RoleMain, Required: true},
			},
		},
	}
}

func relatedRefsToMetaScanRefGroups(refs []format.RelatedRef) []commonClient.MetaScanRefGroup {
	if len(refs) == 0 {
		return nil
	}
	group := commonClient.MetaScanRefGroup{
		Refs: make([]commonClient.MetaScanRef, 0, len(refs)),
	}
	for _, ref := range refs {
		path := strings.TrimSpace(ref.Ref.Path)
		if path == "" {
			continue
		}
		if ref.Primary {
			group.Primary = path
		}
		group.Refs = append(group.Refs, commonClient.MetaScanRef{
			Path:     path,
			Role:     strings.TrimSpace(ref.Ref.Role),
			Required: ref.Required,
		})
	}
	if group.Primary == "" && len(group.Refs) > 0 {
		group.Primary = group.Refs[0].Path
	}
	if len(group.Refs) == 0 {
		return nil
	}
	return []commonClient.MetaScanRefGroup{group}
}

func nativeTargetCatalogPaths(endpoint planner.EndpointSpec) []string {
	loc, err := endpoint.ParentResourceLocator()
	if err != nil {
		return nil
	}

	switch loc.Type {
	case resourcetree.TypeSchema, resourcetree.TypeDatabase:
		return []string{strings.TrimSpace(loc.PathString())}
	}
	return nil
}

func cleanPathValue(value interface{}) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "<nil>" {
		return ""
	}
	return text
}
