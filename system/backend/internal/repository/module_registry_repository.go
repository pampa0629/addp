package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"
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
var ErrProcessMetricsDeclarationImmutable = errors.New("process metrics declaration is immutable")

func NewModuleRegistryRepository(db *gorm.DB) *ModuleRegistryRepository {
	return &ModuleRegistryRepository{db: db}
}

func (r *ModuleRegistryRepository) AreActivePermissionsOwnedBy(owner string, keys []string) (bool, error) {
	if len(keys) == 0 {
		return true, nil
	}
	var count int64
	if err := r.db.Table("permissions").
		Where("owner_module = ? AND status = ? AND permission_key IN ?", owner, "active", keys).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count == int64(len(keys)), nil
}

func marshalRegistryJSON(value interface{}) (datatypes.JSON, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	return datatypes.JSON(data), err
}

func registeredHost(moduleURL, healthCheckURL string) string {
	address := moduleURL
	if address == "" {
		address = healthCheckURL
	}
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// PublishDeclaration changes only publisher-owned fields under the same row lock as administrator updates.
func (r *ModuleRegistryRepository) PublishDeclaration(name string, version int64, fields models.ModuleDeclarationFields) (*models.ModuleDefinition, error) {
	var definition models.ModuleDefinition
	var configuration, provider datatypes.JSON
	var err error
	if fields.ConfigurationManagement != nil {
		if configuration, err = marshalRegistryJSON(fields.ConfigurationManagement); err != nil {
			return nil, err
		}
	}
	if fields.TaskProvider != nil {
		if provider, err = marshalRegistryJSON(fields.TaskProvider); err != nil {
			return nil, err
		}
	}
	err = r.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC().Truncate(time.Microsecond)
		if version == 0 {
			definition = models.ModuleDefinition{ModuleName: name, Enabled: true, RoutePrefix: *fields.RoutePrefix, ConfigurationManagement: configuration, TaskProvider: provider, Version: 1, DeclarationPublishedAt: &now}
			created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "module_name"}}, DoNothing: true}).Create(&definition)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected == 0 {
				return ErrModuleDefinitionVersionConflict
			}
			return nil
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("module_name = ?", name).First(&definition).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if definition.Version != version {
			return ErrModuleDefinitionVersionConflict
		}
		routeChanged := definition.RoutePrefix != *fields.RoutePrefix
		if definition.DeclarationPublishedAt != nil && !routeChanged && equalDeclarationJSON(definition.ConfigurationManagement, configuration) && equalDeclarationJSON(definition.TaskProvider, provider) {
			return nil
		}
		if err := tx.Model(&definition).Updates(map[string]any{
			"route_prefix": *fields.RoutePrefix, "configuration_management": configuration, "task_provider": provider,
			"version": version + 1, "declaration_published_at": now,
		}).Error; err != nil {
			return err
		}
		if routeChanged && definition.Enabled {
			var active int64
			if err := tx.Model(&models.ModuleRuntimeInstance{}).Where("module_definition_id = ? AND role = ? AND status = ? AND lease_expires_at > ?", definition.ID, models.ModuleRuntimeRoleBackend, models.ModuleRuntimeStatusUp, now).Count(&active).Error; err != nil {
				return err
			}
			if active > 0 {
				if err := bumpModuleRegistryRevision(tx, now); err != nil {
					return err
				}
			}
		}
		return tx.First(&definition, definition.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &definition, nil
}

// PostgreSQL JSONB can reorder keys and insert whitespace on read.
func equalDeclarationJSON(left, right datatypes.JSON) bool {
	if bytes.Equal(left, right) {
		return true
	}
	var a, b any
	decode := func(raw datatypes.JSON, value *any) error {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		return decoder.Decode(value)
	}
	return decode(left, &a) == nil && decode(right, &b) == nil && reflect.DeepEqual(a, b)
}

func (r *ModuleRegistryRepository) GetDeclaration(name string) (*models.ModuleDefinition, error) {
	var definition models.ModuleDefinition
	if err := r.db.Where("module_name = ?", name).First(&definition).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &definition, nil
}

