package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/events"
	commonutils "github.com/addp/common/utils"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

var (
	ErrResourceNotFound         = errors.New("资源不存在")
	ErrResourceForbidden        = errors.New("没有权限访问该资源")
	ErrBuiltinResourceImmutable = errors.New("内置资源不可删除或修改")
)

type ResourceService struct {
	repo           *repository.ResourceRepository
	userRepo       *repository.UserRepository
	encryptionKey  []byte
	eventPublisher *events.ResourceEventPublisher
}

func NewResourceService(repo *repository.ResourceRepository, userRepo *repository.UserRepository, encryptionKey []byte, redisClient *redis.Client) *ResourceService {
	return &ResourceService{
		repo:           repo,
		userRepo:       userRepo,
		encryptionKey:  encryptionKey,
		eventPublisher: events.NewResourceEventPublisher(redisClient, nil),
	}
}

func (s *ResourceService) Create(req *models.ResourceCreateRequest, createdBy uint) (*models.Resource, error) {
	// 获取创建者信息以确定租户
	user, err := s.userRepo.GetByID(createdBy)
	if err != nil {
		return nil, errors.New("用户不存在")
	}

	if err := s.ensureResourceManagementPermission(user); err != nil {
		return nil, err
	}

	// 检查重复资源
	if err := s.checkDuplicateResource(req, *user.TenantID); err != nil {
		return nil, err
	}

	// 验证扫描配置
	if err := s.validateScanConfig(req.ScanConfig); err != nil {
		return nil, fmt.Errorf("扫描配置验证失败: %w", err)
	}

	// 加密敏感字段
	encryptedConnInfo, err := s.encryptSensitiveFields(req.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("加密连接信息失败: %w", err)
	}

	resource := &models.Resource{
		Name:           req.Name,
		DisplayName:    req.DisplayName, // 中文显示名称
		ResourceType:   req.ResourceType,
		ConnectionInfo: encryptedConnInfo,
		Description:    req.Description,
		ScanConfig:     req.ScanConfig, // 保存扫描配置
		CreatedBy:      &createdBy,
		TenantID:       user.TenantID, // 继承用户的租户ID
		IsActive:       true,
	}

	// 如果未提供 display_name，默认等于 name
	if resource.DisplayName == "" {
		resource.DisplayName = resource.Name
	}

	// 自动生成 capabilities（如果请求中未提供）
	if resource.Capabilities == nil || *resource.Capabilities == "" {
		capabilities := s.generateDefaultCapabilities(req.ResourceType)
		resource.Capabilities = &capabilities
	}

	if err := s.repo.Create(resource); err != nil {
		return nil, err
	}

	// 发布资源创建事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishResourceChange(context.Background(), resource.ID, events.ActionCreate)
	}

	return s.sanitizeResource(resource), nil
}

// CreateInternal 供内部服务调用创建资源
func (s *ResourceService) CreateInternal(req *models.ResourceCreateRequest, tenantID uint, createdBy *uint) (*models.Resource, error) {
	if req == nil {
		return nil, errors.New("无效的请求数据")
	}

	// 验证扫描配置
	if err := s.validateScanConfig(req.ScanConfig); err != nil {
		return nil, fmt.Errorf("扫描配置验证失败: %w", err)
	}

	encryptedConnInfo, err := s.encryptSensitiveFields(req.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("加密连接信息失败: %w", err)
	}

	var tenantPtr *uint
	if tenantID > 0 {
		tenantPtr = &tenantID
	}

	resource := &models.Resource{
		Name:           req.Name,
		DisplayName:    req.DisplayName, // 中文显示名称
		ResourceType:   req.ResourceType,
		ConnectionInfo: encryptedConnInfo,
		Description:    req.Description,
		ScanConfig:     req.ScanConfig, // 保存扫描配置
		TenantID:       tenantPtr,
		IsActive:       true,
		CreatedBy:      createdBy,
	}

	// 如果未提供 display_name，默认等于 name
	if resource.DisplayName == "" {
		resource.DisplayName = resource.Name
	}

	// 自动生成 capabilities（如果请求中未提供）
	if resource.Capabilities == nil || *resource.Capabilities == "" {
		capabilities := s.generateDefaultCapabilities(req.ResourceType)
		resource.Capabilities = &capabilities
	}

	if err := s.repo.Create(resource); err != nil {
		return nil, err
	}

	// 发布资源创建事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishResourceChange(context.Background(), resource.ID, events.ActionCreate)
	}

	return s.sanitizeResource(resource), nil
}

