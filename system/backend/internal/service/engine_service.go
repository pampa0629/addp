package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/dbbridge"
	engineplugin "github.com/addp/common/engine/plugin"
	supermapworkflow "github.com/addp/common/engine/plugins/supermap_workflow"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/events"
	commonsecurity "github.com/addp/common/security"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	ErrResourceNotFound          = errors.New("资源不存在")
	ErrResourceForbidden         = errors.New("没有权限访问该资源")
	ErrBuiltinResourceImmutable  = errors.New("内置资源不可删除或修改")
	ErrUnsupportedEngineType     = errors.New("不支持的系统引擎类型")
	ErrSpatialWorkspaceNotFound  = errors.New("未找到可启用的空间工作区")
	ErrEngineIdentityImmutable   = errors.New("引擎物理端点身份不可修改")
	ErrEngineDeleting            = errors.New("引擎正在删除，不能执行该操作")
	ErrEngineDeleted             = errors.New("引擎已删除，不能执行该操作")
	ErrEngineRestoreRequired     = errors.New("相同身份的引擎已删除，必须显式恢复")
	ErrEngineVersionConflict     = errors.New("引擎已被其他操作修改，请刷新后重试")
	ErrInvalidEngineLifecycle    = errors.New("无效的引擎生命周期状态")
	ErrInvalidArtifactPolicy     = errors.New("无效的外部产物处理策略")
	ErrEngineCleanupUnavailable  = errors.New("引擎删除所需的资源回收服务不可用")
	ErrDeletionAssessmentInvalid = errors.New("引擎删除影响评估无效")
	ErrDeletionAssessmentPending = errors.New("引擎删除影响评估尚未完成")
	ErrDeletionAssessmentExpired = errors.New("引擎删除影响评估已过期")
	ErrDeletionImpactChanged     = errors.New("引擎删除影响已经变化，需要重新确认")
	ErrDeletionRunningExecutions = errors.New("仍有运行任务正在使用该引擎")
	ErrDeletionConfirmation      = errors.New("删除确认文本与引擎名称不一致")
)

type EngineRestoreRequiredError struct {
	EngineID uint
}

func (e *EngineRestoreRequiredError) Error() string {
	return fmt.Sprintf("%s（engine_id=%d）", ErrEngineRestoreRequired.Error(), e.EngineID)
}

func (e *EngineRestoreRequiredError) Unwrap() error { return ErrEngineRestoreRequired }

var disallowedSystemEngineTypes = map[string]struct{}{
	"sqlite":     {},
	"spatialite": {},
}

type EngineService struct {
	repo           *repository.EngineRepository
	encryptionKey  []byte
	eventPublisher *events.EngineEventPublisher
	cleanup        EngineCleanupOrchestrator
}

type EngineCleanupOrchestrator interface {
	CreateEngineDeletionAssessment(context.Context, uint, uint, map[string]interface{}) (string, error)
	CreateExecuteTask(context.Context, string, string, uint, CleanupExecuteConfirmation) (string, error)
	GetTaskStatus(context.Context, string) (*models.TaskStatusResponse, error)
}

type EngineListFilter struct {
	EngineType       string
	CapabilityGroups []string
	EngineOrigins    []string
	IncludeBuiltin   bool
	LifecycleStates  []string
}

func NewEngineService(repo *repository.EngineRepository, encryptionKey []byte, redisClient *redis.Client) *EngineService {
	return &EngineService{
		repo:           repo,
		encryptionKey:  encryptionKey,
		eventPublisher: events.NewEngineEventPublisher(redisClient, nil),
	}
}

func (s *EngineService) WithCleanupOrchestrator(cleanup *CleanupOrchestratorService) *EngineService {
	s.cleanup = cleanup
	return s
}

func (s *EngineService) persistEngine(engine *models.Engine) error {
	if err := s.repo.Update(engine); err != nil {
		if errors.Is(err, repository.ErrEngineVersionConflict) {
			return ErrEngineVersionConflict
		}
		return err
	}
	return nil
}

func (s *EngineService) Create(req *models.EngineCreateRequest, createdBy, tenantID uint) (*models.Engine, bool, error) {
	if req == nil {
		return nil, false, errors.New("无效的请求数据")
	}
	identityKey, err := buildConnectionIdentityKey(req.EngineType, req.ConnectionInfo)
	if err != nil {
		return nil, false, err
	}
	tenantPtr := &tenantID
	if existing, err := s.repo.FindByIdentityKey(req.EngineType, tenantPtr, identityKey); err == nil {
		return s.resolveIdempotentRegistration(existing)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	if existing, err := s.repo.FindByNameAndTenant(req.Name, tenantID); err == nil && existing != nil {
		return nil, false, fmt.Errorf("资源名称 '%s' 已存在，请使用其他名称", req.Name)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	// 加密敏感字段
	encryptedConnInfo, err := s.encryptConnectionInfoForStorage(req.EngineType, req.ConnectionInfo)
	if err != nil {
		return nil, false, fmt.Errorf("加密连接信息失败: %w", err)
	}

	engine := &models.Engine{
		Name:                   req.Name, // 显示名称
		EngineType:             req.EngineType,
		ConnectionInfo:         encryptedConnInfo,
		IdentityKey:            identityKey,
		Version:                1,
		Description:            req.Description,
		CreatedBy:              &createdBy,
		TenantID:               &tenantID,
		LifecycleState:         models.EngineLifecycleActive,
		ExternalArtifactPolicy: models.ExternalArtifactPolicyDelete,
	}

	// 设置引擎来源（如果提供）
	if req.EngineOrigin != "" {
		engine.EngineOrigin = req.EngineOrigin
	}

	// 保存能力声明（如果提供）
	if req.Capabilities != nil {
		engine.Capabilities = req.Capabilities
	}

	if err := s.prepareEngineCapabilities(engine); err != nil {
		return nil, false, err
	}

	if err := s.repo.Create(engine); err != nil {
		// 唯一索引解决并发注册竞争；输掉竞争的一方读取并返回永久实例。
		if existing, findErr := s.repo.FindByIdentityKey(req.EngineType, tenantPtr, identityKey); findErr == nil {
			return s.resolveIdempotentRegistration(existing)
		}
		return nil, false, err
	}

	// 发布资源创建事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(context.Background(), engine.ID, events.ActionCreate)
	}

	return s.sanitizeResource(engine), true, nil
}

func (s *EngineService) resolveIdempotentRegistration(existing *models.Engine) (*models.Engine, bool, error) {
	if existing == nil {
		return nil, false, ErrResourceNotFound
	}
	switch existing.LifecycleState {
	case models.EngineLifecycleActive, models.EngineLifecycleDisabled:
		return s.sanitizeResource(existing), false, nil
	case models.EngineLifecycleDeleting:
		return nil, false, ErrEngineDeleting
	case models.EngineLifecycleDeleted:
		return nil, false, &EngineRestoreRequiredError{EngineID: existing.ID}
	default:
		return nil, false, fmt.Errorf("%w: %s", ErrInvalidEngineLifecycle, existing.LifecycleState)
	}
}

// CreateInternal 供内部服务调用创建资源
func (s *EngineService) CreateInternal(req *models.EngineCreateRequest, tenantID uint, createdBy *uint) (*models.Engine, error) {
	if req == nil {
		return nil, errors.New("无效的请求数据")
	}

	identityKey, err := buildConnectionIdentityKey(req.EngineType, req.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	var tenantPtr *uint
	if tenantID > 0 {
		tenantPtr = &tenantID
	}
	if existing, findErr := s.repo.FindByIdentityKey(req.EngineType, tenantPtr, identityKey); findErr == nil {
		engine, _, resolveErr := s.resolveIdempotentRegistration(existing)
		return engine, resolveErr
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}

	encryptedConnInfo, err := s.encryptConnectionInfoForStorage(req.EngineType, req.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("加密连接信息失败: %w", err)
	}

	engine := &models.Engine{
		Name:                   req.Name, // 显示名称
		EngineType:             req.EngineType,
		ConnectionInfo:         encryptedConnInfo,
		IdentityKey:            identityKey,
		Version:                1,
		Description:            req.Description,
		TenantID:               tenantPtr,
		LifecycleState:         models.EngineLifecycleActive,
		ExternalArtifactPolicy: models.ExternalArtifactPolicyDelete,
		CreatedBy:              createdBy,
	}

	if req.Capabilities != nil {
		engine.Capabilities = req.Capabilities
	}

	if err := s.prepareEngineCapabilities(engine); err != nil {
		return nil, err
	}

	if err := s.repo.Create(engine); err != nil {
		if existing, findErr := s.repo.FindByIdentityKey(req.EngineType, tenantPtr, identityKey); findErr == nil {
			resolved, _, resolveErr := s.resolveIdempotentRegistration(existing)
			return resolved, resolveErr
		}
		return nil, err
	}

	// 发布资源创建事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(context.Background(), engine.ID, events.ActionCreate)
	}

	return s.sanitizeResource(engine), nil
}

func (s *EngineService) GetByID(id, tenantID uint) (*models.Engine, error) {
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(engine, tenantID); err != nil {
		return nil, err
	}

	return s.sanitizeResource(engine), nil
}

func (s *EngineService) List(filter EngineListFilter, tenantID uint) ([]models.Engine, error) {
	lifecycleStates, err := normalizeLifecycleStateFilter(filter.LifecycleStates)
	if err != nil {
		return nil, err
	}
	engines, err := s.repo.ListVisibleByTenant(tenantID, filter.EngineType, lifecycleStates)

	if err != nil {
		return nil, err
	}

	filtered := filterEngines(engines, filter)
	sanitized := make([]models.Engine, 0, len(filtered))
	for i := range filtered {
		sanitized = append(sanitized, *s.sanitizeResource(&filtered[i]))
	}

	return sanitized, nil
}

func (s *EngineService) ListRuntimeDescriptors(
	page, pageSize int,
	filter EngineListFilter,
	tenantID uint,
) ([]models.EngineRuntimeDescriptor, int64, error) {
	lifecycleStates, err := normalizeLifecycleStateFilter(filter.LifecycleStates)
	if err != nil {
		return nil, 0, err
	}
	engines, err := s.repo.ListVisibleByTenant(tenantID, filter.EngineType, lifecycleStates)
	if err != nil {
		return nil, 0, err
	}
	filtered := filterEngines(engines, filter)
	total := int64(len(filtered))
	start := (page - 1) * pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	descriptors := make([]models.EngineRuntimeDescriptor, 0, end-start)
	for index := start; index < end; index++ {
		descriptor, err := s.buildRuntimeDescriptor(&filtered[index])
		if err != nil {
			return nil, 0, err
		}
		descriptors = append(descriptors, *descriptor)
	}
	return descriptors, total, nil
}

func (s *EngineService) GetRuntimeDescriptor(id, tenantID uint) (*models.EngineRuntimeDescriptor, error) {
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}
	if err := s.authorizeResourceAccess(engine, tenantID); err != nil {
		return nil, err
	}
	return s.buildRuntimeDescriptor(engine)
}

