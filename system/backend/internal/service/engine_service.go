package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/addp/common/dbbridge"
	engineplugin "github.com/addp/common/engine/plugin"
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

type EngineService struct {
	repo           *repository.EngineRepository
	userRepo       *repository.UserRepository
	encryptionKey  []byte
	eventPublisher *events.EngineEventPublisher
}

func NewEngineService(repo *repository.EngineRepository, userRepo *repository.UserRepository, encryptionKey []byte, redisClient *redis.Client) *EngineService {
	return &EngineService{
		repo:           repo,
		userRepo:       userRepo,
		encryptionKey:  encryptionKey,
		eventPublisher: events.NewEngineEventPublisher(redisClient, nil),
	}
}

func (s *EngineService) Create(req *models.EngineCreateRequest, createdBy uint) (*models.Engine, error) {
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

	engine := &models.Engine{
		Name:           req.Name, // 显示名称
		EngineType:     req.EngineType,
		ConnectionInfo: encryptedConnInfo,
		Description:    req.Description,
		ScanConfig:     req.ScanConfig, // 保存扫描配置
		CreatedBy:      &createdBy,
		TenantID:       user.TenantID, // 继承用户的租户ID
		IsActive:       true,
	}

	// 设置引擎分类（如果提供）
	if req.EngineCategory != "" {
		engine.EngineCategory = req.EngineCategory
	}

	// 保存能力声明（如果提供）
	if req.Capabilities != nil {
		engine.Capabilities = req.Capabilities
	}

	// 自动生成 capabilities（如果请求中未提供）
	if engine.Capabilities == nil || *engine.Capabilities == "" {
		capabilities := s.generateDefaultCapabilities(req.EngineType)
		engine.Capabilities = toJSONStringPtr(capabilities)
	}

	// 验证能力声明
	if err := s.validateCapabilities(engine.Capabilities); err != nil {
		return nil, fmt.Errorf("能力声明验证失败: %w", err)
	}

	if err := s.repo.Create(engine); err != nil {
		return nil, err
	}

	// 发布资源创建事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(context.Background(), engine.ID, events.ActionCreate)
	}

	return s.sanitizeResource(engine), nil
}

// CreateInternal 供内部服务调用创建资源
func (s *EngineService) CreateInternal(req *models.EngineCreateRequest, tenantID uint, createdBy *uint) (*models.Engine, error) {
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

	engine := &models.Engine{
		Name:           req.Name, // 显示名称
		EngineType:     req.EngineType,
		ConnectionInfo: encryptedConnInfo,
		Description:    req.Description,
		ScanConfig:     req.ScanConfig, // 保存扫描配置
		TenantID:       tenantPtr,
		IsActive:       true,
		CreatedBy:      createdBy,
	}

	if req.Capabilities != nil {
		engine.Capabilities = req.Capabilities
	}

	// 自动生成 capabilities（如果请求中未提供）
	if engine.Capabilities == nil || *engine.Capabilities == "" {
		capabilities := s.generateDefaultCapabilities(req.EngineType)
		engine.Capabilities = toJSONStringPtr(capabilities)
	}

	if err := s.validateCapabilities(engine.Capabilities); err != nil {
		return nil, fmt.Errorf("能力声明验证失败: %w", err)
	}

	if err := s.repo.Create(engine); err != nil {
		return nil, err
	}

	// 发布资源创建事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(context.Background(), engine.ID, events.ActionCreate)
	}

	return s.sanitizeResource(engine), nil
}

func (s *EngineService) GetByID(id uint, currentUserID uint) (*models.Engine, error) {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(engine, currentUser); err != nil {
		return nil, err
	}

	return s.sanitizeResource(engine), nil
}

