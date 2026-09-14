package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/google/uuid"
)

type DataValidationService struct {
	repo *repository.DataValidationRepository

	checkTimeout time.Duration
}

type DataValidationWriteRequest struct {
	Code          string                       `json:"code"`
	Name          string                       `json:"name"`
	Description   string                       `json:"description"`
	Version       int64                        `json:"version"`
	TableBindings []DataValidationTableBinding `json:"table_bindings"`
	Assertions    json.RawMessage              `json:"assertions"`
}

func NewDataValidationService(repo *repository.DataValidationRepository, checkTimeout time.Duration) *DataValidationService {
	return &DataValidationService{repo: repo, checkTimeout: checkTimeout}
}

func (s *DataValidationService) List(ctx context.Context, tenantID int64, page, pageSize int) ([]models.DataValidationTask, int64, error) {
	return s.repo.List(ctx, tenantID, page, pageSize)
}

func (s *DataValidationService) Get(ctx context.Context, tenantID, id int64) (*models.DataValidationTask, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: data validation id is invalid", commonAPI.ErrBadRequest)
	}
	return s.repo.Get(ctx, tenantID, id)
}

func (s *DataValidationService) Create(ctx context.Context, tenantID, userID int64, request DataValidationWriteRequest) (*models.DataValidationTask, error) {
	bindingsJSON, assertionsJSON, err := s.validateWrite(ctx, tenantID, request, false)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	task := &models.DataValidationTask{
		TenantID: tenantID, Code: strings.TrimSpace(request.Code), Name: strings.TrimSpace(request.Name), Description: strings.TrimSpace(request.Description),
		Version:       1,
		TableBindings: bindingsJSON, Assertions: assertionsJSON,
		CreatedBy: userID, UpdatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *DataValidationService) Update(ctx context.Context, tenantID, userID, id int64, request DataValidationWriteRequest) (*models.DataValidationTask, error) {
	if id <= 0 || request.Version <= 0 {
		return nil, fmt.Errorf("%w: data validation id or version is invalid", commonAPI.ErrBadRequest)
	}
	current, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.Code) != current.Code {
		return nil, fmt.Errorf("%w: data validation code is immutable", commonAPI.ErrBadRequest)
	}
	bindingsJSON, assertionsJSON, err := s.validateWrite(ctx, tenantID, request, true)
	if err != nil {
		return nil, err
	}
	task := &models.DataValidationTask{
		ID: id, TenantID: tenantID, Code: current.Code, Name: strings.TrimSpace(request.Name), Description: strings.TrimSpace(request.Description),
		TableBindings: bindingsJSON, Assertions: assertionsJSON, UpdatedBy: userID, UpdatedAt: time.Now().UTC(),
	}
	if err := s.repo.Replace(ctx, task, request.Version); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, tenantID, id)
}

func (s *DataValidationService) Delete(ctx context.Context, tenantID, id, version int64) error {
	if id <= 0 || version <= 0 {
		return fmt.Errorf("%w: data validation id or version is invalid", commonAPI.ErrBadRequest)
	}
	return s.repo.Delete(ctx, tenantID, id, version)
}

func (s *DataValidationService) validateWrite(ctx context.Context, tenantID int64, request DataValidationWriteRequest, updating bool) (json.RawMessage, json.RawMessage, error) {
	code, name := strings.TrimSpace(request.Code), strings.TrimSpace(request.Name)
	if tenantID <= 0 || !dataValidationNamePattern.MatchString(code) || len(code) > 100 || name == "" || len(name) > 200 {
		return nil, nil, fmt.Errorf("%w: data validation definition is invalid", commonAPI.ErrBadRequest)
	}
	if !updating && request.Version != 0 {
		return nil, nil, fmt.Errorf("%w: version is not accepted when creating a data validation", commonAPI.ErrBadRequest)
	}
	document, err := validateDataValidationContract(request.TableBindings, request.Assertions)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", commonAPI.ErrBadRequest, err)
	}
	bindingsJSON, err := json.Marshal(request.TableBindings)
	if err != nil {
		return nil, nil, err
	}
	assertionsJSON, err := json.Marshal(document)
	if err != nil {
		return nil, nil, err
	}
	return bindingsJSON, assertionsJSON, nil
}

func (s *DataValidationService) Execute(ctx context.Context, tenantID, taskID int64, triggerType, source, parentExecutionID string) (string, error) {
	triggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil || (triggerType != commonExecution.TriggerTypeManual && triggerType != commonExecution.TriggerTypeScheduled) {
		return "", fmt.Errorf("%w: data validation trigger type is invalid", commonAPI.ErrBadRequest)
	}
	if strings.TrimSpace(source) != commonExecution.ModuleOrchestrator || strings.TrimSpace(parentExecutionID) == "" {
		return "", fmt.Errorf("%w: data validation can only be executed by orchestrator", commonAPI.ErrBadRequest)
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
		return "", fmt.Errorf("%w: data validation snapshot is invalid", commonAPI.ErrConflict)
	}
	executionID := uuid.NewString()
	now := time.Now().UTC()
	execution := &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: int(tenantID), Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeDataValidation, Source: commonExecution.ModuleOrchestrator,
		ParentExecutionID: &parentExecutionID, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status: commonExecution.ExecutionStatusPending, TriggerType: triggerType, MaxAttempts: 3,
		CreatedAt: now, UpdatedAt: now,
		ExecutionConfig: commonModels.JSONMap{
			"schema_version": dataValidationExecutionConfigVersion, "task_version": task.Version,
			"table_bindings": bindings, "assertions": document, "parent_execution_id": parentExecutionID,
			"check_timeout_ms": s.checkTimeout.Milliseconds(),
		},
	}
	if _, err := s.repo.CreateExecution(ctx, taskID, tenantID, execution); err != nil {
		return "", err
	}
	return executionID, nil
}

func decodeGateTaskContract(task *models.DataValidationTask) ([]DataValidationTableBinding, *DataValidationAssertionDocument, error) {
	var bindings []DataValidationTableBinding
	if err := decodeStrictJSON(task.TableBindings, &bindings); err != nil {
		return nil, nil, err
	}
	document, err := validateDataValidationContract(bindings, task.Assertions)
	return bindings, document, err
}
