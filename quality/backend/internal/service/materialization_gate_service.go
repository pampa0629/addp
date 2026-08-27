package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/google/uuid"
)

type MaterializationGateService struct {
	repo         *repository.MaterializationGateRepository
	modelClient  *commonClient.ModelClient
	checkTimeout time.Duration
}

type MaterializationGateWriteRequest struct {
	Code                   string                            `json:"code"`
	Name                   string                            `json:"name"`
	Description            string                            `json:"description"`
	Version                int64                             `json:"version"`
	MaterializationGroupID int64                             `json:"materialization_group_id"`
	TableBindings          []MaterializationGateTableBinding `json:"table_bindings"`
	Assertions             json.RawMessage                   `json:"assertions"`
}

func NewMaterializationGateService(repo *repository.MaterializationGateRepository, modelClient *commonClient.ModelClient, checkTimeout time.Duration) *MaterializationGateService {
	return &MaterializationGateService{repo: repo, modelClient: modelClient, checkTimeout: checkTimeout}
}

func (s *MaterializationGateService) List(ctx context.Context, tenantID int64, page, pageSize int) ([]models.MaterializationGateTask, int64, error) {
	return s.repo.List(ctx, tenantID, page, pageSize)
}

func (s *MaterializationGateService) Get(ctx context.Context, tenantID, id int64) (*models.MaterializationGateTask, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: materialization gate id is invalid", commonAPI.ErrBadRequest)
	}
	return s.repo.Get(ctx, tenantID, id)
}

func (s *MaterializationGateService) Create(ctx context.Context, tenantID, userID int64, request MaterializationGateWriteRequest) (*models.MaterializationGateTask, error) {
	group, bindingsJSON, assertionsJSON, err := s.validateWrite(ctx, tenantID, request, false)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	task := &models.MaterializationGateTask{
		TenantID: tenantID, Code: strings.TrimSpace(request.Code), Name: strings.TrimSpace(request.Name), Description: strings.TrimSpace(request.Description),
		Version: 1, MaterializationGroupID: group.ID, MaterializationGroupVersion: group.Version,
		TableBindings: bindingsJSON, Assertions: assertionsJSON,
		CreatedBy: userID, UpdatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *MaterializationGateService) Update(ctx context.Context, tenantID, userID, id int64, request MaterializationGateWriteRequest) (*models.MaterializationGateTask, error) {
	if id <= 0 || request.Version <= 0 {
		return nil, fmt.Errorf("%w: materialization gate id or version is invalid", commonAPI.ErrBadRequest)
	}
	current, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.Code) != current.Code {
		return nil, fmt.Errorf("%w: materialization gate code is immutable", commonAPI.ErrBadRequest)
	}
	group, bindingsJSON, assertionsJSON, err := s.validateWrite(ctx, tenantID, request, true)
	if err != nil {
		return nil, err
	}
	task := &models.MaterializationGateTask{
		ID: id, TenantID: tenantID, Code: current.Code, Name: strings.TrimSpace(request.Name), Description: strings.TrimSpace(request.Description),
		MaterializationGroupID: group.ID, MaterializationGroupVersion: group.Version,
		TableBindings: bindingsJSON, Assertions: assertionsJSON, UpdatedBy: userID, UpdatedAt: time.Now().UTC(),
	}
	if err := s.repo.Replace(ctx, task, request.Version); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, tenantID, id)
}

func (s *MaterializationGateService) Delete(ctx context.Context, tenantID, id, version int64) error {
	if id <= 0 || version <= 0 {
		return fmt.Errorf("%w: materialization gate id or version is invalid", commonAPI.ErrBadRequest)
	}
	return s.repo.Delete(ctx, tenantID, id, version)
}

