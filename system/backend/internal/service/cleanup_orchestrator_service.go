package service

import (
	"context"
	"errors"
	"fmt"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"sort"
	"strings"
	"time"
)

var (
	ErrCleanupManagerScopeRequired             = errors.New("Meta cleanup requires Manager in the same assessment")
	ErrCleanupExecuteConfirmRequired           = errors.New("resource reclaim execute confirmation is required")
	ErrCleanupExecuteConfirmTokenRequired      = errors.New("resource reclaim execute confirmation token is required")
	ErrCleanupExternalArtifactPolicyRequired   = errors.New("physical resource reclaim requires an external artifact policy")
	ErrCleanupExternalArtifactPolicyInvalid    = errors.New("external artifact policy is invalid")
	ErrCleanupExternalArtifactPolicyNotAllowed = errors.New("external artifact policy is only allowed for physical resource reclaim")
)

type CleanupOrchestratorService struct {
	coordinator *cleanupCoordinator
	registry    *ModuleRegistryService
	reads       client.ExecutionReadResolver
}

func NewCleanupOrchestratorService(db *gorm.DB, registry *ModuleRegistryService) *CleanupOrchestratorService {
	return &CleanupOrchestratorService{coordinator: newCleanupCoordinator(db), registry: registry, reads: client.NewExecutionReadAuthorizer(cleanupRegistryReader{db})}
}

