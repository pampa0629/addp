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
func (r *ModuleRegistryRepository) Register(req *models.ModuleRegistrationRequest, leaseDuration time.Duration) (bool, error) {
	metadata, err := marshalRegistryJSON(req.Metadata)
	if err != nil {
		return false, err
	}
	configuration, err := marshalRegistryJSON(req.ConfigurationManagement)
	if err != nil {
		return false, err
	}
	taskProvider, err := marshalRegistryJSON(req.TaskProvider)
	if err != nil {
		return false, err
	}
	now := time.Now()
	changed := false
	err = r.db.Transaction(func(tx *gorm.DB) error {
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
		routeChanged := false
		if definition.RoutePrefix != req.RoutePrefix {
			definitionUpdates["route_prefix"] = req.RoutePrefix
			routeChanged = true
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
		var previous models.ModuleRuntimeInstance
		instanceQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("module_definition_id = ? AND instance_id = ?", definition.ID, req.InstanceID).
			First(&previous)
		instanceChanged := false
		switch {
		case errors.Is(instanceQuery.Error, gorm.ErrRecordNotFound):
			instanceChanged = true
		case instanceQuery.Error != nil:
			return instanceQuery.Error
		case previous.Role != req.Role || previous.ModuleURL != req.ModuleURL ||
			previous.Status != models.ModuleRuntimeStatusUp || !previous.LeaseExpiresAt.After(now):
			instanceChanged = true
		}
		if definition.Enabled && instanceChanged &&
			(req.Role == models.ModuleRuntimeRoleBackend || previous.Role == models.ModuleRuntimeRoleBackend) {
			changed = true
		}
		if definition.Enabled && routeChanged {
			var activeBackendCount int64
			if err := tx.Model(&models.ModuleRuntimeInstance{}).
				Where("module_definition_id = ? AND role = ? AND status = ? AND lease_expires_at > ?", definition.ID,
					models.ModuleRuntimeRoleBackend, models.ModuleRuntimeStatusUp, now).
				Count(&activeBackendCount).Error; err != nil {
				return err
			}
			changed = changed || activeBackendCount > 0
		}
		instance := models.ModuleRuntimeInstance{
			ModuleDefinitionID: definition.ID, InstanceID: req.InstanceID, Role: req.Role,
			ModuleURL: req.ModuleURL, HealthCheckURL: req.HealthCheckURL,
			Status: models.ModuleRuntimeStatusUp, LastHeartbeat: now, LeaseExpiresAt: now.Add(leaseDuration),
			Metadata: metadata, RegisteredAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "module_definition_id"}, {Name: "instance_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"role": req.Role, "module_url": req.ModuleURL, "health_check_url": req.HealthCheckURL,
				"status": models.ModuleRuntimeStatusUp, "last_heartbeat": now,
				"lease_expires_at": now.Add(leaseDuration), "metadata": metadata, "updated_at": now,
			}),
		}).Create(&instance).Error; err != nil {
			return err
		}
		if changed {
			return bumpModuleRegistryRevision(tx, now)
		}
		return nil
	})
	return changed, err
}

func (r *ModuleRegistryRepository) UpdateEnabled(moduleName string, enabled bool, version int64) (*models.ModuleDefinition, bool, error) {
	now := time.Now()
	changed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var definition models.ModuleDefinition
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("module_name = ?", moduleName).First(&definition).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonapi.ErrNotFound
			}
			return err
		}
		if definition.Version != version {
			return ErrModuleDefinitionVersionConflict
		}
		if definition.Enabled == enabled {
			return nil
		}
		changed = true
		if err := tx.Model(&definition).Updates(map[string]interface{}{
			"enabled": enabled, "version": gorm.Expr("version + 1"), "updated_at": now,
		}).Error; err != nil {
			return err
		}
		return bumpModuleRegistryRevision(tx, now)
	})
	if err != nil {
		return nil, false, err
	}
	definition, err := r.GetModule(moduleName)
	return definition, changed, err
}