func (s *MaterializationGateService) validateWrite(ctx context.Context, tenantID int64, request MaterializationGateWriteRequest, updating bool) (*commonClient.MaterializationGroup, json.RawMessage, json.RawMessage, error) {
	code, name := strings.TrimSpace(request.Code), strings.TrimSpace(request.Name)
	if tenantID <= 0 || !materializationGateNamePattern.MatchString(code) || len(code) > 100 || name == "" || len(name) > 200 || request.MaterializationGroupID <= 0 || s.modelClient == nil {
		return nil, nil, nil, fmt.Errorf("%w: materialization gate definition is invalid", commonAPI.ErrBadRequest)
	}
	if !updating && request.Version != 0 {
		return nil, nil, nil, fmt.Errorf("%w: version is not accepted when creating a materialization gate", commonAPI.ErrBadRequest)
	}
	document, err := validateMaterializationGateContract(request.TableBindings, request.Assertions)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %v", commonAPI.ErrBadRequest, err)
	}
	group, err := s.modelClient.WithTenantID(uint(tenantID)).GetMaterializationGroup(ctx, request.MaterializationGroupID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read materialization group: %w", err)
	}
	if err := validateGateGroup(group, request.TableBindings, 0); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %v", commonAPI.ErrConflict, err)
	}
	bindingsJSON, err := json.Marshal(request.TableBindings)
	if err != nil {
		return nil, nil, nil, err
	}
	assertionsJSON, err := json.Marshal(document)
	if err != nil {
		return nil, nil, nil, err
	}
	return group, bindingsJSON, assertionsJSON, nil
}

func (s *MaterializationGateService) Execute(ctx context.Context, tenantID, taskID int64, triggerType, source, parentExecutionID string) (string, error) {
	triggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil || (triggerType != commonExecution.TriggerTypeManual && triggerType != commonExecution.TriggerTypeScheduled) {
		return "", fmt.Errorf("%w: materialization gate trigger type is invalid", commonAPI.ErrBadRequest)
	}
	if strings.TrimSpace(source) != commonExecution.ModuleOrchestrator || strings.TrimSpace(parentExecutionID) == "" {
		return "", fmt.Errorf("%w: materialization gate can only be executed by orchestrator", commonAPI.ErrBadRequest)
	}
	if _, err := uuid.Parse(parentExecutionID); err != nil {
		return "", fmt.Errorf("%w: parent execution id is invalid", commonAPI.ErrBadRequest)
	}
	task, err := s.repo.Get(ctx, tenantID, taskID)
	if err != nil {
		return "", err
	}
	bindings, document, err := decodeGateTaskContract(task)
	if err != nil {
		return "", fmt.Errorf("%w: materialization gate snapshot is invalid", commonAPI.ErrConflict)
	}
	group, err := s.modelClient.WithTenantID(uint(tenantID)).GetMaterializationGroup(ctx, task.MaterializationGroupID)
	if err != nil {
		return "", fmt.Errorf("read materialization group: %w", err)
	}
	if err := validateGateGroup(group, bindings, task.MaterializationGroupVersion); err != nil {
		return "", fmt.Errorf("%w: %v", commonAPI.ErrConflict, err)
	}
	executionID := uuid.NewString()
	now := time.Now().UTC()
	execution := &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: int(tenantID), Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeMaterializationGate, Source: commonExecution.ModuleOrchestrator,
		ParentExecutionID: &parentExecutionID, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status: commonExecution.ExecutionStatusPending, TriggerType: triggerType, MaxAttempts: 3,
		CreatedAt: now, UpdatedAt: now,
		ExecutionConfig: commonModels.JSONMap{
			"schema_version": materializationGateExecutionConfigVersion, "task_version": task.Version,
			"materialization_group_id": task.MaterializationGroupID, "materialization_group_version": task.MaterializationGroupVersion,
			"table_bindings": bindings, "assertions": document, "parent_execution_id": parentExecutionID,
			"check_timeout_ms": s.checkTimeout.Milliseconds(),
		},
	}
	if _, err := s.repo.CreateExecution(ctx, taskID, tenantID, execution); err != nil {
		return "", err
	}
	return executionID, nil
}

func decodeGateTaskContract(task *models.MaterializationGateTask) ([]MaterializationGateTableBinding, *MaterializationGateAssertionDocument, error) {
	var bindings []MaterializationGateTableBinding
	if err := decodeStrictJSON(task.TableBindings, &bindings); err != nil {
		return nil, nil, err
	}
	document, err := validateMaterializationGateContract(bindings, task.Assertions)
	return bindings, document, err
}