func (s *EngineService) List(page, pageSize int, engineType string, currentUserID uint) ([]models.Engine, int64, error) {
	offset := (page - 1) * pageSize

	// 获取当前用户信息
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, 0, err
	}

	var engines []models.Engine
	var total int64

	// SuperAdmin可以查看所有资源
	if currentUser.UserType == models.UserTypeSuperAdmin {
		engines, total, err = s.repo.List(offset, pageSize, engineType)
	} else {
		if currentUser.TenantID == nil {
			return nil, 0, errors.New("当前用户未关联租户，无法访问资源")
		}
		engines, total, err = s.repo.ListByTenant(*currentUser.TenantID, offset, pageSize, engineType)
	}

	if err != nil {
		return nil, 0, err
	}

	// 脱敏敏感字段
	sanitized := make([]models.Engine, 0, len(engines))
	for i := range engines {
		sanitized = append(sanitized, *s.sanitizeResource(&engines[i]))
	}

	return sanitized, total, nil
}

func (s *EngineService) Update(id uint, req *models.EngineUpdateRequest, currentUserID uint) (*models.Engine, error) {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(engine, currentUser); err != nil {
		return nil, err
	}

	if err := s.ensureResourceManagementPermission(currentUser); err != nil {
		return nil, err
	}

	// 检查是否为内置资源（内置资源不允许修改核心配置）
	if engine.IsBuiltin {
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
		engine.Name = *req.Name
	}
	if req.ConnectionInfo != nil {
		// 合并连接信息：如果新值是脱敏占位符，保留原值
		mergedConnInfo := s.mergeConnectionInfo(engine.ConnectionInfo, *req.ConnectionInfo)

		// 加密敏感字段
		encryptedConnInfo, err := s.encryptSensitiveFields(mergedConnInfo)
		if err != nil {
			return nil, fmt.Errorf("加密连接信息失败: %w", err)
		}
		engine.ConnectionInfo = encryptedConnInfo
	}
	if req.Description != nil {
		engine.Description = *req.Description
	}
	if req.IsActive != nil {
		engine.IsActive = *req.IsActive
	}
	if req.ScanConfig != nil {
		engine.ScanConfig = req.ScanConfig
	}
	if req.Capabilities != nil {
		engine.Capabilities = req.Capabilities
	}
	if err := s.validateCapabilities(engine.Capabilities); err != nil {
		return nil, fmt.Errorf("能力声明验证失败: %w", err)
	}

	if err := s.repo.Update(engine); err != nil {
		return nil, err
	}

	// 发布资源更新事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(context.Background(), engine.ID, events.ActionUpdate)
	}

	return s.sanitizeResource(engine), nil
}

func (s *EngineService) Delete(id uint, currentUserID uint) error {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return err
	}

	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrResourceNotFound
		}
		return err
	}

	if err := s.authorizeResourceAccess(engine, currentUser); err != nil {
		return err
	}

	if err := s.ensureResourceManagementPermission(currentUser); err != nil {
		return err
	}

	// 检查是否为内置资源
	if engine.IsBuiltin {
		return ErrBuiltinResourceImmutable
	}

	if err := s.repo.Delete(id); err != nil {
		return err
	}

	// 发布资源删除事件
	if s.eventPublisher != nil {
		_ = s.eventPublisher.PublishEngineChange(context.Background(), id, events.ActionDelete)
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
		decryptedConnInfo, err := s.decryptSensitiveFields(engines[i].ConnectionInfo)
		if err != nil {
			return nil, fmt.Errorf("解密资源 %d 连接信息失败: %w", engines[i].ID, err)
		}
		engines[i].ConnectionInfo = decryptedConnInfo
	}

	return engines, nil
}