func (s *EngineService) buildRuntimeDescriptor(engine *models.Engine) (*models.EngineRuntimeDescriptor, error) {
	if engine == nil {
		return nil, ErrResourceNotFound
	}
	descriptor := &models.EngineRuntimeDescriptor{
		ID: engine.ID, Name: engine.Name, EngineType: engine.EngineType,
		EngineOrigin: engine.EngineOrigin, Description: engine.Description,
		LifecycleState: engine.LifecycleState, IsBuiltin: engine.IsBuiltin,
		Capabilities: engine.Capabilities, ConnectionStatus: engine.ConnectionStatus,
	}
	if !engineselection.SupportsComputeEntrypoint(engine, "workflow") &&
		!engineselection.SupportsComputeEntrypoint(engine, "script") &&
		!engineselection.SupportsComputeEntrypoint(engine, "inference") &&
		!supportsFederatedQueryRuntime(engine) {
		return descriptor, nil
	}
	connectionInfo, err := s.decryptStoredConnectionInfo(engine.EngineType, engine.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密运行时端点失败: %w", err)
	}
	protocol := strings.TrimSpace(engineplugin.GetString(engineplugin.ConnectionInfo(connectionInfo), "protocol"))
	if protocol == "" {
		protocol = "http"
	}
	host := strings.TrimSpace(engineplugin.GetString(engineplugin.ConnectionInfo(connectionInfo), "host"))
	port := engineplugin.GetInt(engineplugin.ConnectionInfo(connectionInfo), "port")
	if host == "" || port <= 0 {
		return nil, fmt.Errorf("运行时引擎 %d 缺少有效 host/port", engine.ID)
	}
	descriptor.RuntimeEndpoint = &models.EngineRuntimeEndpoint{Protocol: protocol, Host: host, Port: port}
	return descriptor, nil
}

func supportsFederatedQueryRuntime(engine *models.Engine) bool {
	if engine == nil || engine.Capabilities == nil {
		return false
	}
	capabilities, err := engineplugin.ParseEngineCapabilities(string(*engine.Capabilities))
	return err == nil && capabilities != nil && capabilities.Compute != nil &&
		capabilities.Compute.Query != nil && capabilities.Compute.Query.Supported &&
		capabilities.Compute.Query.Federation != nil && capabilities.Compute.Query.Federation.Supported
}

func filterEngines(engines []models.Engine, filter EngineListFilter) []models.Engine {
	capabilityGroups := stringSet(filter.CapabilityGroups)
	engineOrigins := stringSet(filter.EngineOrigins)
	filtered := make([]models.Engine, 0, len(engines))

	for _, engine := range engines {
		if !filter.IncludeBuiltin && engine.IsBuiltin {
			continue
		}
		if len(engineOrigins) > 0 {
			if _, ok := engineOrigins[engine.EngineOrigin]; !ok {
				continue
			}
		}
		if len(capabilityGroups) > 0 && !matchesCapabilityGroup(engine.Capabilities, capabilityGroups) {
			continue
		}
		filtered = append(filtered, engine)
	}

	return filtered
}

func matchesCapabilityGroup(capabilitiesJSON *models.JSONString, groups map[string]struct{}) bool {
	if capabilitiesJSON == nil || *capabilitiesJSON == "" {
		return false
	}
	capabilities, err := engineplugin.ParseEngineCapabilities(string(*capabilitiesJSON))
	if err != nil || capabilities == nil {
		return false
	}
	if _, ok := groups["storage"]; ok && capabilities.Storage != nil {
		return true
	}
	if _, ok := groups["compute"]; ok && capabilities.Compute != nil {
		return true
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func normalizeLifecycleStateFilter(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{models.EngineLifecycleActive}, nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		state := strings.TrimSpace(value)
		switch state {
		case models.EngineLifecycleActive, models.EngineLifecycleDisabled, models.EngineLifecycleDeleting, models.EngineLifecycleDeleted:
		default:
			return nil, fmt.Errorf("%w: %s", ErrInvalidEngineLifecycle, state)
		}
		if _, ok := seen[state]; ok {
			continue
		}
		seen[state] = struct{}{}
		result = append(result, state)
	}
	return result, nil
}

func (s *EngineService) Update(id, tenantID uint, req *models.EngineUpdateRequest) (*models.Engine, error) {
	if req == nil || req.Version < 1 {
		return nil, ErrEngineVersionConflict
	}
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(engine, tenantID); err != nil {
		return nil, err
	}
	if engine.LifecycleState == models.EngineLifecycleDeleting {
		return nil, ErrEngineDeleting
	}
	if engine.LifecycleState == models.EngineLifecycleDeleted {
		return nil, ErrEngineDeleted
	}
	if engine.Version != req.Version {
		return nil, ErrEngineVersionConflict
	}

	// 检查是否为内置资源（内置资源不允许修改核心配置）
	if engine.IsBuiltin {
		// 允许修改描述和显示名称，但不允许修改连接信息、名称等核心配置
		if req.Name != nil || req.ConnectionInfo != nil {
			return nil, ErrBuiltinResourceImmutable
		}
	}

	if req.Name != nil {
		engine.Name = *req.Name
	}
	if req.ConnectionInfo != nil {
		plainConnInfo, err := s.decryptStoredConnectionInfo(engine.EngineType, engine.ConnectionInfo)
		if err != nil {
			return nil, fmt.Errorf("解密连接信息失败: %w", err)
		}
		// 合并明文连接信息：如果新值是脱敏占位符，保留原值。
		mergedConnInfo := s.mergePlainConnectionInfo(engine.EngineType, plainConnInfo, *req.ConnectionInfo)
		if err := s.validateConnectionIdentityUnchanged(engine.EngineType, plainConnInfo, mergedConnInfo); err != nil {
			return nil, err
		}

		// 加密敏感字段
		encryptedConnInfo, err := s.encryptConnectionInfoForStorage(engine.EngineType, mergedConnInfo)
		if err != nil {
			return nil, fmt.Errorf("加密连接信息失败: %w", err)
		}
		engine.ConnectionInfo = encryptedConnInfo
	}
	if req.Description != nil {
		engine.Description = *req.Description
	}
	if req.LifecycleState != nil {
		state := strings.TrimSpace(*req.LifecycleState)
		if state != models.EngineLifecycleActive && state != models.EngineLifecycleDisabled {
			return nil, fmt.Errorf("%w: %s", ErrInvalidEngineLifecycle, state)
		}
		engine.LifecycleState = state
	}
	if req.Capabilities != nil {
		engine.Capabilities = req.Capabilities
	}
	if req.ConnectionInfo != nil || req.Capabilities != nil {
		if err := s.prepareEngineCapabilities(engine); err != nil {
			return nil, err
		}
	}

	if err := s.persistEngine(engine); err != nil {
		return nil, err
	}

	// 发布资源更新事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(context.Background(), engine.ID, events.ActionUpdate)
	}

	return s.sanitizeResource(engine), nil
}

