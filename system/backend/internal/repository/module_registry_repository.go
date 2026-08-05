package repository

import (
	"encoding/json"
	"errors"
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/system/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ModuleRegistryRepository 模块注册表数据访问层
type ModuleRegistryRepository struct {
	db *gorm.DB
}

// NewModuleRegistryRepository 创建模块注册表Repository
func NewModuleRegistryRepository(db *gorm.DB) *ModuleRegistryRepository {
	return &ModuleRegistryRepository{db: db}
}

// Register 注册或更新模块(幂等操作)
func (r *ModuleRegistryRepository) Register(req *models.ModuleRegistrationRequest) error {
	// 序列化metadata为JSON
	var metadata datatypes.JSON
	if req.Metadata != nil {
		data, err := json.Marshal(req.Metadata)
		if err != nil {
			return err
		}
		metadata = data
	}
	var configurationManagement datatypes.JSON
	if req.ConfigurationManagement != nil {
		data, err := json.Marshal(req.ConfigurationManagement)
		if err != nil {
			return err
		}
		configurationManagement = data
	}

	// 使用 UPSERT 逻辑：如果存在则更新，否则创建
	module := models.ModuleRegistry{
		ModuleName:              req.ModuleName,
		ModuleURL:               req.ModuleURL,
		RoutePrefix:             req.RoutePrefix,
		HealthCheckURL:          req.HealthCheckURL,
		Status:                  "up",
		LastHeartbeat:           time.Now(),
		Metadata:                metadata,
		ConfigurationManagement: configurationManagement,
	}

	// 按 module_name 查找现有记录
	var existing models.ModuleRegistry
	err := r.db.Where("module_name = ?", req.ModuleName).First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 创建新记录
		return r.db.Create(&module).Error
	} else if err != nil {
		return err
	}

	// 更新现有记录
	return r.db.Model(&existing).Updates(map[string]interface{}{
		"module_url":               req.ModuleURL,
		"route_prefix":             req.RoutePrefix,
		"health_check_url":         req.HealthCheckURL,
		"status":                   "up",
		"last_heartbeat":           time.Now(),
		"metadata":                 metadata,
		"configuration_management": configurationManagement,
	}).Error
}

// UpdateHeartbeat 更新模块心跳时间
func (r *ModuleRegistryRepository) UpdateHeartbeat(moduleName string) error {
	return r.db.Model(&models.ModuleRegistry{}).
		Where("module_name = ?", moduleName).
		Updates(map[string]interface{}{
			"last_heartbeat": time.Now(),
			"status":         "up",
		}).Error
}

// GetModule 获取单个模块信息
func (r *ModuleRegistryRepository) GetModule(moduleName string) (*models.ModuleRegistry, error) {
	var module models.ModuleRegistry
	err := r.db.Where("module_name = ?", moduleName).First(&module).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &module, nil
}

// ListModules 获取所有模块列表
func (r *ModuleRegistryRepository) ListModules() ([]models.ModuleRegistry, error) {
	var modules []models.ModuleRegistry
	err := r.db.Order("module_name ASC").Find(&modules).Error
	return modules, err
}

// ListActiveModules 获取所有状态为up的模块
func (r *ModuleRegistryRepository) ListActiveModules() ([]models.ModuleRegistry, error) {
	var modules []models.ModuleRegistry
	err := r.db.Where("status = ?", "up").Order("module_name ASC").Find(&modules).Error
	return modules, err
}

// DeleteModule 删除模块注册
func (r *ModuleRegistryRepository) DeleteModule(moduleName string) error {
	return r.db.Where("module_name = ?", moduleName).Delete(&models.ModuleRegistry{}).Error
}

// MarkStaleModules 标记超时未心跳的模块为down
func (r *ModuleRegistryRepository) MarkStaleModules(timeout time.Duration) error {
	cutoffTime := time.Now().Add(-timeout)
	return r.db.Model(&models.ModuleRegistry{}).
		Where("last_heartbeat < ? AND status = ?", cutoffTime, "up").
		Update("status", "down").Error
}
