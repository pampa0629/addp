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
	"github.com/addp/common/resourcetree"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/google/uuid"
	"strconv"
)

type PlanService struct {
	repo                   *repository.PlanRepository
	systemClient           *commonClient.SystemServiceClient
	ruleRepo               *repository.RuleRepository
	executionAuthorization *commonClient.SystemExecutionAuthorizationClient
	standardClient         *commonClient.StandardClient

	checkTimeout time.Duration
}

type PlanWriteRequest struct {
	Code          string             `json:"code"`
	OwnerDomainID *int64             `json:"owner_domain_id,omitempty"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	Version       int64              `json:"version"`
	TableBindings []PlanTableBinding `json:"table_bindings"`
	CheckItems    []PlanCheckRequest `json:"check_items"`
}

type PlanCheckRequest struct {
	RuleKey    string               `json:"rule_key"`
	RuleID     int64                `json:"rule_id"`
	RevisionNo int64                `json:"revision_no"`
	Bindings   models.CheckBindings `json:"bindings"`
	Severity   string               `json:"severity"`
	Disabled   bool                 `json:"disabled"`
}

func NewPlanService(repo *repository.PlanRepository, checkTimeout time.Duration) *PlanService {
	s := &PlanService{repo: repo, checkTimeout: checkTimeout}
	if repo != nil {
		s.ruleRepo = repo.RuleRepository()
	}
	return s
}

func (s *PlanService) WithClients(system *commonClient.SystemServiceClient, authorization *commonClient.SystemExecutionAuthorizationClient) *PlanService {
	s.systemClient, s.executionAuthorization = system, authorization
	return s
}

func (s *PlanService) WithStandardClient(standard *commonClient.StandardClient) *PlanService {
	s.standardClient = standard
	return s
}

func (s *PlanService) List(ctx context.Context, tenantID int64, ownerDomainID *int64, page, pageSize int) ([]models.QualityPlan, int64, error) {
	return s.repo.List(ctx, tenantID, ownerDomainID, page, pageSize)
}

func (s *PlanService) Get(ctx context.Context, tenantID, id int64) (*models.QualityPlan, error) {
	if id <= 0 {
		return nil, fmt.Errorf("%w: quality plan id is invalid", commonAPI.ErrBadRequest)
	}
	return s.repo.Get(ctx, tenantID, id)
}

func (s *PlanService) Create(ctx context.Context, tenantID, userID int64, request PlanWriteRequest) (*models.QualityPlan, error) {
	bindingsJSON, items, err := s.validateWrite(ctx, tenantID, request, nil)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	task := &models.QualityPlan{
		TenantID: tenantID, Code: strings.TrimSpace(request.Code), Name: strings.TrimSpace(request.Name), Description: strings.TrimSpace(request.Description), OwnerDomainID: request.OwnerDomainID,
		Version:       1,
		TableBindings: bindingsJSON, CheckItems: items,
		CreatedBy: userID, UpdatedBy: userID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, tenantID, task.ID)
}

func (s *PlanService) Update(ctx context.Context, tenantID, userID, id int64, request PlanWriteRequest) (*models.QualityPlan, error) {
	if id <= 0 || request.Version <= 0 {
		return nil, fmt.Errorf("%w: quality plan id or version is invalid", commonAPI.ErrBadRequest)
	}
	current, err := s.repo.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.Code) != current.Code {
		return nil, fmt.Errorf("%w: quality plan code is immutable", commonAPI.ErrBadRequest)
	}
	if current.Version != request.Version {
		return nil, repository.ErrVersionConflict
	}
	bindingsJSON, items, err := s.validateWrite(ctx, tenantID, request, current)
	if err != nil {
		return nil, err
	}
	task := &models.QualityPlan{
		ID: id, TenantID: tenantID, Code: current.Code, Name: strings.TrimSpace(request.Name), Description: strings.TrimSpace(request.Description), OwnerDomainID: request.OwnerDomainID,
		TableBindings: bindingsJSON, CheckItems: items, UpdatedBy: userID, UpdatedAt: time.Now().UTC(),
	}
	if err := s.repo.Replace(ctx, task, request.Version); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, tenantID, id)
}

func (s *PlanService) Delete(ctx context.Context, tenantID, id, version int64) error {
	if id <= 0 || version <= 0 {
		return fmt.Errorf("%w: quality plan id or version is invalid", commonAPI.ErrBadRequest)
	}
	return s.repo.Delete(ctx, tenantID, id, version)
}

func (s *PlanService) validateWrite(ctx context.Context, tenantID int64, request PlanWriteRequest, current *models.QualityPlan) (json.RawMessage, []models.PlanCheckItem, error) {
	code, name := strings.TrimSpace(request.Code), strings.TrimSpace(request.Name)
	if tenantID <= 0 || !planNamePattern.MatchString(code) || len(code) > 100 || name == "" || len(name) > 200 {
		return nil, nil, fmt.Errorf("%w: quality plan definition is invalid", commonAPI.ErrBadRequest)
	}
	if err := validateOwnedDomain(ctx, s.standardClient, tenantID, request.OwnerDomainID); err != nil {
		return nil, nil, err
	}
	if current == nil && request.Version != 0 {
		return nil, nil, fmt.Errorf("%w: version is not accepted when creating a quality plan", commonAPI.ErrBadRequest)
	}
	if len(request.CheckItems) == 0 || len(request.CheckItems) > 500 {
		return nil, nil, commonAPI.ErrBadRequest
	}
	items := make([]models.PlanCheckItem, len(request.CheckItems))
	for i, item := range request.CheckItems {
		if item.RuleID <= 0 || item.RevisionNo <= 0 {
			return nil, nil, commonAPI.ErrBadRequest
		}
		items[i] = models.PlanCheckItem{RuleKey: item.RuleKey, RuleID: item.RuleID, RevisionNo: item.RevisionNo, Bindings: item.Bindings, Severity: item.Severity, Disabled: item.Disabled}
	}
	if err := s.ruleRepo.Resolve(ctx, tenantID, items); err != nil {
		return nil, nil, err
	}
	resolved := models.QualityPlan{CheckItems: items}
	if err := resolved.ResolveRules(); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", commonAPI.ErrBadRequest, err)
	}
	document, err := validatePlanContract(request.TableBindings, resolved.Rules)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", commonAPI.ErrBadRequest, err)
	}
	complete := true
	for _, binding := range request.TableBindings {
		if binding.Locator == "" {
			complete = false
		}
	}
	if complete {
		if err := s.validatePostgreSQLTargets(ctx, tenantID, request.TableBindings, document); err != nil {
			return nil, nil, err
		}
	}
	bindingsJSON, err := json.Marshal(request.TableBindings)
	if err != nil {
		return nil, nil, err
	}
	return bindingsJSON, items, nil
}

func (s *PlanService) Execute(ctx context.Context, tenantID, taskID int64, triggerType, source, parentExecutionID string, request models.PlanRunRequest) (string, error) {
	return s.execute(ctx, tenantID, taskID, triggerType, source, parentExecutionID, "", 0, request)
}

func (s *PlanService) Run(ctx context.Context, tenantID, taskID, userID int64, token string, request models.PlanRunRequest) (string, error) {
	return s.execute(ctx, tenantID, taskID, commonExecution.TriggerTypeManual, commonExecution.ModuleQuality, "", token, userID, request)
}

func (s *PlanService) execute(ctx context.Context, tenantID, taskID int64, triggerType, source, parentID, token string, userID int64, request models.PlanRunRequest) (string, error) {
	triggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil || (triggerType != commonExecution.TriggerTypeManual && triggerType != commonExecution.TriggerTypeScheduled) {
		return "", fmt.Errorf("%w: invalid trigger type", commonAPI.ErrBadRequest)
	}
	var parent *string
	if source == commonExecution.ModuleOrchestrator {
		if _, err := uuid.Parse(parentID); err != nil {
			return "", fmt.Errorf("%w: invalid parent execution", commonAPI.ErrBadRequest)
		}
		parent = &parentID
	} else if source != commonExecution.ModuleQuality || parentID != "" || userID <= 0 || token == "" || triggerType != commonExecution.TriggerTypeManual || s.executionAuthorization == nil {
		return "", fmt.Errorf("%w: invalid execution origin", commonAPI.ErrBadRequest)
	}
	now := time.Now().UTC()
	execution := &commonExecution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenantID), Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan, Source: source, ParentExecutionID: parent,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, Status: commonExecution.ExecutionStatusPending, TriggerType: triggerType, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		ExecutionConfig: commonModels.JSONMap{"schema_version": planExecutionConfigVersion, "parent_execution_id": parentID, "check_timeout_ms": s.checkTimeout.Milliseconds()},
	}
	if userID > 0 {
		id := int(userID)
		execution.TriggeredBy = &id
	}
	_, err = s.repo.CreateExecution(ctx, taskID, tenantID, execution, request)
	if err != nil {
		return "", err
	}
	if parent == nil {
		var bindings []PlanTableBinding
		if err = json.Unmarshal(execution.ExecutionConfig["table_bindings"].(json.RawMessage), &bindings); err == nil && len(bindings) > 0 {
			locator, parseErr := resourcetree.ParseURI(bindings[0].Locator)
			err = parseErr
			if err == nil {
				var issued *commonClient.IssuedExecutionAuthorization
				issued, err = s.executionAuthorization.Issue(ctx, token, commonClient.IssueExecutionAuthorizationRequest{Audience: commonExecution.AudienceQuality, ExecutionID: execution.ExecutionID, Accesses: []commonClient.ExecutionEngineAccessScope{{EngineID: strconv.FormatUint(uint64(locator.EngineID), 10), Effects: []string{"read"}}}, ExpiresIn: 3600})
				if err == nil {
					var fields map[string]interface{}
					fields, err = commonClient.TaskExecutionAuthorizationFields(issued)
					if err == nil {
						err = s.repo.AttachPendingAuthorization(ctx, tenantID, execution.ExecutionID, fields)
					}
				}
			}
		} else if err == nil {
			err = fmt.Errorf("plan has no bindings")
		}
		if err != nil {
			finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if failureErr := s.repo.FailPendingExecution(finalizeCtx, taskID, tenantID, execution.ExecutionID, "quality.authorization.issue_failed", time.Now().UTC()); failureErr != nil {
				return "", fmt.Errorf("authorization: %v; finalize: %w", err, failureErr)
			}
			return "", err
		}
	}
	return execution.ExecutionID, nil
}