func (s *EngineService) CreateDeletionAssessment(id, tenantID, actorID uint, externalArtifactPolicy string) (string, error) {
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrResourceNotFound
		}
		return "", err
	}

	if err := s.authorizeResourceAccess(engine, tenantID); err != nil {
		return "", err
	}
	if engine.IsBuiltin {
		return "", ErrBuiltinResourceImmutable
	}
	if engine.LifecycleState == models.EngineLifecycleDeleting {
		return "", ErrEngineDeleting
	}
	if engine.LifecycleState == models.EngineLifecycleDeleted {
		return "", ErrEngineDeleted
	}
	if s.cleanup == nil {
		return "", ErrEngineCleanupUnavailable
	}
	policy, err := normalizeExternalArtifactPolicy(externalArtifactPolicy)
	if err != nil {
		return "", err
	}
	if engine.TenantID == nil {
		return "", errors.New("平台共享引擎不支持租户删除工作流")
	}
	cleanupContext := cleanupEngineDeletionContext(id, policy)
	cleanupContext["assessment_phase"] = "preflight"
	taskID, err := s.cleanup.CreateEngineDeletionAssessment(
		context.Background(), *engine.TenantID, actorID, cleanupContext,
	)
	if err != nil {
		return "", err
	}
	return taskID, nil
}

func (s *EngineService) GetDeletionAssessment(id, tenantID uint, assessmentID string) (*models.TaskStatusResponse, error) {
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}
	if err := s.authorizeResourceAccess(engine, tenantID); err != nil {
		return nil, err
	}
	if s.cleanup == nil {
		return nil, ErrEngineCleanupUnavailable
	}
	status, err := s.cleanup.GetTaskStatus(context.Background(), strings.TrimSpace(assessmentID))
	if err != nil {
		return nil, err
	}
	if err := validateDeletionAssessmentIdentity(status, id, tenantID); err != nil {
		return nil, err
	}
	return status, nil
}

func (s *EngineService) BeginDeletion(id, tenantID, actorID uint, req *models.EngineDeleteRequest) (*models.Engine, error) {
	if req == nil || req.Version < 1 || strings.TrimSpace(req.AssessmentID) == "" {
		return nil, ErrDeletionAssessmentInvalid
	}
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}
	if err := s.authorizeResourceAccess(engine, tenantID); err != nil {
		return nil, err
	}
	if engine.IsBuiltin {
		return nil, ErrBuiltinResourceImmutable
	}
	if engine.LifecycleState == models.EngineLifecycleDeleting {
		return nil, ErrEngineDeleting
	}
	if engine.LifecycleState == models.EngineLifecycleDeleted {
		return nil, ErrEngineDeleted
	}
	if engine.Version != req.Version {
		return nil, ErrEngineVersionConflict
	}
	if s.cleanup == nil {
		return nil, ErrEngineCleanupUnavailable
	}
	policy, err := normalizeExternalArtifactPolicy(req.ExternalArtifactPolicy)
	if err != nil {
		return nil, err
	}
	if engine.TenantID == nil {
		return nil, errors.New("平台共享引擎不支持租户删除工作流")
	}
	if strings.TrimSpace(req.ConfirmationToken) != engine.Name {
		return nil, ErrDeletionConfirmation
	}
	assessment, err := s.GetDeletionAssessment(id, tenantID, req.AssessmentID)
	if err != nil {
		return nil, err
	}
	confirmedDigest, err := validateDeletionAssessmentReady(assessment, policy, time.Now())
	if err != nil {
		return nil, err
	}

	now := time.Now()
	engine.LifecycleState = models.EngineLifecycleDeleting
	engine.ExternalArtifactPolicy = policy
	engine.DeletionRequestedAt = &now
	engine.DeletionRequestedBy = &actorID
	engine.DeletionScanTaskID = nil
	engine.DeletionExecuteTaskID = nil
	engine.DeletionError = ""
	if err := s.persistEngine(engine); err != nil {
		return nil, err
	}

	cleanupContext := cleanupEngineDeletionContext(id, policy)
	cleanupContext["assessment_phase"] = "validation"
	cleanupContext["confirmed_assessment_id"] = strings.TrimSpace(req.AssessmentID)
	cleanupContext["confirmed_impact_digest"] = confirmedDigest
	scanTaskID, err := s.cleanup.CreateEngineDeletionAssessment(
		context.Background(), *engine.TenantID, actorID, cleanupContext,
	)
	if err != nil {
		engine.DeletionError = err.Error()
		_ = s.persistEngine(engine)
		return nil, err
	}
	engine.DeletionScanTaskID = &scanTaskID
	if err := s.persistEngine(engine); err != nil {
		return nil, err
	}
	go s.continueDeletion(engine.ID, scanTaskID)
	return s.sanitizeResource(engine), nil
}

func normalizeExternalArtifactPolicy(value string) (string, error) {
	policy := strings.TrimSpace(value)
	if policy == "" {
		policy = models.ExternalArtifactPolicyDelete
	}
	switch policy {
	case models.ExternalArtifactPolicyDelete, models.ExternalArtifactPolicyAbandon:
		return policy, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrInvalidArtifactPolicy, policy)
	}
}

func cleanupEngineDeletionContext(engineID uint, policy string) map[string]interface{} {
	return map[string]interface{}{
		"engine_id":                engineID,
		"external_artifact_policy": policy,
	}
}

func validateDeletionAssessmentIdentity(status *models.TaskStatusResponse, engineID, tenantID uint) error {
	if status == nil || status.Task.Action != events.CleanupActionScan || status.Task.CauseEvent != events.CleanupCauseEngineDeleting {
		return ErrDeletionAssessmentInvalid
	}
	if status.Task.TenantID != tenantID {
		return ErrResourceForbidden
	}
	contextEngineID, ok := cleanupContextUint(status.Task.Context, "engine_id")
	if !ok || contextEngineID != engineID {
		return ErrDeletionAssessmentInvalid
	}
	return nil
}

func validateDeletionAssessmentReady(status *models.TaskStatusResponse, policy string, now time.Time) (string, error) {
	if status == nil || status.Status != "completed" {
		return "", ErrDeletionAssessmentPending
	}
	if strings.TrimSpace(stringFromContext(status.Task.Context, "assessment_phase")) != "preflight" {
		return "", ErrDeletionAssessmentInvalid
	}
	if strings.TrimSpace(stringFromContext(status.Task.Context, "external_artifact_policy")) != policy {
		return "", ErrDeletionAssessmentInvalid
	}
	startedAt, err := time.Parse(time.RFC3339, status.Task.StartedAt)
	if err != nil || now.Sub(startedAt) > 10*time.Minute {
		return "", ErrDeletionAssessmentExpired
	}
	impact, digest, err := validatedDeletionImpact(status)
	if err != nil {
		return "", err
	}
	if impact.Running > 0 {
		return "", ErrDeletionRunningExecutions
	}
	return digest, nil
}

func validatedDeletionImpact(status *models.TaskStatusResponse) (events.CleanupImpactSummary, string, error) {
	if status == nil || len(status.Task.ExpectedModules) == 0 {
		return events.CleanupImpactSummary{}, "", ErrDeletionAssessmentInvalid
	}
	for _, module := range status.Task.ExpectedModules {
		value, ok := status.Results[module]
		if !ok {
			return events.CleanupImpactSummary{}, "", ErrDeletionAssessmentPending
		}
		result, ok := value.(events.CleanupResultData)
		if !ok || result.Status != events.CleanupResultSuccess || result.Impact == nil || strings.TrimSpace(result.Impact.Digest) == "" {
			return events.CleanupImpactSummary{}, "", ErrDeletionAssessmentInvalid
		}
	}
	impact, digest := impactFromResults(status.Results)
	if strings.TrimSpace(digest) == "" {
		return events.CleanupImpactSummary{}, "", ErrDeletionAssessmentInvalid
	}
	return impact, digest, nil
}

func cleanupContextUint(values map[string]interface{}, key string) (uint, bool) {
	if values == nil {
		return 0, false
	}
	switch value := values[key].(type) {
	case uint:
		return value, value > 0
	case int:
		return uint(value), value > 0
	case int64:
		return uint(value), value > 0
	case float64:
		return uint(value), value > 0
	case json.Number:
		parsed, err := strconv.ParseUint(string(value), 10, 32)
		return uint(parsed), err == nil && parsed > 0
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
		return uint(parsed), err == nil && parsed > 0
	default:
		return 0, false
	}
}