// ListInternalWithCapability 按能力过滤资源（用于内部服务调用）
func (s *EngineService) ListInternalWithCapability(tenantID uint, filter commonutils.CapabilityFilter) ([]models.Engine, error) {
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
	return commonutils.FilterEnginesByCapability(allResources, filter), nil
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

	decryptedConnInfo, err := s.decryptSensitiveFields(engine.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	engineCopy := *engine
	engineCopy.ConnectionInfo = decryptedConnInfo
	return &engineCopy, nil
}

// GetForConnection 返回带解密信息的资源，用于当前用户执行连接测试
func (s *EngineService) GetForConnection(id uint, currentUserID uint) (*models.Engine, error) {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	engine, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(engine, currentUser); err != nil {
		return nil, err
	}

	if err := s.ensureResourceManagementPermission(currentUser); err != nil {
		return nil, err
	}

	decryptedConnInfo, err := s.decryptSensitiveFields(engine.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	engineCopy := *engine
	engineCopy.ConnectionInfo = decryptedConnInfo
	return &engineCopy, nil
}

// BuildConnectionTestEngine 返回带解密连接信息的资源副本，并用可选的当前表单配置覆盖。
// 用于编辑弹窗测试未保存的配置，同时复用已有资源的权限、类型与状态更新链路。
func (s *EngineService) BuildConnectionTestEngine(id uint, currentUserID uint, override *models.ConnectionInfo) (*models.Engine, error) {
	engine, err := s.GetForConnection(id, currentUserID)
	if err != nil {
		return nil, err
	}

	if override != nil {
		mergedConnInfo := s.mergeConnectionInfo(engine.ConnectionInfo, *override)
		engine.ConnectionInfo = s.stripConnectionInfoMetaFields(mergedConnInfo)
	}

	return engine, nil
}

// mergeConnectionInfo 合并连接信息，识别前端掩码占位并保留原始敏感值
func (s *EngineService) mergeConnectionInfo(original, updated models.ConnectionInfo) models.ConnectionInfo {
	merged := make(models.ConnectionInfo)

	// 先复制原始字段，并对敏感字段做解密，得到可操作的明文值
	for k, v := range original {
		if s.isConnectionInfoMetaField(k) {
			continue
		}

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
		if s.isConnectionInfoMetaField(k) {
			continue
		}

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

// encryptSensitiveFields 加密连接信息中的敏感字段
func (s *EngineService) encryptSensitiveFields(connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
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
func (s *EngineService) decryptSensitiveFields(connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
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

func (s *EngineService) maskSensitiveFields(connInfo models.ConnectionInfo) models.ConnectionInfo {
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

func (s *EngineService) sanitizeResource(engine *models.Engine) *models.Engine {
	if engine == nil {
		return nil
	}

	copyResource := *engine
	copyResource.ConnectionInfo = s.maskSensitiveFields(engine.ConnectionInfo)
	return &copyResource
}

func (s *EngineService) getCurrentUser(userID uint) (*models.User, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}
	return user, nil
}

func (s *EngineService) authorizeResourceAccess(engine *models.Engine, user *models.User) error {
	if user.UserType == models.UserTypeSuperAdmin {
		return nil
	}

	// 内置引擎对所有租户可见和可用
	if engine.IsBuiltin {
		return nil
	}

	// 检查用户和资源是否都有租户ID
	if user.TenantID == nil || engine.TenantID == nil {
		return ErrResourceForbidden
	}

	// 比较租户ID
	if *user.TenantID != *engine.TenantID {
		return ErrResourceForbidden
	}

	return nil
}

func (s *EngineService) isSensitiveField(field string) bool {
	switch field {
	case "password", "access_key", "secret_key", "token", "api_key":
		return true
	default:
		return false
	}
}

func (s *EngineService) ensureResourceManagementPermission(user *models.User) error {
	if user.UserType == models.UserTypeSuperAdmin || user.UserType == models.UserTypeTenantAdmin {
		return nil
	}
	return ErrResourceForbidden
}

// validateScanConfig 验证扫描配置的有效性
func (s *EngineService) validateScanConfig(config *models.ScanConfig) error {
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

// validateCapabilities 验证引擎能力声明的有效性
func (s *EngineService) validateCapabilities(capabilitiesPtr *models.JSONString) error {
	if capabilitiesPtr == nil || *capabilitiesPtr == "" {
		return nil // 空能力声明是允许的
	}

	structured, err := engineplugin.ParseEngineCapabilities(string(*capabilitiesPtr))
	if err != nil {
		return fmt.Errorf("能力声明 JSON 格式错误: %w", err)
	}
	if structured == nil {
		return fmt.Errorf("能力声明不能为空")
	}
	if structured.SchemaVersion == "" {
		return fmt.Errorf("结构化能力声明必须包含 schema_version")
	}
	if structured.EngineType == "" {
		return fmt.Errorf("结构化能力声明必须包含 engine_type")
	}

	return nil
}

func toJSONStringPtr(value string) *models.JSONString {
	jsonValue := models.JSONString(value)
	return &jsonValue
}

// generateDefaultCapabilities 根据资源类型生成默认 capabilities
func (s *EngineService) generateDefaultCapabilities(engineType string) string {
	// 尝试从插件系统获取能力描述
	capabilities, err := dbbridge.GenerateCapabilities(engineType)
	if err == nil {
		return capabilities
	}

	// 降级：对于API引擎等非数据库类型，使用硬编码
	engineTypeLower := strings.ToLower(engineType)
	var caps engineplugin.EngineCapabilities
	switch engineTypeLower {
	case "python_workflow":
		caps = engineplugin.NewWorkflowCapabilities(engineTypeLower, "addp.workflow/v1")

	case "spark_workflow":
		caps = engineplugin.NewWorkflowCapabilities(engineTypeLower, "addp.workflow/v1")

	default:
		caps = engineplugin.EngineCapabilities{
			SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
			EngineType:    engineTypeLower,
			EngineFamily:  "unknown",
		}
	}

	capabilitiesJSON, err := engineplugin.MarshalEngineCapabilities(caps)
	if err != nil {
		return "{}"
	}
	return capabilitiesJSON
}

// RefreshAllEngineCapabilities 将已注册引擎的能力声明统一刷新为当前插件体系结构。
// ADDP 当前不保留旧 capabilities 结构；未知插件类型保留为 unknown 族的新结构。
func (s *EngineService) RefreshAllEngineCapabilities() error {
	engines, err := s.repo.ListAll()
	if err != nil {
		return err
	}

	for i := range engines {
		engine := engines[i]
		capabilities := s.generateDefaultCapabilities(engine.EngineType)
		capabilitiesJSON := toJSONStringPtr(capabilities)
		if err := s.validateCapabilities(capabilitiesJSON); err != nil {
			return fmt.Errorf("生成引擎 %d(%s) 能力声明失败: %w", engine.ID, engine.EngineType, err)
		}
		if engine.Capabilities != nil && string(*engine.Capabilities) == capabilities {
			continue
		}
		engine.Capabilities = capabilitiesJSON
		if err := s.repo.Update(&engine); err != nil {
			return fmt.Errorf("刷新引擎 %d(%s) 能力声明失败: %w", engine.ID, engine.EngineType, err)
		}
	}

	return nil
}

// checkDuplicateResource 检查是否存在重复资源
func (s *EngineService) checkDuplicateResource(req *models.EngineCreateRequest, tenantID uint) error {
	// 检查 1: 同名资源
	existing, err := s.repo.FindByNameAndTenant(req.Name, tenantID)
	if err == nil && existing != nil {
		return fmt.Errorf("资源名称 '%s' 已存在，请使用其他名称", req.Name)
	}

	// 检查 2: 针对 PostgreSQL，检查连接信息（host+port+database）
	if strings.ToLower(req.EngineType) == "postgresql" || strings.ToLower(req.EngineType) == "postgres" {
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

	// 检查 3: 针对 S3/MinIO，检查连接信息（endpoint）
	if strings.ToLower(req.EngineType) == "s3" || strings.ToLower(req.EngineType) == "minio" {
		endpoint, _ := req.ConnectionInfo["endpoint"].(string)
		if endpoint != "" {
			// 查找同一租户下是否已有相同endpoint的S3引擎
			engines, _, err := s.repo.ListByTenant(tenantID, 0, 1000, "s3")
			if err == nil {
				for _, engine := range engines {
					if existingEndpoint, ok := engine.ConnectionInfo["endpoint"].(string); ok {
						if existingEndpoint == endpoint {
							return fmt.Errorf("对象存储连接 %s 已注册（资源名称: %s），不能重复注册",
								endpoint, engine.Name)
						}
					}
				}
			}
		}
	}

	return nil
}

// CheckAndUpdateConnectionStatus 检测并更新资源连接状态（同步）
// 返回true表示在线，false表示离线
// 用于启动时的健康检查和用户手动测试连接
func (s *EngineService) CheckAndUpdateConnectionStatus(engineID uint) bool {
	fmt.Printf("[ConnectionCheck] 🔍 开始检查引擎连接状态: ID=%d\n", engineID)

	// 1. 获取资源
	engine, err := s.repo.GetByID(engineID)
	if err != nil {
		fmt.Printf("[ConnectionCheck] ❌ 获取资源失败: %v\n", err)
		s.updateConnectionStatus(engineID, "unknown", fmt.Sprintf("获取资源失败: %v", err))
		return false
	}
	fmt.Printf("[ConnectionCheck] ✅ 获取引擎信息: type=%s, name=%s\n", engine.EngineType, engine.Name)

	// 2. 跳过API类型资源（api.xxx）的连接检测
	// API类型资源需要通过health_check_config配置的健康检查端点来检测
	if strings.HasPrefix(engine.EngineType, "api.") {
		fmt.Printf("[ConnectionCheck] ⏭️  跳过API类型资源\n")
		s.updateConnectionStatus(engineID, "unknown", "API类型资源不支持自动连接检测")
		return false
	}

	// 3. 解密连接信息
	decryptedConnInfo, err := s.decryptSensitiveFields(engine.ConnectionInfo)
	if err != nil {
		fmt.Printf("[ConnectionCheck] ❌ 解密连接信息失败: %v\n", err)
		s.updateConnectionStatus(engineID, "unknown", fmt.Sprintf("解密连接信息失败: %v", err))
		return false
	}
	engine.ConnectionInfo = decryptedConnInfo
	fmt.Printf("[ConnectionCheck] 🔓 连接信息解密成功: %+v\n", engine.ConnectionInfo)

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
	s.updateConnectionStatus(engineID, "online", "连接正常")
	return true
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
	// 获取资源
	engine, err := s.repo.GetByID(engineID)
	if err != nil {
		return err
	}

	// 更新状态字段
	now := time.Now()
	engine.ConnectionStatus = status
	engine.LastCheckAt = &now
	engine.CheckMessage = message

	// 保存更新
	return s.repo.Update(engine)
}

// UpdateConnectionStatus 更新资源连接状态（用于缓存优化）
// 由Meta模块在后台检测后调用，更新资源的连接状态缓存
// 注意：此方法已废弃，建议使用AsyncCheckConnection触发System自己检测
// 保留是为了向后兼容
func (s *EngineService) UpdateConnectionStatus(engineID uint, status string, message string) error {
	return s.updateConnectionStatus(engineID, status, message)
}

// GetByEngineTypeAndTenant 根据 engine_type 和 tenant_id 查询引擎
// 用于工作流引擎自注册时查找是否已存在记录
func (s *EngineService) GetByEngineTypeAndTenant(engineType string, tenantID *uint) (*models.Engine, error) {
	return s.repo.GetByEngineTypeAndTenant(engineType, tenantID)
}

// CreateEngine 创建引擎
func (s *EngineService) CreateEngine(engine *models.Engine) error {
	if engine.Capabilities == nil || *engine.Capabilities == "" {
		capabilities := s.generateDefaultCapabilities(engine.EngineType)
		engine.Capabilities = toJSONStringPtr(capabilities)
	}
	if err := s.validateCapabilities(engine.Capabilities); err != nil {
		return fmt.Errorf("能力声明验证失败: %w", err)
	}
	return s.repo.Create(engine)
}

// UpdateEngine 更新引擎
func (s *EngineService) UpdateEngine(engine *models.Engine) error {
	if engine.Capabilities == nil || *engine.Capabilities == "" {
		capabilities := s.generateDefaultCapabilities(engine.EngineType)
		engine.Capabilities = toJSONStringPtr(capabilities)
	}
	if err := s.validateCapabilities(engine.Capabilities); err != nil {
		return fmt.Errorf("能力声明验证失败: %w", err)
	}
	return s.repo.Update(engine)
}