func (s *ResourceService) GetByID(id uint, currentUserID uint) (*models.Resource, error) {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(resource, currentUser); err != nil {
		return nil, err
	}

	return s.sanitizeResource(resource), nil
}

func (s *ResourceService) List(page, pageSize int, resourceType string, currentUserID uint) ([]models.Resource, error) {
	offset := (page - 1) * pageSize

	// 获取当前用户信息
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	var resources []models.Resource

	// SuperAdmin可以查看所有资源
	if currentUser.UserType == models.UserTypeSuperAdmin {
		resources, err = s.repo.List(offset, pageSize, resourceType)
	} else {
		if currentUser.TenantID == nil {
			return nil, errors.New("当前用户未关联租户，无法访问资源")
		}
		resources, err = s.repo.ListByTenant(*currentUser.TenantID, offset, pageSize, resourceType)
	}

	if err != nil {
		return nil, err
	}

	// 脱敏敏感字段
	sanitized := make([]models.Resource, 0, len(resources))
	for i := range resources {
		sanitized = append(sanitized, *s.sanitizeResource(&resources[i]))
	}

	return sanitized, nil
}

func (s *ResourceService) Update(id uint, req *models.ResourceUpdateRequest, currentUserID uint) (*models.Resource, error) {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(resource, currentUser); err != nil {
		return nil, err
	}

	if err := s.ensureResourceManagementPermission(currentUser); err != nil {
		return nil, err
	}

	// 检查是否为内置资源（内置资源不允许修改核心配置）
	if resource.IsBuiltin {
		// 允许修改描述和显示名称，但不允许修改连接信息、名称等核心配置
		if req.Name != nil || req.ConnectionInfo != nil {
			return nil, ErrBuiltinResourceImmutable
		}
	}

	// 验证扫描配置
	if req.ScanConfig != nil {
		if err := s.validateScanConfig(req.ScanConfig); err != nil {
			return nil, fmt.Errorf("扫描配置验证失败: %w", err)
		}
	}

	if req.Name != nil {
		resource.Name = *req.Name
	}
	if req.DisplayName != nil {
		resource.DisplayName = *req.DisplayName
	}
	if req.ConnectionInfo != nil {
		// 合并连接信息：如果新值是脱敏占位符，保留原值
		mergedConnInfo := s.mergeConnectionInfo(resource.ConnectionInfo, *req.ConnectionInfo)

		// 加密敏感字段
		encryptedConnInfo, err := s.encryptSensitiveFields(mergedConnInfo)
		if err != nil {
			return nil, fmt.Errorf("加密连接信息失败: %w", err)
		}
		resource.ConnectionInfo = encryptedConnInfo
	}
	if req.Description != nil {
		resource.Description = *req.Description
	}
	if req.IsActive != nil {
		resource.IsActive = *req.IsActive
	}
	if req.ScanConfig != nil {
		resource.ScanConfig = req.ScanConfig
	}

	if err := s.repo.Update(resource); err != nil {
		return nil, err
	}

	// 发布资源更新事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishResourceChange(context.Background(), resource.ID, events.ActionUpdate)
	}

	return s.sanitizeResource(resource), nil
}

func (s *ResourceService) Delete(id uint, currentUserID uint) error {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return err
	}

	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrResourceNotFound
		}
		return err
	}

	if err := s.authorizeResourceAccess(resource, currentUser); err != nil {
		return err
	}

	if err := s.ensureResourceManagementPermission(currentUser); err != nil {
		return err
	}

	// 检查是否为内置资源
	if resource.IsBuiltin {
		return ErrBuiltinResourceImmutable
	}

	if err := s.repo.Delete(id); err != nil {
		return err
	}

	// 发布资源删除事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishResourceChange(context.Background(), id, events.ActionDelete)
	}

	return nil
}

// ListInternal 内部服务调用的资源列表查询（不做租户权限检查）
func (s *ResourceService) ListInternal(resourceType string, tenantID uint) ([]models.Resource, error) {
	var resources []models.Resource
	var err error

	if tenantID > 0 {
		// 按租户过滤
		resources, err = s.repo.ListByTenant(tenantID, 0, 9999, resourceType)
	} else {
		// 返回所有资源
		resources, err = s.repo.List(0, 9999, resourceType)
	}

	if err != nil {
		return nil, err
	}

	// 解密所有资源的敏感字段
	for i := range resources {
		decryptedConnInfo, err := s.decryptSensitiveFields(resources[i].ConnectionInfo)
		if err != nil {
			return nil, fmt.Errorf("解密资源 %d 连接信息失败: %w", resources[i].ID, err)
		}
		resources[i].ConnectionInfo = decryptedConnInfo
	}

	return resources, nil
}