func (r *ModuleRegistryRepository) UpdateHeartbeat(moduleName, instanceID string, leaseDuration time.Duration) (bool, error) {
	now := time.Now()
	changed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var instance models.ModuleRuntimeInstance
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("instance_id = ? AND module_definition_id = (?)", instanceID,
				tx.Model(&models.ModuleDefinition{}).Select("id").Where("module_name = ?", moduleName)).
			First(&instance).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonapi.ErrNotFound
			}
			return err
		}
		changed = instance.Status != models.ModuleRuntimeStatusUp || !instance.LeaseExpiresAt.After(now)
		if changed {
			var definition models.ModuleDefinition
			if err := tx.Select("enabled").Where("id = ?", instance.ModuleDefinitionID).First(&definition).Error; err != nil {
				return err
			}
			changed = definition.Enabled && instance.Role == models.ModuleRuntimeRoleBackend
		}
		if err := tx.Model(&instance).Updates(map[string]interface{}{
			"last_heartbeat": now, "lease_expires_at": now.Add(leaseDuration), "status": models.ModuleRuntimeStatusUp,
		}).Error; err != nil {
			return err
		}
		if changed {
			return bumpModuleRegistryRevision(tx, now)
		}
		return nil
	})
	return changed, err
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

func (r *ModuleRegistryRepository) MarkStaleModules(now time.Time) (bool, error) {
	changed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var routableCount int64
		if err := tx.Model(&models.ModuleRuntimeInstance{}).
			Joins("JOIN module_definitions ON module_definitions.id = module_runtime_instances.module_definition_id").
			Where("module_runtime_instances.lease_expires_at <= ? AND module_runtime_instances.status = ? AND module_runtime_instances.role = ? AND module_definitions.enabled = ?",
				now, models.ModuleRuntimeStatusUp, models.ModuleRuntimeRoleBackend, true).
			Count(&routableCount).Error; err != nil {
			return err
		}
		result := tx.Model(&models.ModuleRuntimeInstance{}).
			Where("lease_expires_at <= ? AND status = ?", now, models.ModuleRuntimeStatusUp).
			Updates(map[string]interface{}{"status": models.ModuleRuntimeStatusDown, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		changed = routableCount > 0
		if changed {
			return bumpModuleRegistryRevision(tx, now)
		}
		return nil
	})
	return changed, err
}

func (r *ModuleRegistryRepository) Deregister(moduleName, instanceID string) (bool, error) {
	now := time.Now()
	changed := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var definition models.ModuleDefinition
		if err := tx.Where("module_name = ?", moduleName).First(&definition).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var instance models.ModuleRuntimeInstance
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("module_definition_id = ? AND instance_id = ?", definition.ID, instanceID).First(&instance).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if instance.Status != models.ModuleRuntimeStatusUp {
			return nil
		}
		changed = definition.Enabled && instance.Role == models.ModuleRuntimeRoleBackend && instance.LeaseExpiresAt.After(now)
		if err := tx.Model(&instance).Updates(map[string]interface{}{
			"status": models.ModuleRuntimeStatusDown, "lease_expires_at": now, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if changed {
			return bumpModuleRegistryRevision(tx, now)
		}
		return nil
	})
	return changed, err
}

func (r *ModuleRegistryRepository) GetRegistryRevision() (int64, error) {
	var state models.ModuleRegistryState
	if err := r.db.Where("id = ?", 1).First(&state).Error; err != nil {
		return 0, err
	}
	return state.Revision, nil
}

func bumpModuleRegistryRevision(tx *gorm.DB, now time.Time) error {
	result := tx.Model(&models.ModuleRegistryState{}).Where("id = ?", 1).
		Updates(map[string]interface{}{"revision": gorm.Expr("revision + 1"), "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("module registry state is missing")
	}
	return nil
}