var ErrModuleDeclarationNotPublished = errors.New("module declaration not published")

// Register records the process lease against a published definition.
func (r *ModuleRegistryRepository) Register(req *models.ModuleRegistrationRequest, leaseDuration time.Duration) (bool, error) {
	metadata, err := marshalRegistryJSON(req.Metadata)
	if err != nil {
		return false, err
	}
	hostNodeIPs, err := marshalRegistryJSON(req.HostNodeIPs)
	if err != nil {
		return false, err
	}
	now := time.Now()
	changed := false
	err = r.db.Transaction(func(tx *gorm.DB) error {
		var definition models.ModuleDefinition
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("module_name = ?", req.ModuleName).First(&definition).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && definition.DeclarationPublishedAt == nil) {
			return ErrModuleDeclarationNotPublished
		}
		if err != nil {
			return err
		}
		var previous models.ModuleRuntimeInstance
		instanceQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("module_definition_id = ? AND instance_id = ?", definition.ID, req.InstanceID).
			First(&previous)
		instanceChanged := false
		if instanceQuery.Error == nil {
			declarationEqual := previous.ProcessMetrics.Equal(req.ProcessMetrics)
			if !declarationEqual || (previous.ProcessMetrics != nil && (previous.Role != req.Role || previous.ProcessStartedAt == nil || !previous.ProcessStartedAt.Truncate(time.Microsecond).Equal(req.ProcessStartedAt.Truncate(time.Microsecond)))) {
				return ErrProcessMetricsDeclarationImmutable
			}
		}
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
		instance := models.ModuleRuntimeInstance{
			ProcessMetrics:     req.ProcessMetrics,
			ModuleDefinitionID: definition.ID, InstanceID: req.InstanceID, Role: req.Role,
			DeclaredNodeID: req.NodeID, RegistrationClientID: req.RegistrationClientID,
			ModuleURL: req.ModuleURL, HealthCheckURL: req.HealthCheckURL,
			RegisteredHost: registeredHost(req.ModuleURL, req.HealthCheckURL),
			HostNodeName:   req.HostNodeName, RuntimeHostname: req.RuntimeHostname,
			HostNodeIPs: req.HostNodeIPs,
			Status:      models.ModuleRuntimeStatusUp, LastHeartbeat: now, LeaseExpiresAt: now.Add(leaseDuration),
			ProcessStartedAt: &req.ProcessStartedAt,
			Metadata:         metadata, RegisteredAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "module_definition_id"}, {Name: "instance_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"role": req.Role, "module_url": req.ModuleURL, "health_check_url": req.HealthCheckURL,
				"registered_host": instance.RegisteredHost,
				"host_node_name":  req.HostNodeName, "runtime_hostname": req.RuntimeHostname,
				"host_node_ips": hostNodeIPs,
				"status":        models.ModuleRuntimeStatusUp, "last_heartbeat": now,
				"lease_expires_at": now.Add(leaseDuration), "metadata": metadata, "updated_at": now,
				"process_started_at": req.ProcessStartedAt, "stopped_at": nil, "stop_reason": "",
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

// CreateCatalogDefinition records platform administrator intent without a
// runtime lease. OnConflict never overwrites a concurrent service registration.
func (r *ModuleRegistryRepository) CreateCatalogDefinition(enabled bool) (*models.ModuleDefinition, error) {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		// Use a map: GORM's model default:true must not replace explicit false.
		result := tx.Model(&models.ModuleDefinition{}).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "module_name"}}, DoNothing: true,
		}).Create(map[string]interface{}{
			"module_name": "catalog", "route_prefix": "/catalog", "enabled": enabled,
			"version": int64(1), "created_at": now, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrModuleDefinitionVersionConflict
		}
		return bumpModuleRegistryRevision(tx, now)
	})
	if err != nil {
		return nil, err
	}
	return r.GetModule("catalog")
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
			"stopped_at": nil, "stop_reason": "",
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
	err := r.db.Where("module_name = ?", moduleName).First(&definition).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	instances, err := r.listCurrentRuntimeInstances([]uint{definition.ID}, time.Now())
	if err != nil {
		return nil, err
	}
	definition.RuntimeInstances = instances[definition.ID]
	return &definition, nil
}

