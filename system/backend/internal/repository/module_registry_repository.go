package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	commonapi "github.com/addp/common/api"
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/system/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ModuleRegistryRepository struct{ db *gorm.DB }

var ErrModuleDefinitionVersionConflict = errors.New("module definition version conflict")

func NewModuleRegistryRepository(db *gorm.DB) *ModuleRegistryRepository {
	return &ModuleRegistryRepository{db: db}
}

func marshalRegistryJSON(value interface{}) (datatypes.JSON, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	return datatypes.JSON(data), err
}

// Register 原子维护持久定义和当前进程实例；不覆盖管理员 enabled 状态。
func (r *ModuleRegistryRepository) Register(req *models.ModuleRegistrationRequest, leaseDuration time.Duration) error {
	metadata, err := marshalRegistryJSON(req.Metadata)
	if err != nil {
		return err
	}
	configuration, err := marshalRegistryJSON(req.ConfigurationManagement)
	if err != nil {
		return err
	}
	taskProvider, err := marshalRegistryJSON(req.TaskProvider)
	if err != nil {
		return err
	}
	now := time.Now()
	return r.db.Transaction(func(tx *gorm.DB) error {
		definition := models.ModuleDefinition{
			ModuleName: req.ModuleName, RoutePrefix: req.RoutePrefix, Enabled: true,
			Version: 1, ConfigurationManagement: configuration, TaskProvider: taskProvider,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "module_name"}},
			DoNothing: true,
		}).Create(&definition).Error; err != nil {
			return err
		}
		if err := tx.Where("module_name = ?", req.ModuleName).First(&definition).Error; err != nil {
			return err
		}
		definitionUpdates := map[string]interface{}{}
		if definition.RoutePrefix != req.RoutePrefix {
			definitionUpdates["route_prefix"] = req.RoutePrefix
		}
		// Worker/Scheduler 不携带模块级声明，不能因此清空 Backend 已发布的定义。
		if req.ConfigurationManagement != nil && !bytes.Equal(definition.ConfigurationManagement, configuration) {
			definitionUpdates["configuration_management"] = configuration
		}
		// Backend 是 TaskProvider 角色声明的唯一发布者。Backend 显式不携带声明
		// 表示撤销该角色；Worker/Scheduler 的 nil 只表示不参与声明维护。
		if req.Role == models.ModuleRuntimeRoleBackend && !bytes.Equal(definition.TaskProvider, taskProvider) {
			if req.TaskProvider == nil {
				definitionUpdates["task_provider"] = nil
			} else {
				definitionUpdates["task_provider"] = taskProvider
			}
		}
		if len(definitionUpdates) > 0 {
			definitionUpdates["version"] = gorm.Expr("version + 1")
			definitionUpdates["updated_at"] = now
			if err := tx.Model(&models.ModuleDefinition{}).Where("id = ?", definition.ID).Updates(definitionUpdates).Error; err != nil {
				return err
			}
		}
		instance := models.ModuleRuntimeInstance{
			ModuleDefinitionID: definition.ID, InstanceID: req.InstanceID, Role: req.Role,
			ModuleURL: req.ModuleURL, HealthCheckURL: req.HealthCheckURL,
			Status: models.ModuleRuntimeStatusUp, LastHeartbeat: now, LeaseExpiresAt: now.Add(leaseDuration),
			Metadata: metadata, RegisteredAt: now,
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "module_definition_id"}, {Name: "instance_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"role": req.Role, "module_url": req.ModuleURL, "health_check_url": req.HealthCheckURL,
				"status": models.ModuleRuntimeStatusUp, "last_heartbeat": now,
				"lease_expires_at": now.Add(leaseDuration), "metadata": metadata, "updated_at": now,
			}),
		}).Create(&instance).Error
	})
}

func (r *ModuleRegistryRepository) UpdateEnabled(moduleName string, enabled bool, version int64) (*models.ModuleDefinition, error) {
	now := time.Now()
	result := r.db.Model(&models.ModuleDefinition{}).
		Where("module_name = ? AND version = ?", moduleName, version).
		Updates(map[string]interface{}{
			"enabled": enabled, "version": gorm.Expr("version + 1"), "updated_at": now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		var count int64
		if err := r.db.Model(&models.ModuleDefinition{}).Where("module_name = ?", moduleName).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, commonapi.ErrNotFound
		}
		return nil, ErrModuleDefinitionVersionConflict
	}
	return r.GetModule(moduleName)
}

func (r *ModuleRegistryRepository) UpdateHeartbeat(moduleName, instanceID string, leaseDuration time.Duration) error {
	now := time.Now()
	result := r.db.Model(&models.ModuleRuntimeInstance{}).
		Where("instance_id = ? AND module_definition_id = (?)", instanceID,
			r.db.Model(&models.ModuleDefinition{}).Select("id").Where("module_name = ?", moduleName)).
		Updates(map[string]interface{}{
			"last_heartbeat": now, "lease_expires_at": now.Add(leaseDuration), "status": models.ModuleRuntimeStatusUp,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *ModuleRegistryRepository) GetModule(moduleName string) (*models.ModuleDefinition, error) {
	var definition models.ModuleDefinition
	err := r.db.Preload("RuntimeInstances", func(db *gorm.DB) *gorm.DB {
		return db.Order("role ASC, instance_id ASC")
	}).Where("module_name = ?", moduleName).First(&definition).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &definition, nil
}

func (r *ModuleRegistryRepository) ListModules() ([]models.ModuleDefinition, error) {
	var definitions []models.ModuleDefinition
	err := r.db.Preload("RuntimeInstances", func(db *gorm.DB) *gorm.DB {
		return db.Order("role ASC, instance_id ASC")
	}).Order("module_name ASC").Find(&definitions).Error
	return definitions, err
}

func (r *ModuleRegistryRepository) MarkStaleModules(now time.Time) error {
	return r.db.Model(&models.ModuleRuntimeInstance{}).
		Where("lease_expires_at <= ? AND status = ?", now, models.ModuleRuntimeStatusUp).
		Update("status", models.ModuleRuntimeStatusDown).Error
}