// ListInternalWithCapability 按能力过滤资源（用于内部服务调用）
func (s *ResourceService) ListInternalWithCapability(tenantID uint, filter commonutils.CapabilityFilter) ([]models.Resource, error) {
	// 1. 先获取所有资源（可以按租户过滤）
	allResources, err := s.ListInternal("", tenantID)
	if err != nil {
		return nil, err
	}

	// 2. 空过滤器返回所有资源
	if len(filter.StorageTypes) == 0 && len(filter.ComputeTypes) == 0 {
		return allResources, nil
	}

	// 3. 逐个资源进行能力匹配
	var filtered []models.Resource
	for _, resource := range allResources {
		if s.matchesCapabilityFilter(&resource, filter) {
			filtered = append(filtered, resource)
		}
	}

	return filtered, nil
}

// matchesCapabilityFilter 检查资源是否匹配能力过滤器
func (s *ResourceService) matchesCapabilityFilter(resource *models.Resource, filter commonutils.CapabilityFilter) bool {
	// 解析 capabilities
	cap, err := commonutils.ParseCapabilities(resource.Capabilities)
	if err != nil || cap == nil {
		return false
	}

	// 检查存储能力匹配
	matchesStorage := false
	if len(filter.StorageTypes) > 0 {
		for _, storage := range cap.Storage {
			for _, targetType := range filter.StorageTypes {
				if storage.Type == targetType {
					matchesStorage = true
					break
				}
			}
			if matchesStorage {
				break
			}
		}
	} else {
		matchesStorage = true // 空过滤器匹配所有
	}

	// 检查计算能力匹配
	matchesCompute := false
	if len(filter.ComputeTypes) > 0 {
		for _, compute := range cap.Compute {
			for _, targetType := range filter.ComputeTypes {
				if compute.Type == targetType {
					matchesCompute = true
					break
				}
			}
			if matchesCompute {
				break
			}
		}
	} else {
		matchesCompute = true // 空过滤器匹配所有
	}

	// 根据 RequireBoth 标志决定逻辑
	if filter.RequireBoth {
		// AND 逻辑：必须同时满足
		return matchesStorage && matchesCompute
	}

	// OR 逻辑：满足任一即可
	if len(filter.StorageTypes) > 0 && len(filter.ComputeTypes) > 0 {
		return matchesStorage || matchesCompute
	}

	// 只有一种过滤条件
	if len(filter.StorageTypes) > 0 {
		return matchesStorage
	}

	return matchesCompute
}