func (r *ModuleRegistryRepository) ListModules() ([]models.ModuleDefinition, error) {
	var definitions []models.ModuleDefinition
	if err := r.db.Order("module_name ASC").Find(&definitions).Error; err != nil {
		return nil, err
	}
	definitionIDs := make([]uint, 0, len(definitions))
	for index := range definitions {
		definitionIDs = append(definitionIDs, definitions[index].ID)
	}
	instances, err := r.listCurrentRuntimeInstances(definitionIDs, time.Now())
	if err != nil {
		return nil, err
	}
	for index := range definitions {
		definitions[index].RuntimeInstances = instances[definitions[index].ID]
	}
	return definitions, nil
}

// listCurrentRuntimeInstances 返回有界当前投影：全部有效租约实例，以及每个
// 当前无有效实例角色的最近一次离线观测。窗口排序在数据库完成，避免加载全部历史。
func (r *ModuleRegistryRepository) listCurrentRuntimeInstances(definitionIDs []uint, now time.Time) (map[uint][]models.ModuleRuntimeInstance, error) {
	result := make(map[uint][]models.ModuleRuntimeInstance, len(definitionIDs))
	if len(definitionIDs) == 0 {
		return result, nil
	}
	base := r.db.Model(&models.ModuleRuntimeInstance{}).
		Where("module_definition_id IN ?", definitionIDs).
		Select(`module_runtime_instances.*,
			CASE WHEN status = ? AND lease_expires_at > ? THEN 1 ELSE 0 END AS effective_up,
			ROW_NUMBER() OVER (
				PARTITION BY module_definition_id, role
				ORDER BY CASE WHEN status = ? AND lease_expires_at > ? THEN 0 ELSE 1 END,
					updated_at DESC, id DESC
			) AS role_rank`,
			models.ModuleRuntimeStatusUp, now, models.ModuleRuntimeStatusUp, now)
	var instances []models.ModuleRuntimeInstance
	if err := r.db.Table("(?) AS current_instances", base).
		Where("effective_up = 1 OR role_rank = 1").
		Order("module_definition_id ASC, role ASC, instance_id ASC").
		Scan(&instances).Error; err != nil {
		return nil, err
	}
	pointers := make([]*models.ModuleRuntimeInstance, 0, len(instances))
	for index := range instances {
		pointers = append(pointers, &instances[index])
	}
	if err := r.resolveNodeBindings(pointers); err != nil {
		return nil, err
	}
	for index := range instances {
		instance := instances[index]
		result[instance.ModuleDefinitionID] = append(result[instance.ModuleDefinitionID], instance)
	}
	return result, nil
}