func stringFromContext(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func (s *EngineService) continueDeletion(engineID uint, scanTaskID string) {
	ctx := context.Background()
	engine, err := s.repo.GetByID(engineID)
	if err != nil || engine.LifecycleState != models.EngineLifecycleDeleting || engine.DeletionScanTaskID == nil || *engine.DeletionScanTaskID != scanTaskID {
		return
	}

	executeTaskID := ""
	if engine.DeletionExecuteTaskID != nil {
		executeTaskID = strings.TrimSpace(*engine.DeletionExecuteTaskID)
	}
	if executeTaskID == "" {
		scanStatus, err := s.waitForCleanupTask(ctx, scanTaskID, 45*time.Second)
		if err != nil {
			s.setDeletionError(engineID, scanTaskID, fmt.Sprintf("cleanup scan failed: %v", err))
			return
		}
		if scanStatus.Status != "completed" {
			s.setDeletionError(engineID, scanTaskID, fmt.Sprintf("cleanup scan ended with status %s", scanStatus.Status))
			return
		}
		impact, digest, err := validatedDeletionImpact(scanStatus)
		if err != nil {
			s.setDeletionError(engineID, scanTaskID, fmt.Sprintf("validate deletion impact: %v", err))
			return
		}
		if impact.Running > 0 {
			s.setDeletionError(engineID, scanTaskID, ErrDeletionRunningExecutions.Error())
			return
		}
		confirmedDigest := strings.TrimSpace(stringFromContext(scanStatus.Task.Context, "confirmed_impact_digest"))
		if confirmedDigest == "" || confirmedDigest != digest {
			s.setDeletionError(engineID, scanTaskID, ErrDeletionImpactChanged.Error())
			return
		}
		executeTaskID, err = s.cleanup.CreateExecuteTask(
			ctx,
			scanTaskID,
			events.CleanupModePhysical,
			valueOrZero(engine.DeletionRequestedBy),
			CleanupExecuteConfirmation{Confirmed: true, ConfirmationToken: "CONFIRM"},
		)
		if err != nil {
			s.setDeletionError(engineID, scanTaskID, fmt.Sprintf("create cleanup execute task: %v", err))
			return
		}
		if !s.setDeletionExecuteTask(engineID, scanTaskID, executeTaskID) {
			return
		}
	}

	executeStatus, err := s.waitForCleanupTask(ctx, executeTaskID, 6*time.Minute)
	if err != nil {
		s.setDeletionError(engineID, scanTaskID, fmt.Sprintf("cleanup execute failed: %v", err))
		return
	}
	if executeStatus.Status != "completed" {
		s.setDeletionError(engineID, scanTaskID, fmt.Sprintf("cleanup execute ended with status %s", executeStatus.Status))
		return
	}

	current, err := s.repo.GetByID(engineID)
	if err != nil || current.LifecycleState != models.EngineLifecycleDeleting || current.DeletionScanTaskID == nil || *current.DeletionScanTaskID != scanTaskID {
		return
	}
	now := time.Now()
	current.LifecycleState = models.EngineLifecycleDeleted
	current.ConnectionStatus = models.EngineConnectionUnknown
	current.LastCheckAt = nil
	current.CheckMessage = ""
	current.ConnectionInfo = s.scrubSensitiveConnectionInfo(current.EngineType, current.ConnectionInfo)
	current.DeletedAt = &now
	current.DeletedBy = current.DeletionRequestedBy
	current.DeletionError = ""
	if err := s.persistEngine(current); err != nil {
		s.setDeletionError(engineID, scanTaskID, fmt.Sprintf("finalize engine deletion: %v", err))
		return
	}
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(context.Background(), engineID, events.ActionDelete)
	}
}

func (s *EngineService) Restore(id, tenantID, actorID uint, req *models.EngineRestoreRequest) (*models.Engine, error) {
	if req == nil || req.Version < 1 {
		return nil, ErrEngineVersionConflict
	}
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}
	if err := s.authorizeResourceAccess(engine, tenantID); err != nil {
		return nil, err
	}
	if engine.IsBuiltin {
		return nil, ErrBuiltinResourceImmutable
	}
	if engine.LifecycleState != models.EngineLifecycleDeleted {
		return nil, fmt.Errorf("%w: 只有 deleted 引擎可以恢复", ErrInvalidEngineLifecycle)
	}
	if engine.Version != req.Version {
		return nil, ErrEngineVersionConflict
	}
	identityKey, err := buildConnectionIdentityKey(engine.EngineType, req.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	if string(identityKey) != string(engine.IdentityKey) {
		return nil, ErrEngineIdentityImmutable
	}
	encryptedConnInfo, err := s.encryptConnectionInfoForStorage(engine.EngineType, req.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("加密连接信息失败: %w", err)
	}
	engine.Name = strings.TrimSpace(req.Name)
	engine.Description = req.Description
	engine.ConnectionInfo = encryptedConnInfo
	engine.Capabilities = req.Capabilities
	if err := s.prepareEngineCapabilities(engine); err != nil {
		return nil, err
	}
	now := time.Now()
	engine.LifecycleState = models.EngineLifecycleActive
	engine.ConnectionStatus = models.EngineConnectionUnknown
	engine.LastCheckAt = nil
	engine.CheckMessage = ""
	engine.DeletionScanTaskID = nil
	engine.DeletionExecuteTaskID = nil
	engine.DeletionError = ""
	engine.RestoredAt = &now
	engine.RestoredBy = &actorID
	if err := s.persistEngine(engine); err != nil {
		return nil, err
	}
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(context.Background(), engine.ID, events.ActionUpdate)
	}
	go s.CheckAndUpdateConnectionStatus(engine.ID)
	return s.sanitizeResource(engine), nil
}

func (s *EngineService) scrubSensitiveConnectionInfo(engineType string, connInfo models.ConnectionInfo) models.ConnectionInfo {
	if connInfo == nil {
		return models.ConnectionInfo{}
	}
	scrubbed := make(models.ConnectionInfo, len(connInfo))
	for field, value := range connInfo {
		if s.isSensitiveField(engineType, field) {
			continue
		}
		scrubbed[field] = value
	}
	return scrubbed
}

func (s *EngineService) waitForCleanupTask(ctx context.Context, taskID string, timeout time.Duration) (*models.TaskStatusResponse, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		status, err := s.cleanup.GetTaskStatus(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if status != nil && isTaskTerminal(status.Status) {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("task %s timed out", taskID)
		case <-ticker.C:
		}
	}
}

func (s *EngineService) setDeletionExecuteTask(engineID uint, scanTaskID, executeTaskID string) bool {
	engine, err := s.repo.GetByID(engineID)
	if err != nil || engine.DeletionScanTaskID == nil || *engine.DeletionScanTaskID != scanTaskID {
		return false
	}
	engine.DeletionExecuteTaskID = &executeTaskID
	engine.DeletionError = ""
	return s.persistEngine(engine) == nil
}

func (s *EngineService) setDeletionError(engineID uint, scanTaskID, message string) {
	engine, err := s.repo.GetByID(engineID)
	if err != nil || engine.DeletionScanTaskID == nil || *engine.DeletionScanTaskID != scanTaskID {
		return
	}
	engine.DeletionError = strings.TrimSpace(message)
	_ = s.persistEngine(engine)
}

func valueOrZero(value *uint) uint {
	if value == nil {
		return 0
	}
	return *value
}

func (s *EngineService) ResumeDeletingEngines() error {
	engines, err := s.repo.ListDeleting()
	if err != nil {
		return err
	}
	for i := range engines {
		engine := &engines[i]
		if engine.DeletionScanTaskID == nil || strings.TrimSpace(*engine.DeletionScanTaskID) == "" {
			continue
		}
		go s.continueDeletion(engine.ID, *engine.DeletionScanTaskID)
	}
	return nil
}

// ListInternal 内部服务调用的资源列表查询（不做租户权限检查）
func (s *EngineService) ListInternal(engineType string, tenantID uint) ([]models.Engine, error) {
	var engines []models.Engine
	var err error

	if tenantID > 0 {
		// 按租户过滤
		engines, _, err = s.repo.ListByTenant(tenantID, 0, 9999, engineType)
	} else {
		// 返回所有资源
		engines, _, err = s.repo.List(0, 9999, engineType)
	}

	if err != nil {
		return nil, err
	}

	// 解密所有资源的敏感字段
	for i := range engines {
		decryptedConnInfo, err := s.decryptStoredConnectionInfo(engines[i].EngineType, engines[i].ConnectionInfo)
		if err != nil {
			return nil, fmt.Errorf("解密资源 %d 连接信息失败: %w", engines[i].ID, err)
		}
		engines[i].ConnectionInfo = decryptedConnInfo
	}

	return engines, nil
}

// ListInternalWithCapability 按能力过滤资源（用于内部服务调用）
func (s *EngineService) ListInternalWithCapability(tenantID uint, filter engineselection.CapabilityFilter) ([]models.Engine, error) {
	// 1. 先获取所有资源（可以按租户过滤）
	allResources, err := s.ListInternal("", tenantID)
	if err != nil {
		return nil, err
	}

	// 2. 空过滤器返回所有资源
	if len(filter.StorageTypes) == 0 {
		return allResources, nil
	}

	// 3. 使用通用过滤器进行能力匹配
	return engineselection.FilterEnginesByCapability(allResources, filter), nil
}