// GetByIDInternal 内部服务直接访问资源详情（返回解密信息）
func (s *ResourceService) GetByIDInternal(id uint) (*models.Resource, error) {
	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	decryptedConnInfo, err := s.decryptSensitiveFields(resource.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	resourceCopy := *resource
	resourceCopy.ConnectionInfo = decryptedConnInfo
	return &resourceCopy, nil
}

// GetForConnection 返回带解密信息的资源，用于当前用户执行连接测试
func (s *ResourceService) GetForConnection(id uint, currentUserID uint) (*models.Resource, error) {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(resource, currentUser); err != nil {
		return nil, err
	}

	if err := s.ensureResourceManagementPermission(currentUser); err != nil {
		return nil, err
	}

	decryptedConnInfo, err := s.decryptSensitiveFields(resource.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	resourceCopy := *resource
	resourceCopy.ConnectionInfo = decryptedConnInfo
	return &resourceCopy, nil
}

// mergeConnectionInfo 合并连接信息，识别前端掩码占位并保留原始敏感值
func (s *ResourceService) mergeConnectionInfo(original, updated models.ConnectionInfo) models.ConnectionInfo {
	merged := make(models.ConnectionInfo)

	// 先复制原始字段，并对敏感字段做解密，得到可操作的明文值
	for k, v := range original {
		if !s.isSensitiveField(k) {
			merged[k] = v
			continue
		}

		strVal, ok := v.(string)
		if !ok || strVal == "" {
			merged[k] = v
			continue
		}

		if decrypted, err := commonutils.Decrypt(strVal, s.encryptionKey); err == nil {
			merged[k] = decrypted
		} else {
			// 兼容旧数据（未加密或格式异常），保持原值
			merged[k] = strVal
		}
	}

	// 再合并更新字段，对于敏感字段，需要判断是否仍为掩码占位
	for k, v := range updated {
		if !s.isSensitiveField(k) {
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

// isMaskedPlaceholder 判断用户输入是否为掩码占位，而非真实敏感值
func (s *ResourceService) isMaskedPlaceholder(value string, originalPlain string) bool {
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

// encryptSensitiveFields 加密连接信息中的敏感字段
func (s *ResourceService) encryptSensitiveFields(connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
	encrypted := make(models.ConnectionInfo)
	for k, v := range connInfo {
		encrypted[k] = v
	}

	// 定义需要加密的敏感字段
	sensitiveFields := []string{"password", "access_key", "secret_key", "token", "api_key"}

	for _, field := range sensitiveFields {
		if val, exists := connInfo[field]; exists {
			if strVal, ok := val.(string); ok && strVal != "" {
				encryptedVal, err := commonutils.Encrypt(strVal, s.encryptionKey)
				if err != nil {
					return nil, fmt.Errorf("加密字段 %s 失败: %w", field, err)
				}
				encrypted[field] = encryptedVal
			}
		}
	}

	return encrypted, nil
}

// decryptSensitiveFields 解密连接信息中的敏感字段
func (s *ResourceService) decryptSensitiveFields(connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
	decrypted := make(models.ConnectionInfo)
	for k, v := range connInfo {
		decrypted[k] = v
	}

	// 定义需要解密的敏感字段
	sensitiveFields := []string{"password", "access_key", "secret_key", "token", "api_key"}

	for _, field := range sensitiveFields {
		if val, exists := connInfo[field]; exists {
			if strVal, ok := val.(string); ok && strVal != "" {
				log.Printf("🔐 [DECRYPT] 开始解密字段 '%s' | 密文长度: %d", field, len(strVal))
				decryptedVal, err := commonutils.Decrypt(strVal, s.encryptionKey)
				if err != nil {
					// 如果解密失败，可能是未加密的旧数据，保持原值
					// 在生产环境中应该记录日志
					log.Printf("❌ [DECRYPT] 解密字段 '%s' 失败: %v | 密文前30字符: %s...",
						field, err, strVal[:min(len(strVal), 30)])
					decrypted[field] = strVal
					continue
				}
				log.Printf("✅ [DECRYPT] 解密字段 '%s' 成功 | 明文长度: %d 字节", field, len(decryptedVal))
				decrypted[field] = decryptedVal
			}
		}
	}

	return decrypted, nil
}

func (s *ResourceService) maskSensitiveFields(connInfo models.ConnectionInfo) models.ConnectionInfo {
	if connInfo == nil {
		return nil
	}

	masked := make(models.ConnectionInfo)
	for k, v := range connInfo {
		if s.isSensitiveField(k) && v != nil {
			masked[k] = "******"
			continue
		}
		masked[k] = v
	}
	return masked
}

func (s *ResourceService) sanitizeResource(resource *models.Resource) *models.Resource {
	if resource == nil {
		return nil
	}

	copyResource := *resource
	copyResource.ConnectionInfo = s.maskSensitiveFields(resource.ConnectionInfo)
	return &copyResource
}

func (s *ResourceService) getCurrentUser(userID uint) (*models.User, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}
	return user, nil
}

func (s *ResourceService) authorizeResourceAccess(resource *models.Resource, user *models.User) error {
	if user.UserType == models.UserTypeSuperAdmin {
		return nil
	}

	// 检查用户和资源是否都有租户ID
	if user.TenantID == nil || resource.TenantID == nil {
		return ErrResourceForbidden
	}

	// 比较租户ID
	if *user.TenantID != *resource.TenantID {
		return ErrResourceForbidden
	}

	return nil
}

func (s *ResourceService) isSensitiveField(field string) bool {
	switch field {
	case "password", "access_key", "secret_key", "token", "api_key":
		return true
	default:
		return false
	}
}

func (s *ResourceService) ensureResourceManagementPermission(user *models.User) error {
	if user.UserType == models.UserTypeSuperAdmin || user.UserType == models.UserTypeTenantAdmin {
		return nil
	}
	return ErrResourceForbidden
}

// validateScanConfig 验证扫描配置的有效性
func (s *ResourceService) validateScanConfig(config *models.ScanConfig) error {
	if config == nil {
		return nil
	}

	// 验证调度类型（仅在启用定时扫描时验证）
	if config.ScheduledScan {
		validScheduleTypes := map[string]bool{
			"daily":   true,
			"weekly":  true,
			"monthly": true,
			"cron":    true,
		}
		if !validScheduleTypes[config.ScheduleType] {
			return fmt.Errorf("无效的调度类型: %s", config.ScheduleType)
		}

		// 验证 Cron 表达式
		if config.ScheduleType == "cron" {
			if config.CronExpression == "" {
				return errors.New("调度类型为 cron 时必须提供 cron_expression")
			}
			parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
			if _, err := parser.Parse(config.CronExpression); err != nil {
				return fmt.Errorf("无效的 Cron 表达式: %w", err)
			}
		}
	}

	// 验证立即扫描深度
	if config.ImmediateScan && config.ImmediateDepth != "" {
		if config.ImmediateDepth != "basic" && config.ImmediateDepth != "deep" {
			return fmt.Errorf("无效的立即扫描深度: %s (必须是 basic 或 deep)", config.ImmediateDepth)
		}
	}

	// 兼容旧版字段验证
	if config.ScanDepth != "" && config.ScanDepth != "shallow" && config.ScanDepth != "deep" && config.ScanDepth != "basic" {
		return fmt.Errorf("无效的扫描深度: %s (必须是 shallow, basic 或 deep)", config.ScanDepth)
	}

	return nil
}

// generateDefaultCapabilities 根据资源类型生成默认 capabilities
func (s *ResourceService) generateDefaultCapabilities(resourceType string) string {
	// 尝试从插件系统获取能力描述
	capabilities, err := dbbridge.GenerateCapabilities(resourceType)
	if err == nil {
		return capabilities
	}

	// 降级：对于API引擎等非数据库类型，使用硬编码
	resourceTypeLower := strings.ToLower(resourceType)
	switch resourceTypeLower {
	// API引擎 - 内置模块
	case "api.meta":
		return `{"compute":[{"type":"scan","dev_modes":["workflow","form"],"supported_sources":["postgresql","mysql","minio","s3"],"features":["basic","deep","scheduled"]}]}`

	case "api.transfer":
		return `{"compute":[{"type":"transfer.batch","dev_modes":["workflow","form"],"features":["incremental","scheduled","parallel"]}]}`

	case "api.manager":
		return `{"compute":[{"type":"tile_cache","dev_modes":["workflow","form"],"supported_formats":["mvt","pbf"],"features":["pre_cache","on_demand"]}]}`

	case "api.geopandas":
		return `{"compute":[{"type":"spatial","dev_modes":["workflow"],"supported_formats":["geojson","wkt","shapely"],"features":["dag","memory_efficient","batch"]}]}`

	case "api.spark_sedona":
		return `{"compute":[{"type":"spatial","dev_modes":["workflow"],"engine":"sedona","scale":"distributed","features":["big_data","distributed"]}]}`

	default:
		// 未知类型，生成通用 storage 能力
		return `{"storage":[{"type":"generic"}]}`
	}
}

// checkDuplicateResource 检查是否存在重复资源
func (s *ResourceService) checkDuplicateResource(req *models.ResourceCreateRequest, tenantID uint) error {
	// 检查 1: 同名资源
	existing, err := s.repo.FindByNameAndTenant(req.Name, tenantID)
	if err == nil && existing != nil {
		return fmt.Errorf("资源名称 '%s' 已存在，请使用其他名称", req.Name)
	}

	// 检查 2: 针对 PostgreSQL，检查连接信息（host+port+database）
	if strings.ToLower(req.ResourceType) == "postgresql" || strings.ToLower(req.ResourceType) == "postgres" {
		host, _ := req.ConnectionInfo["host"].(string)
		portFloat, _ := req.ConnectionInfo["port"].(float64)
		database, _ := req.ConnectionInfo["database"].(string)

		port := int(portFloat)

		existing, err := s.repo.FindByConnection(tenantID, host, port, database)
		if err == nil && existing != nil {
			return fmt.Errorf("数据库连接 %s:%d/%s 已注册（资源名称: %s），不能重复注册",
				host, port, database, existing.Name)
		}
	}

	return nil
}