func (r *ModuleRegistryRepository) ListModuleRuntimeInstances(
	filter models.ModuleRuntimeInstanceFilter,
	now time.Time,
) ([]models.ModuleRuntimeInstanceRow, int64, error) {
	query := r.db.Table("module_runtime_instances")
	timeColumn := "module_runtime_instances.registered_at"
	if filter.TimeBasis == models.ModuleRuntimeTimeOffline {
		// Match the read projection before the lease scanner persists the observation.
		observations := r.db.Model(&models.ModuleRuntimeInstance{}).Select(`module_runtime_instances.*,
			CASE WHEN status = ? AND lease_expires_at <= ? THEN lease_expires_at
			ELSE stopped_at END AS offline_determined_at`, models.ModuleRuntimeStatusUp, now)
		query = r.db.Table("(?) AS module_runtime_instances", observations)
		timeColumn = "module_runtime_instances.offline_determined_at"
		query = query.Where(timeColumn + " IS NOT NULL")
	}
	query = query.Joins("JOIN module_definitions ON module_definitions.id = module_runtime_instances.module_definition_id")
	if filter.ProcessMetricsDeclared != nil {
		if *filter.ProcessMetricsDeclared {
			query = query.Where("module_runtime_instances.process_metrics IS NOT NULL")
		} else {
			query = query.Where("module_runtime_instances.process_metrics IS NULL")
		}
	}
	if len(filter.IDs) > 0 {
		query = query.Where("module_runtime_instances.id IN ?", filter.IDs)
	}
	if len(filter.InstanceIDs) > 0 {
		query = query.Where("module_runtime_instances.instance_id IN ?", filter.InstanceIDs)
	}
	if filter.ModuleName != "" {
		query = query.Where("module_definitions.module_name = ?", filter.ModuleName)
	}
	if filter.RegisteredHost != "" {
		query = query.Where("module_runtime_instances.registered_host = ?", filter.RegisteredHost)
	}
	if filter.NodeIP != "" {
		if r.db.Dialector.Name() == "sqlite" {
			query = query.Where("EXISTS (SELECT 1 FROM json_each(module_runtime_instances.host_node_ips) WHERE value = ?)", filter.NodeIP)
		} else {
			addresses, _ := json.Marshal([]string{filter.NodeIP})
			query = query.Where("module_runtime_instances.host_node_ips @> ?::jsonb", string(addresses))
		}
	}
	if filter.NodeName != "" {
		query = query.Where("LOWER(module_runtime_instances.host_node_name) = ? OR LOWER(module_runtime_instances.runtime_hostname) = ?", filter.NodeName, filter.NodeName)
	}
	if filter.Role != "" {
		query = query.Where("module_runtime_instances.role = ?", filter.Role)
	}
	switch filter.Status {
	case models.ModuleRuntimeStatusUp:
		query = query.Where("module_runtime_instances.status = ? AND module_runtime_instances.lease_expires_at > ?", models.ModuleRuntimeStatusUp, now)
	case models.ModuleRuntimeStatusDown:
		query = query.Where("module_runtime_instances.status = ? OR module_runtime_instances.lease_expires_at <= ?", models.ModuleRuntimeStatusDown, now)
	}
	if filter.StopReason != "" {
		query = query.Where(`CASE
			WHEN module_runtime_instances.status = ? AND module_runtime_instances.lease_expires_at <= ? THEN ?
			WHEN module_runtime_instances.status = ? THEN module_runtime_instances.stop_reason
			ELSE '' END = ?`, models.ModuleRuntimeStatusUp, now, models.ModuleRuntimeStopExpired,
			models.ModuleRuntimeStatusDown, filter.StopReason)
	}
	if !filter.TimeFrom.IsZero() {
		query = query.Where(timeColumn+" >= ?", filter.TimeFrom)
	}
	if !filter.TimeTo.IsZero() {
		query = query.Where(timeColumn+" < ?", filter.TimeTo)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var instances []models.ModuleRuntimeInstanceRow
	if err := query.Select("module_runtime_instances.*, module_definitions.module_name AS module_name").
		Order(timeColumn + " DESC, module_runtime_instances.id DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Scan(&instances).Error; err != nil {
		return nil, 0, err
	}
	pointers := make([]*models.ModuleRuntimeInstance, 0, len(instances))
	for index := range instances {
		pointers = append(pointers, &instances[index].ModuleRuntimeInstance)
	}
	if err := r.resolveNodeBindings(pointers); err != nil {
		return nil, 0, err
	}
	return instances, total, nil
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
			Updates(map[string]interface{}{"status": models.ModuleRuntimeStatusDown, "stopped_at": gorm.Expr("lease_expires_at"), "stop_reason": models.ModuleRuntimeStopExpired, "updated_at": now})
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
		leaseValid := instance.LeaseExpiresAt.After(now)
		changed = definition.Enabled && instance.Role == models.ModuleRuntimeRoleBackend && leaseValid
		stoppedAt := now
		stopReason := models.ModuleRuntimeStopGraceful
		if !leaseValid {
			stoppedAt = instance.LeaseExpiresAt
			stopReason = models.ModuleRuntimeStopExpired
		}
		if err := tx.Model(&instance).Updates(map[string]interface{}{
			"status": models.ModuleRuntimeStatusDown, "lease_expires_at": stoppedAt, "stopped_at": stoppedAt,
			"stop_reason": stopReason, "updated_at": now,
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