// GetByIDInternal 内部服务直接访问资源详情（返回解密信息）
func (s *EngineService) GetByIDInternal(id uint) (*models.Engine, error) {
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	decryptedConnInfo, err := s.decryptStoredConnectionInfo(engine.EngineType, engine.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	engineCopy := *engine
	engineCopy.ConnectionInfo = decryptedConnInfo
	return &engineCopy, nil
}

// GetForConnection 返回带解密信息的资源，用于当前用户执行连接测试
func (s *EngineService) GetForConnection(id, tenantID uint) (*models.Engine, error) {
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(engine, tenantID); err != nil {
		return nil, err
	}
	if engine.LifecycleState == models.EngineLifecycleDeleting {
		return nil, ErrEngineDeleting
	}
	if engine.LifecycleState == models.EngineLifecycleDeleted {
		return nil, ErrEngineDeleted
	}

	decryptedConnInfo, err := s.decryptStoredConnectionInfo(engine.EngineType, engine.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	engineCopy := *engine
	engineCopy.ConnectionInfo = decryptedConnInfo
	return &engineCopy, nil
}

// GetForExecution returns decrypted connection information only for an active,
// currently online engine inside the already-authorized execution tenant boundary.
func (s *EngineService) GetForExecution(id, tenantID uint) (*models.Engine, error) {
	engine, err := s.GetForConnection(id, tenantID)
	if err != nil {
		return nil, err
	}
	if !engineselection.IsAvailable(engine) {
		return nil, ErrResourceForbidden
	}
	return engine, nil
}

// BuildConnectionTestEngine 返回带解密连接信息的资源副本，并用可选的当前表单配置覆盖。
// 用于编辑弹窗测试未保存的配置，同时复用已有资源的权限、类型与状态更新链路。
func (s *EngineService) BuildConnectionTestEngine(id, tenantID uint, override *models.ConnectionInfo) (*models.Engine, error) {
	engine, err := s.GetForConnection(id, tenantID)
	if err != nil {
		return nil, err
	}

	if override != nil {
		mergedConnInfo := s.mergePlainConnectionInfo(engine.EngineType, engine.ConnectionInfo, *override)
		engine.ConnectionInfo = s.stripConnectionInfoMetaFields(mergedConnInfo)
	}

	return engine, nil
}

// mergePlainConnectionInfo 合并两份明文连接信息，识别前端掩码占位并保留原始敏感值。
// 调用方必须先把数据库中的连接信息解密，避免在合并阶段混用密文和明文。
func (s *EngineService) mergePlainConnectionInfo(engineType string, original, updated models.ConnectionInfo) models.ConnectionInfo {
	merged := make(models.ConnectionInfo)

	// 先复制原始明文字段。
	for k, v := range original {
		if s.isConnectionInfoMetaField(k) {
			continue
		}
		merged[k] = v
	}

	// 再合并更新字段，对于敏感字段，需要判断是否仍为掩码占位
	for k, v := range updated {
		if s.isConnectionInfoMetaField(k) {
			continue
		}

		if !s.isSensitiveField(engineType, k) {
			merged[k] = v
			continue
		}

		strVal, ok := v.(string)
		if !ok {
			merged[k] = v
			continue
		}

		currentPlain, _ := merged[k].(string)
		if currentPlain != "" && s.isMaskedPlaceholder(strVal, currentPlain) {
			// 输入仍为掩码，占位符情况下保持原始明文
			continue
		}

		merged[k] = strVal
	}

	return merged
}

func (s *EngineService) validateConnectionIdentityUnchanged(engineType string, original, updated models.ConnectionInfo) error {
	fields, plugin, err := connectionIdentityDefinition(engineType)
	if err != nil {
		return err
	}
	changed := make([]string, 0)
	for _, field := range fields {
		before := normalizedConnectionIdentityValue(field, original, plugin)
		after := normalizedConnectionIdentityValue(field, updated, plugin)
		if before != after {
			changed = append(changed, field)
		}
	}
	if len(changed) > 0 {
		return fmt.Errorf("%w: %s", ErrEngineIdentityImmutable, strings.Join(changed, ", "))
	}
	return nil
}

func buildConnectionIdentityKey(engineType string, connInfo models.ConnectionInfo) (models.JSONString, error) {
	identityKey, err := engineplugin.BuildConnectionIdentityKey(engineType, engineplugin.ConnectionInfo(connInfo))
	if err != nil {
		return "", err
	}
	return models.JSONString(identityKey), nil
}

func connectionIdentityDefinition(engineType string) ([]string, engineplugin.EnginePlugin, error) {
	return engineplugin.ConnectionIdentityDefinition(engineType)
}

func normalizedConnectionIdentityValue(field string, connInfo models.ConnectionInfo, plugin engineplugin.EnginePlugin) string {
	return engineplugin.NormalizeConnectionIdentityValue(field, engineplugin.ConnectionInfo(connInfo), plugin)
}

func (s *EngineService) stripConnectionInfoMetaFields(connInfo models.ConnectionInfo) models.ConnectionInfo {
	cleaned := make(models.ConnectionInfo)
	for k, v := range connInfo {
		if s.isConnectionInfoMetaField(k) {
			continue
		}
		cleaned[k] = v
	}
	return cleaned
}

func (s *EngineService) isConnectionInfoMetaField(field string) bool {
	return strings.HasPrefix(field, "_")
}

// isMaskedPlaceholder 判断用户输入是否为掩码占位，而非真实敏感值
func (s *EngineService) isMaskedPlaceholder(value string, originalPlain string) bool {
	if value == "" || originalPlain == "" {
		return false
	}

	if value == "******" || value == "****" || isAllAsterisks(value) {
		return true
	}

	if strings.Count(value, "*") >= 4 {
		masked := maskWithAsterisks(originalPlain)
		if masked == value && masked != originalPlain {
			return true
		}
	}

	return false
}

func isAllAsterisks(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch != '*' {
			return false
		}
	}
	return true
}

func maskWithAsterisks(plain string) string {
	if plain == "" {
		return ""
	}
	if len(plain) <= 4 {
		return "****"
	}
	return plain[:4] + "****" + plain[len(plain)-4:]
}

// encryptConnectionInfoForStorage 加密即将持久化的明文连接信息中的敏感字段。
func (s *EngineService) encryptConnectionInfoForStorage(engineType string, connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
	encrypted := make(models.ConnectionInfo)
	for k, v := range connInfo {
		encrypted[k] = v
	}

	for field := range s.sensitiveFieldsForEngine(engineType) {
		if val, exists := connInfo[field]; exists {
			if strVal, ok := val.(string); ok && strVal != "" {
				encryptedVal, err := commonsecurity.Encrypt(strVal, s.encryptionKey)
				if err != nil {
					return nil, fmt.Errorf("加密字段 %s 失败: %w", field, err)
				}
				encrypted[field] = encryptedVal
			}
		}
	}

	return encrypted, nil
}

// decryptStoredConnectionInfo 解密从数据库读取的连接信息中的敏感字段。
func (s *EngineService) decryptStoredConnectionInfo(engineType string, connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
	decrypted := make(models.ConnectionInfo)
	for k, v := range connInfo {
		decrypted[k] = v
	}

	for field := range s.sensitiveFieldsForEngine(engineType) {
		if val, exists := connInfo[field]; exists {
			if strVal, ok := val.(string); ok && strVal != "" {
				decryptedVal, err := commonsecurity.Decrypt(strVal, s.encryptionKey)
				if err != nil {
					return nil, fmt.Errorf("解密字段 %s 失败: %w", field, err)
				}
				decrypted[field] = decryptedVal
			}
		}
	}

	return decrypted, nil
}

func (s *EngineService) maskSensitiveFields(engineType string, connInfo models.ConnectionInfo) models.ConnectionInfo {
	if connInfo == nil {
		return nil
	}

	masked := make(models.ConnectionInfo)
	for k, v := range connInfo {
		if s.isSensitiveField(engineType, k) && v != nil {
			masked[k] = "******"
			continue
		}
		masked[k] = v
	}
	return masked
}

func (s *EngineService) sanitizeResource(engine *models.Engine) *models.Engine {
	if engine == nil {
		return nil
	}

	copyResource := *engine
	copyResource.ConnectionInfo = s.maskSensitiveFields(engine.EngineType, engine.ConnectionInfo)
	return &copyResource
}

func (s *EngineService) authorizeResourceAccess(engine *models.Engine, tenantID uint) error {
	// 内置引擎对所有租户可见和可用
	if engine.IsBuiltin {
		return nil
	}

	// 检查用户和资源是否都有租户ID
	if tenantID == 0 || engine.TenantID == nil {
		return ErrResourceForbidden
	}

	// 比较租户ID
	if tenantID != *engine.TenantID {
		return ErrResourceForbidden
	}

	return nil
}

func (s *EngineService) sensitiveFieldsForEngine(engineType string) map[string]struct{} {
	provider, err := engineplugin.Get(strings.TrimSpace(engineType))
	if err != nil {
		return nil
	}

	fields := make(map[string]struct{}, len(provider.SensitiveFields()))
	for _, field := range provider.SensitiveFields() {
		field = strings.TrimSpace(field)
		if field != "" {
			fields[field] = struct{}{}
		}
	}
	return fields
}

func (s *EngineService) isSensitiveField(engineType, field string) bool {
	_, ok := s.sensitiveFieldsForEngine(engineType)[field]
	return ok
}

// validateCapabilities 验证引擎能力声明的有效性
func (s *EngineService) validateCapabilities(capabilitiesPtr *models.JSONString) error {
	_, err := s.parseCapabilities(capabilitiesPtr)
	return err
}

func (s *EngineService) validateCapabilitiesForEngine(engineType string, capabilitiesPtr *models.JSONString) error {
	structured, err := s.parseCapabilities(capabilitiesPtr)
	if err != nil {
		return err
	}
	engineTypeLower := strings.ToLower(strings.TrimSpace(engineType))
	if structured.EngineType != engineTypeLower {
		return fmt.Errorf("结构化能力声明 engine_type 必须为 %s", engineTypeLower)
	}
	return nil
}

func (s *EngineService) parseCapabilities(capabilitiesPtr *models.JSONString) (*engineplugin.EngineCapabilities, error) {
	if capabilitiesPtr == nil || *capabilitiesPtr == "" {
		return nil, nil // 空能力声明是允许的
	}

	structured, err := engineplugin.ParseEngineCapabilities(string(*capabilitiesPtr))
	if err != nil {
		return nil, fmt.Errorf("能力声明 JSON 格式错误: %w", err)
	}
	if structured == nil {
		return nil, fmt.Errorf("能力声明不能为空")
	}
	if structured.SchemaVersion != engineplugin.CapabilitiesSchemaVersion {
		return nil, fmt.Errorf("结构化能力声明 schema_version 必须为 %s", engineplugin.CapabilitiesSchemaVersion)
	}
	if structured.EngineType == "" {
		return nil, fmt.Errorf("结构化能力声明必须包含 engine_type")
	}

	return structured, nil
}

func toJSONStringPtr(value string) *models.JSONString {
	jsonValue := models.JSONString(value)
	return &jsonValue
}

// pluginCapabilities 返回内置插件声明的标准 capabilities。
func (s *EngineService) pluginCapabilities(engineType string) (string, error) {
	capabilities, err := dbbridge.GenerateCapabilities(engineType)
	if err == nil {
		return capabilities, nil
	}
	return "", err
}

func (s *EngineService) validateSystemEngineType(engineType string, capabilities *models.JSONString) error {
	engineTypeLower := strings.ToLower(strings.TrimSpace(engineType))
	if _, disallowed := disallowedSystemEngineTypes[engineTypeLower]; disallowed {
		return fmt.Errorf("%w: %s 是 Transfer 本地文件连接器，不能注册到 System 统一引擎表", ErrUnsupportedEngineType, engineTypeLower)
	}

	if capabilities != nil && *capabilities != "" {
		return s.validateCapabilitiesForEngine(engineTypeLower, capabilities)
	}

	generatedCapabilities, err := s.pluginCapabilities(engineTypeLower)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedEngineType, engineTypeLower)
	}
	if err := s.validateCapabilitiesForEngine(engineTypeLower, toJSONStringPtr(generatedCapabilities)); err != nil {
		return fmt.Errorf("引擎类型 %s 能力声明无效: %w", engineTypeLower, err)
	}
	return nil
}