// Run is the only parent supervisor. Restart resumes aggregation from the
// original deadline; it never creates children or dispatches Owner work.
func (s *CleanupOrchestratorService) Run(ctx context.Context) {
	worker := "system-cleanup:" + uuid.NewString()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.coordinator.tick(ctx, worker); err != nil && ctx.Err() == nil {
			logger.L().Error("Cleanup coordination failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *CleanupOrchestratorService) CreateScanTask(ctx context.Context, requestID string, tenantID uint, scope []string, userID uint) (string, error) {
	record, err := s.coordinator.acceptUser(ctx, cleanupSubmission{RequestID: requestID, TenantID: tenantID, RequestedBy: userID, Action: events.CleanupActionScan, Scope: scope, TriggerType: execution.TriggerTypeManual}, s.cleanupDiscovery(ctx))
	if err != nil {
		return "", err
	}
	return record.RequestID, nil
}

type CleanupExecuteConfirmation struct {
	Confirmed              bool
	ConfirmationToken      string
	ExternalArtifactPolicy string
}

func (s *CleanupOrchestratorService) CreateExecuteTask(ctx context.Context, requestID, basedOnScan, mode string, tenantID, userID uint, confirmation CleanupExecuteConfirmation) (string, error) {
	if events.ValidateCleanupMode(mode) != nil {
		return "", commonapi.ErrBadRequest
	}
	snapshot, err := s.tenantSnapshot(ctx, basedOnScan, tenantID)
	if err != nil {
		return "", err
	}
	if snapshot.Input.Action != events.CleanupActionScan || snapshot.Parent.Status != execution.ExecutionStatusSuccess || snapshot.Input.CauseEvent != "" || snapshot.Input.Context.EngineID != 0 || snapshot.Input.Context.TenantID != 0 {
		return "", commonapi.ErrConflict
	}
	if err := validateCleanupExecuteScope(snapshot.Input.ExpectedModules); err != nil {
		return "", err
	}
	cutoff, err := snapshot.Record.FrozenCutoff()
	if err != nil || cutoff == nil {
		return "", commonapi.ErrConflict
	}
	summary, _, digest, err := cleanupReceiptSummary(cutoff.Receipts)
	if err != nil {
		return "", err
	}
	if err = validateCleanupExecuteConfirmation(mode, summary, confirmation); err != nil {
		return "", err
	}
	input := snapshot.Input
	context := input.Context
	if mode == events.CleanupModePhysical {
		context.ExternalArtifactPolicy = strings.TrimSpace(confirmation.ExternalArtifactPolicy)
	}
	record, err := s.coordinator.acceptUser(ctx, cleanupSubmission{RequestID: requestID, TenantID: tenantID, RequestedBy: userID, Action: events.CleanupActionExecute, CleanupMode: mode, Scope: input.ExpectedModules, TriggerType: execution.TriggerTypeManual, CauseEvent: input.CauseEvent, BasedOnScan: basedOnScan, Context: context, Confirmation: events.CleanupConfirmation{Confirmed: true, TextProvided: confirmation.ConfirmationToken == "CONFIRM", RiskLevel: summary.RiskLevel, ImpactDigest: digest}}, func(_ []string, _ string) ([]string, error) { return input.ExpectedModules, nil })
	if err != nil {
		return "", err
	}
	return record.RequestID, nil
}

// tenantSnapshot is an internal fact read after the entrypoint authorizes
// the current Tenant action; it is never returned as a child read decision.
func (s *CleanupOrchestratorService) tenantSnapshot(ctx context.Context, id string, tenant uint) (*cleanupSnapshot, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id {
		return nil, commonapi.ErrBadRequest
	}
	if tenant == 0 {
		return nil, commonapi.ErrForbidden
	}
	var record models.CleanupRequestRecord
	err = s.coordinator.db.WithContext(ctx).Where("request_id = ? AND tenant_id = ?", id, tenant).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, commonapi.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	snapshot, err := s.coordinator.snapshot(ctx, id, tenant)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *CleanupOrchestratorService) resolveExpectedModules(scope []string) ([]string, error) {
	enabledModules, err := s.enabledCleanupExecutorModules()
	if err != nil {
		return nil, err
	}
	if len(enabledModules) == 0 {
		return nil, fmt.Errorf("未发现已注册且启用的资源回收执行方模块")
	}

	enabledSet := make(map[string]struct{}, len(enabledModules))
	for _, module := range enabledModules {
		enabledSet[module] = struct{}{}
	}

	if len(scope) == 0 {
		return enabledModules, nil
	}

	result := make([]string, 0, len(scope))
	seen := make(map[string]struct{}, len(scope))
	for _, module := range scope {
		normalized := strings.TrimSpace(module)
		if normalized == "" {
			continue
		}
		if _, duplicated := seen[normalized]; duplicated {
			continue
		}
		if _, ok := enabledSet[normalized]; !ok {
			return nil, fmt.Errorf("模块 %s 未注册或未启用资源回收执行方", normalized)
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("资源回收评估范围不能为空")
	}
	return result, nil
}

func (s *CleanupOrchestratorService) resolveExpectedModulesForCause(scope []string, causeEvent string) ([]string, error) {
	if causeEvent != events.CleanupCauseEngineDeleting {
		return s.resolveExpectedModules(scope)
	}
	if len(scope) > 0 {
		return nil, fmt.Errorf("Engine 删除影响评估不允许缩小模块范围")
	}
	if s.registry == nil {
		return nil, fmt.Errorf("module registry service is not configured")
	}
	modules, err := s.registry.ListModules()
	if err != nil {
		return nil, fmt.Errorf("查询 Engine 删除影响评估模块失败: %w", err)
	}

	expected := make([]string, 0, len(modules))
	unavailable := make([]string, 0)
	for _, module := range modules {
		declared, available := moduleCleanupExecutorState(module, causeEvent)
		if !declared {
			continue
		}
		expected = append(expected, module.ModuleName)
		if !available {
			unavailable = append(unavailable, module.ModuleName)
		}
	}
	sort.Strings(expected)
	sort.Strings(unavailable)
	if len(expected) == 0 {
		return nil, fmt.Errorf("未发现支持 Engine 删除影响评估的模块")
	}
	if len(unavailable) > 0 {
		return nil, fmt.Errorf("以下模块当前不可用，无法完整评估 Engine 删除影响: %s", strings.Join(unavailable, ", "))
	}
	return expected, nil
}

func (s *CleanupOrchestratorService) enabledCleanupExecutorModules() ([]string, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("module registry service is not configured")
	}
	modules, err := s.registry.ListActiveModules()
	if err != nil {
		return nil, fmt.Errorf("查询已注册资源回收执行方模块失败: %w", err)
	}

	result := make([]string, 0, len(modules))
	for _, module := range modules {
		if module == nil {
			continue
		}
		if moduleHasEnabledCleanupExecutor(module) {
			result = append(result, module.ModuleName)
		}
	}
	sort.Strings(result)
	return result, nil
}

func moduleCleanupExecutorState(module *models.ModuleInfo, causeEvent string) (bool, bool) {
	if module == nil {
		return false, false
	}
	declared := false
	for _, instance := range module.Instances {
		if !cleanupExecutorSupportsCause(instance.Metadata, causeEvent) {
			continue
		}
		declared = true
		if module.Enabled && instance.Role == models.ModuleRuntimeRoleBackend && instance.Status == models.ModuleRuntimeStatusUp && instance.LeaseExpiresAt.After(time.Now()) {
			return true, true
		}
	}
	return declared, false
}

func moduleHasEnabledCleanupExecutor(module *models.ModuleInfo) bool {
	if module == nil || !module.Enabled {
		return false
	}
	for _, instance := range module.Instances {
		if instance.Role == models.ModuleRuntimeRoleBackend && instance.Status == models.ModuleRuntimeStatusUp && instance.LeaseExpiresAt.After(time.Now()) && cleanupExecutorEnabled(instance.Metadata) {
			return true
		}
	}
	return false
}

func cleanupExecutorEnabled(metadata map[string]interface{}) bool {
	capabilities, ok := metadata["capabilities"].(map[string]interface{})
	if !ok {
		return false
	}
	cleanupExecutor, ok := capabilities["cleanup_executor"].(map[string]interface{})
	if !ok {
		return false
	}
	enabled, ok := cleanupExecutor["enabled"].(bool)
	return ok && enabled
}

func cleanupExecutorSupportsCause(metadata map[string]interface{}, causeEvent string) bool {
	if !cleanupExecutorEnabled(metadata) {
		return false
	}
	capabilities, _ := metadata["capabilities"].(map[string]interface{})
	cleanupExecutor, _ := capabilities["cleanup_executor"].(map[string]interface{})
	switch causes := cleanupExecutor["causes"].(type) {
	case []interface{}:
		for _, value := range causes {
			if cause, ok := value.(string); ok && strings.TrimSpace(cause) == causeEvent {
				return true
			}
		}
	case []string:
		for _, cause := range causes {
			if strings.TrimSpace(cause) == causeEvent {
				return true
			}
		}
	}
	return false
}

func validateCleanupExecuteConfirmation(cleanupMode string, summary events.CleanupResultSummary, confirmation CleanupExecuteConfirmation) error {
	if !confirmation.Confirmed {
		return ErrCleanupExecuteConfirmRequired
	}
	policy := strings.TrimSpace(confirmation.ExternalArtifactPolicy)
	if cleanupMode == events.CleanupModePhysical {
		if policy == "" {
			return ErrCleanupExternalArtifactPolicyRequired
		}
		if policy != commonModels.ExternalArtifactPolicyDelete && policy != commonModels.ExternalArtifactPolicyAbandon {
			return ErrCleanupExternalArtifactPolicyInvalid
		}
	} else if policy != "" {
		return ErrCleanupExternalArtifactPolicyNotAllowed
	}
	if cleanupMode != events.CleanupModePhysical && summary.RiskLevel != "high" {
		return nil
	}
	if confirmation.ConfirmationToken != "CONFIRM" {
		return ErrCleanupExecuteConfirmTokenRequired
	}
	return nil
}

func higherRisk(left, right string) string {
	rank := map[string]int{"": 0, "low": 1, "medium": 2, "high": 3}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func (s *CleanupOrchestratorService) cleanupDiscovery(ctx context.Context) func([]string, string) ([]string, error) {
	scoped := *s
	if s.registry != nil {
		registry := *s.registry
		registry.repo = repository.NewModuleRegistryRepository(s.coordinator.db.WithContext(ctx))
		scoped.registry = &registry
	}
	return scoped.resolveExpectedModulesForCause
}

func (s *CleanupOrchestratorService) ownedSnapshot(ctx context.Context, id string, tenant, user uint) (*cleanupSnapshot, error) {
	snapshot, err := s.tenantSnapshot(ctx, id, tenant)
	if err != nil {
		return nil, err
	}
	if user == 0 || snapshot.Record.RequestedBy != user {
		return nil, commonapi.ErrNotFound
	}
	return snapshot, nil
}

// Scope is inherited from the immutable assessment; never add a hidden Owner.
func validateCleanupExecuteScope(scope []string) error {
	var meta, manager bool
	for _, owner := range scope {
		meta = meta || owner == events.ModuleMeta
		manager = manager || owner == events.ModuleManager
	}
	if meta && !manager {
		return ErrCleanupManagerScopeRequired
	}
	return nil
}