func (s *EngineService) ValidateSystemEngineType(engineType string) error {
	return s.validateSystemEngineType(engineType, nil)
}

func (s *EngineService) ValidateSystemEngineRegistration(engineType string, capabilities *models.JSONString) error {
	return s.validateSystemEngineType(engineType, capabilities)
}

func (s *EngineService) shouldRefreshCapabilities(capabilities *models.JSONString) bool {
	if capabilities == nil || *capabilities == "" {
		return true
	}
	return s.validateCapabilities(capabilities) != nil
}

// CheckAndUpdateConnectionStatus 检测并更新资源连接状态（同步）
// 返回true表示在线，false表示离线
// 用于启动时的健康检查和用户手动测试连接
func (s *EngineService) CheckAndUpdateConnectionStatus(engineID uint) bool {
	return s.checkAndUpdateConnectionStatus(engineID, true)
}

func (s *EngineService) checkAndUpdateConnectionStatus(engineID uint, forceCapabilityRefresh bool) bool {
	fmt.Printf("[ConnectionCheck] 🔍 开始检查引擎连接状态: ID=%d\n", engineID)

	// 1. 获取资源
	engine, err := s.repo.GetByID(engineID)
	if err != nil {
		fmt.Printf("[ConnectionCheck] ❌ 获取资源失败: %v\n", err)
		s.updateConnectionStatus(engineID, "unknown", fmt.Sprintf("获取资源失败: %v", err))
		return false
	}
	fmt.Printf("[ConnectionCheck] ✅ 获取引擎信息: type=%s, name=%s\n", engine.EngineType, engine.Name)

	// 2. 跳过模块 API 类型资源（api.xxx）的连接检测。
	// 这类记录不属于外部数据/计算引擎，不能通过 EnginePlugin 做连接检测。
	if strings.HasPrefix(engine.EngineType, "api.") {
		fmt.Printf("[ConnectionCheck] ⏭️  跳过API类型资源\n")
		s.updateConnectionStatus(engineID, "unknown", "API类型资源不支持自动连接检测")
		return false
	}

	// 3. 解密连接信息
	decryptedConnInfo, err := s.decryptStoredConnectionInfo(engine.EngineType, engine.ConnectionInfo)
	if err != nil {
		fmt.Printf("[ConnectionCheck] ❌ 解密连接信息失败: %v\n", err)
		s.updateConnectionStatus(engineID, "unknown", fmt.Sprintf("解密连接信息失败: %v", err))
		return false
	}
	engine.ConnectionInfo = decryptedConnInfo
	fmt.Printf("[ConnectionCheck] 连接信息解密成功\n")

	// 4. 测试连接
	fmt.Printf("[ConnectionCheck] ⏱️  开始连接测试 (超时: 3秒)...\n")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = dbbridge.TestConnection(ctx, engine)

	// 5. 更新状态
	if err != nil {
		fmt.Printf("[ConnectionCheck] ❌ 连接测试失败: %v\n", err)
		s.updateConnectionStatus(engineID, "offline", err.Error())
		return false
	}

	fmt.Printf("[ConnectionCheck] ✅ 连接测试成功\n")
	checkMessage := "连接正常"
	// 周期探测只在离线/未知 -> 在线的边沿刷新能力，避免每轮健康检查
	// 都对同一引擎重复执行较重的能力探测。
	if forceCapabilityRefresh || engine.ConnectionStatus != models.EngineConnectionOnline {
		if err := s.refreshEngineCapabilities(engineID); err != nil {
			checkMessage = fmt.Sprintf("连接正常；能力刷新失败: %v", err)
			fmt.Printf("[ConnectionCheck] ⚠️  能力刷新失败，保留最后一次成功结果: %v\n", err)
		}
	}
	s.updateConnectionStatus(engineID, models.EngineConnectionOnline, checkMessage)
	return true
}

// refreshEngineCapabilities refreshes one Engine Instance after a successful connection probe.
// A failure is returned to the caller but never clears the last successfully persisted facts.
func (s *EngineService) refreshEngineCapabilities(engineID uint) error {
	if engineID == 0 {
		return errors.New("无效的引擎数据")
	}
	engine, err := s.repo.GetByID(engineID)
	if err != nil {
		return err
	}
	if !s.usesPluginCapabilities(engine.EngineType) && !s.shouldRefreshCapabilities(engine.Capabilities) {
		return nil
	}
	capabilities, err := s.resolveCapabilitiesForEngine(engine)
	if err != nil {
		return err
	}
	capabilitiesJSON := toJSONStringPtr(capabilities)
	if err := s.validateCapabilitiesForEngine(engine.EngineType, capabilitiesJSON); err != nil {
		return fmt.Errorf("能力声明验证失败: %w", err)
	}
	if engine.Capabilities != nil && string(*engine.Capabilities) == capabilities {
		return nil
	}
	engine.Capabilities = capabilitiesJSON
	return s.persistEngine(engine)
}

// AsyncCheckConnection 异步检测资源连接状态（用于被动触发）
// 立即返回，不阻塞调用方
// 用于其他模块在连接失败时通知System刷新状态
func (s *EngineService) AsyncCheckConnection(engineID uint) error {
	// 验证资源存在
	if _, err := s.repo.GetByID(engineID); err != nil {
		return fmt.Errorf("资源不存在: %w", err)
	}

	// 后台异步检测
	go func() {
		s.CheckAndUpdateConnectionStatus(engineID)
	}()

	return nil
}

// updateConnectionStatus 内部方法：更新连接状态
func (s *EngineService) updateConnectionStatus(engineID uint, status, message string) error {
	now := time.Now()
	if err := s.repo.UpdateConnectionObservation(engineID, status, now, message); err != nil {
		return err
	}

	if !strings.EqualFold(strings.TrimSpace(status), "online") {
		return nil
	}
	engine, err := s.repo.GetByID(engineID)
	if err != nil {
		return err
	}
	if _, err := s.workflowRuntimeProbeEngine(engine); err != nil {
		return nil
	}
	return s.reconcileSpatialWorkspaceRuntimeBindings(context.Background())
}

// reconcileSpatialWorkspaceRuntimeBindings 在 Workflow Runtime 可用后重算依赖它的空间工作区绑定。
// 它只基于已持久化的实例能力做协调，不重新连接所有存储引擎做实例能力探测。
func (s *EngineService) reconcileSpatialWorkspaceRuntimeBindings(ctx context.Context) error {
	if s.repo == nil {
		return nil
	}

	engines, err := s.repo.ListAll()
	if err != nil {
		return fmt.Errorf("列出引擎实例失败: %w", err)
	}
	for i := range engines {
		engine := &engines[i]
		if engine.LifecycleState != models.EngineLifecycleActive || engine.Capabilities == nil || strings.TrimSpace(string(*engine.Capabilities)) == "" {
			continue
		}

		current := string(*engine.Capabilities)
		enriched, err := s.enrichInstanceCapabilities(engine, current)
		if err != nil {
			return fmt.Errorf("协调引擎 %d 空间工作区绑定失败: %w", engine.ID, err)
		}
		if capabilitiesJSONEqual(current, enriched) {
			continue
		}

		capabilitiesJSON := toJSONStringPtr(enriched)
		if err := s.validateCapabilitiesForEngine(engine.EngineType, capabilitiesJSON); err != nil {
			return fmt.Errorf("协调引擎 %d 能力声明失败: %w", engine.ID, err)
		}
		engine.Capabilities = capabilitiesJSON
		if err := s.persistEngine(engine); err != nil {
			return fmt.Errorf("保存引擎 %d 空间工作区绑定失败: %w", engine.ID, err)
		}
		if s.eventPublisher != nil {
			_ = s.eventPublisher.PublishEngineChange(ctx, engine.ID, events.ActionUpdate)
		}
	}
	return nil
}

func capabilitiesJSONEqual(left, right string) bool {
	if left == right {
		return true
	}
	leftCapabilities, leftErr := engineplugin.ParseEngineCapabilities(left)
	rightCapabilities, rightErr := engineplugin.ParseEngineCapabilities(right)
	if leftErr != nil || rightErr != nil || leftCapabilities == nil || rightCapabilities == nil {
		return false
	}
	leftCanonical, leftErr := engineplugin.MarshalEngineCapabilities(*leftCapabilities)
	rightCanonical, rightErr := engineplugin.MarshalEngineCapabilities(*rightCapabilities)
	return leftErr == nil && rightErr == nil && leftCanonical == rightCanonical
}

// RecordConnectionStatus 记录 System 自身连接检测得到的资源连接状态。
func (s *EngineService) RecordConnectionStatus(engineID uint, status string, message string) error {
	return s.updateConnectionStatus(engineID, status, message)
}

// CreateEngine 创建引擎

func (s *EngineService) CreateEngine(engine *models.Engine) (*models.Engine, bool, error) {
	if engine == nil {
		return nil, false, errors.New("无效的引擎数据")
	}
	if strings.TrimSpace(engine.LifecycleState) == "" {
		engine.LifecycleState = models.EngineLifecycleActive
	}
	if strings.TrimSpace(engine.ExternalArtifactPolicy) == "" {
		engine.ExternalArtifactPolicy = models.ExternalArtifactPolicyDelete
	}
	identityKey, err := buildConnectionIdentityKey(engine.EngineType, engine.ConnectionInfo)
	if err != nil {
		return nil, false, err
	}
	if existing, findErr := s.repo.FindByIdentityKey(engine.EngineType, engine.TenantID, identityKey); findErr == nil {
		return s.resolveIdempotentRegistration(existing)
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, false, findErr
	}
	encryptedConnInfo, err := s.encryptConnectionInfoForStorage(engine.EngineType, engine.ConnectionInfo)
	if err != nil {
		return nil, false, err
	}
	engine.ConnectionInfo = encryptedConnInfo
	engine.IdentityKey = identityKey
	engine.Version = 1
	if err := s.prepareEngineCapabilities(engine); err != nil {
		return nil, false, err
	}
	if err := s.repo.Create(engine); err != nil {
		if existing, findErr := s.repo.FindByIdentityKey(engine.EngineType, engine.TenantID, identityKey); findErr == nil {
			return s.resolveIdempotentRegistration(existing)
		}
		return nil, false, err
	}
	return engine, true, nil
}

// UpdateEngine 更新引擎
func (s *EngineService) UpdateEngine(engine *models.Engine) error {
	if engine == nil || engine.ID == 0 {
		return errors.New("无效的引擎数据")
	}
	stored, err := s.repo.GetByID(engine.ID)
	if err != nil {
		return err
	}
	if stored.LifecycleState == models.EngineLifecycleDeleting {
		return ErrEngineDeleting
	}
	if stored.LifecycleState == models.EngineLifecycleDeleted {
		return ErrEngineDeleted
	}
	original, err := s.decryptStoredConnectionInfo(stored.EngineType, stored.ConnectionInfo)
	if err != nil {
		return fmt.Errorf("解密连接信息失败: %w", err)
	}
	updated := s.mergePlainConnectionInfo(stored.EngineType, original, engine.ConnectionInfo)
	if err := s.validateConnectionIdentityUnchanged(engine.EngineType, original, updated); err != nil {
		return err
	}
	encryptedConnInfo, err := s.encryptConnectionInfoForStorage(stored.EngineType, updated)
	if err != nil {
		return fmt.Errorf("加密连接信息失败: %w", err)
	}
	engine.ConnectionInfo = encryptedConnInfo
	if err := s.prepareEngineCapabilities(engine); err != nil {
		return err
	}
	return s.persistEngine(engine)
}

func (s *EngineService) prepareEngineCapabilities(engine *models.Engine) error {
	if engine == nil {
		return errors.New("无效的引擎数据")
	}
	capabilities, err := s.resolveCapabilitiesForEngine(engine)
	if err != nil {
		return err
	}
	engine.Capabilities = toJSONStringPtr(capabilities)
	if err := s.validateCapabilitiesForEngine(engine.EngineType, engine.Capabilities); err != nil {
		return fmt.Errorf("能力声明验证失败: %w", err)
	}
	return nil
}

func (s *EngineService) resolveCapabilitiesForEngine(engine *models.Engine) (string, error) {
	if engine == nil {
		return "", errors.New("无效的引擎数据")
	}
	engineTypeLower := strings.ToLower(strings.TrimSpace(engine.EngineType))
	if _, disallowed := disallowedSystemEngineTypes[engineTypeLower]; disallowed {
		return "", fmt.Errorf("%w: %s 是 Transfer 本地文件连接器，不能注册到 System 统一引擎表", ErrUnsupportedEngineType, engineTypeLower)
	}

	if s.usesPluginCapabilities(engineTypeLower) {
		probeEngine := *engine
		decryptedConnInfo, err := s.decryptStoredConnectionInfo(engine.EngineType, engine.ConnectionInfo)
		if err != nil {
			return "", fmt.Errorf("解密连接信息失败: %w", err)
		}
		probeEngine.EngineType = engineTypeLower
		probeEngine.ConnectionInfo = decryptedConnInfo
		capabilities, err := dbbridge.GenerateResolvedCapabilities(context.Background(), &probeEngine)
		if err != nil {
			return "", err
		}
		capabilities, err = s.enrichInstanceCapabilities(engine, capabilities)
		if err != nil {
			return "", err
		}
		return capabilities, nil
	}

	return s.ensureCapabilitiesForEngine(engineTypeLower, engine.Capabilities)
}

func (s *EngineService) ensureCapabilitiesForEngine(engineType string, submitted *models.JSONString) (string, error) {
	engineTypeLower := strings.ToLower(strings.TrimSpace(engineType))
	if _, disallowed := disallowedSystemEngineTypes[engineTypeLower]; disallowed {
		return "", fmt.Errorf("%w: %s 是 Transfer 本地文件连接器，不能注册到 System 统一引擎表", ErrUnsupportedEngineType, engineTypeLower)
	}

	if capabilities, err := s.pluginCapabilities(engineTypeLower); err == nil {
		return capabilities, nil
	}

	if submitted == nil || *submitted == "" {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedEngineType, engineTypeLower)
	}
	if err := s.validateCapabilitiesForEngine(engineTypeLower, submitted); err != nil {
		return "", fmt.Errorf("能力声明验证失败: %w", err)
	}
	return string(*submitted), nil
}

func (s *EngineService) enrichInstanceCapabilities(engine *models.Engine, capabilitiesJSON string) (string, error) {
	if s.repo == nil || engine == nil || strings.TrimSpace(capabilitiesJSON) == "" {
		return capabilitiesJSON, nil
	}

	caps, err := engineplugin.ParseEngineCapabilities(capabilitiesJSON)
	if err != nil || caps == nil || caps.Extensions == nil {
		return capabilitiesJSON, nil
	}

	workspaces, err := engineplugin.SpatialWorkspacesFromExtensions(caps.Extensions)
	if err != nil || len(workspaces) == 0 {
		return capabilitiesJSON, nil
	}

	changed := false
	for i := range workspaces {
		ws := &workspaces[i]
		if strings.ToLower(strings.TrimSpace(ws.Ecosystem)) != "supermap" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(ws.Kind))
		if kind != engineplugin.SpatialWorkspaceSuperMapSDXPostGIS &&
			kind != engineplugin.SpatialWorkspaceSuperMapSDXPostgreSQL {
			continue
		}
		ws.BoundRuntimeEngineID = nil
		if ws.State == engineplugin.SpatialWorkspaceStateDetected {
			ws.CanEnable = false
			if kind == engineplugin.SpatialWorkspaceSuperMapSDXPostgreSQL {
				if runtimeEngine, ok := s.firstAvailableDirectWorkflowRuntime(context.Background(), engine.TenantID, supermapworkflow.RequiredTableOperators()...); ok {
					ws.BoundRuntimeEngineID = &runtimeEngine.ID
					if caps.Storage != nil && caps.Storage.Store != nil {
						caps.Storage.Store.TableSpatialEncoding = &engineplugin.NativeTableSpatialEncodingCapability{
							GeometryReadEncodings:  []string{"ewkb"},
							GeometryWriteEncodings: []string{"ewkb"},
						}
					}
				}
			}
		} else {
			operatorName, ok := spatialWorkspaceEnableOperator(kind)
			if !ok {
				continue
			}
			runtimeEngine, hasRuntime := s.firstAvailableDirectWorkflowRuntime(context.Background(), engine.TenantID, operatorName)
			ws.CanEnable = ws.CanEnable && hasRuntime
			// SDX+ for PostGIS reuses PostgreSQL/PostGIS table access. Its
			// workflow runtime is only needed for the explicit enable action,
			// so do not persist a runtime binding. SDX+ for PostgreSQL owns a
			// private geometry encoding and must retain the exact runtime used
			// by Meta and Transfer table sessions.
			if hasRuntime && kind == engineplugin.SpatialWorkspaceSuperMapSDXPostgreSQL {
				ws.BoundRuntimeEngineID = &runtimeEngine.ID
			}
		}
		changed = true
	}

	if !changed {
		return capabilitiesJSON, nil
	}

	engineplugin.SetSpatialWorkspacesExtension(caps, workspaces)
	return engineplugin.MarshalEngineCapabilities(*caps)
}

func (s *EngineService) EnableSpatialWorkspace(ctx context.Context, id uint, ecosystem, kind string, tenantID uint) (*models.Engine, error) {
	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(engine, tenantID); err != nil {
		return nil, err
	}

	caps, err := s.parseCapabilities(engine.Capabilities)
	if err != nil || caps == nil {
		return nil, ErrSpatialWorkspaceNotFound
	}

	workspaces, err := engineplugin.SpatialWorkspacesFromExtensions(caps.Extensions)
	if err != nil || len(workspaces) == 0 {
		return nil, ErrSpatialWorkspaceNotFound
	}

	targetIndex := -1
	for i := range workspaces {
		ws := workspaces[i]
		if !spatialWorkspaceMatches(ws, ecosystem, kind) {
			continue
		}
		targetIndex = i
		break
	}
	if targetIndex == -1 {
		return nil, ErrSpatialWorkspaceNotFound
	}

	target := workspaces[targetIndex]
	if !target.CanEnable || strings.EqualFold(strings.TrimSpace(target.State), engineplugin.SpatialWorkspaceStateDetected) {
		return nil, errors.New("当前空间工作区暂不可启用")
	}
	operatorName, ok := spatialWorkspaceEnableOperator(target.Kind)
	if !ok {
		return nil, ErrSpatialWorkspaceNotFound
	}

	var runtimeEngine *models.Engine
	if target.BoundRuntimeEngineID != nil && *target.BoundRuntimeEngineID > 0 {
		runtimeEngine, err = s.workflowRuntimeByIDForTenant(*target.BoundRuntimeEngineID, tenantID)
		if err != nil {
			return nil, err
		}
	} else if discovered, found := s.firstAvailableDirectWorkflowRuntime(ctx, engine.TenantID, operatorName); found {
		runtimeEngine = discovered
	} else {
		return nil, fmt.Errorf("没有提供 direct 算子 %s 的可用工作流运行时", operatorName)
	}

	decryptedConnInfo, err := s.decryptStoredConnectionInfo(engine.EngineType, engine.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	invokeResult, err := dbbridge.InvokeOperator(ctx, runtimeEngine, operatorName, engineplugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"connection_info": decryptedConnInfo,
			"alias":           engine.Name,
		},
	})
	if err != nil {
		return nil, err
	}
	if invokeResult != nil && invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, fmt.Errorf("SuperMap 空间工作区启用失败: %s", invokeResult.Error)
	}

	if err := s.prepareEngineCapabilities(engine); err != nil {
		return nil, err
	}
	if err := s.persistEngine(engine); err != nil {
		return nil, err
	}

	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(ctx, engine.ID, events.ActionUpdate)
	}

	return s.sanitizeResource(engine), nil
}

func spatialWorkspaceMatches(workspace engineplugin.SpatialWorkspaceFact, ecosystem, kind string) bool {
	return strings.EqualFold(strings.TrimSpace(workspace.Ecosystem), ecosystem) &&
		strings.EqualFold(strings.TrimSpace(workspace.Kind), kind)
}

func spatialWorkspaceEnableOperator(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case engineplugin.SpatialWorkspaceSuperMapSDXPostGIS:
		return "datasource.enable_postgis", true
	case engineplugin.SpatialWorkspaceSuperMapSDXPostgreSQL:
		return "datasource.enable_postgresql", true
	default:
		return "", false
	}
}

func (s *EngineService) firstAvailableDirectWorkflowRuntime(ctx context.Context, tenantID *uint, operatorNames ...string) (*models.Engine, bool) {
	if s.repo == nil {
		return nil, false
	}

	var (
		engines []models.Engine
		err     error
	)
	if tenantID != nil {
		engines, _, err = s.repo.ListByTenant(*tenantID, 0, 1000, "")
	} else {
		engines, _, err = s.repo.List(0, 1000, "")
	}
	if err != nil || len(engines) == 0 {
		return nil, false
	}
	sort.SliceStable(engines, func(i, j int) bool {
		if engines[i].IsBuiltin != engines[j].IsBuiltin {
			return !engines[i].IsBuiltin
		}
		return engines[i].ID < engines[j].ID
	})
	for i := range engines {
		candidate, err := s.workflowRuntimeProbeEngine(&engines[i])
		if err != nil {
			continue
		}
		if err := dbbridge.RequireDirectWorkflowOperators(ctx, candidate, operatorNames...); err == nil {
			return candidate, true
		}
	}
	return nil, false
}

func (s *EngineService) workflowRuntimeByIDForTenant(id, tenantID uint) (*models.Engine, error) {
	runtimeEngine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("绑定的工作流运行时不存在")
		}
		return nil, err
	}
	if runtimeEngine.LifecycleState != models.EngineLifecycleActive {
		return nil, errors.New("绑定的工作流运行时未启用")
	}
	if err := s.authorizeResourceAccess(runtimeEngine, tenantID); err != nil {
		return nil, errors.New("绑定的工作流运行时对当前租户不可见")
	}
	probeEngine, err := s.workflowRuntimeProbeEngine(runtimeEngine)
	if err != nil {
		return nil, fmt.Errorf("绑定的引擎不是有效的 addp.workflow/v1 运行时: %w", err)
	}
	return probeEngine, nil
}

func (s *EngineService) workflowRuntimeProbeEngine(engine *models.Engine) (*models.Engine, error) {
	if engine == nil || engine.Capabilities == nil || strings.TrimSpace(string(*engine.Capabilities)) == "" {
		return nil, errors.New("缺少工作流能力声明")
	}
	capabilities, err := engineplugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil {
		return nil, err
	}
	if capabilities.Compute == nil || capabilities.Compute.Workflow == nil ||
		!capabilities.Compute.Workflow.Supported ||
		capabilities.Compute.Workflow.RuntimeAPI != engineplugin.WorkflowRuntimeAPIAddpV1 {
		return nil, errors.New("未声明 addp.workflow/v1 工作流能力")
	}
	probeEngine := *engine
	probeEngine.ConnectionInfo, err = s.decryptStoredConnectionInfo(engine.EngineType, engine.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密运行时连接信息失败: %w", err)
	}
	if _, err := dbbridge.WorkflowRuntimeProviderForEngine(&probeEngine); err != nil {
		return nil, err
	}
	return &probeEngine, nil
}

func (s *EngineService) usesPluginCapabilities(engineType string) bool {
	_, err := s.pluginCapabilities(strings.ToLower(strings.TrimSpace(engineType)))
	return err == nil
}
