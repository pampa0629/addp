package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

var ErrObservabilityIdentityBudget = errors.New("observability identity snapshot exceeds budget")

// ObservabilityIdentities reads node intent, module intent and binding evidence
// from one database snapshot. A failed/oversized snapshot is never a partial success.
func (r *ModuleRegistryRepository) ObservabilityIdentities(ctx context.Context) (*commonmodels.ObservabilityIdentitySnapshot, error) {
	result := &commonmodels.ObservabilityIdentitySnapshot{
		Nodes:           []commonmodels.ObservabilityNodeIdentity{},
		ModuleInstances: []commonmodels.ObservabilityModuleIdentity{},
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result.ObservedAt = time.Now().UTC()
		if err := tx.Model(&models.HostNode{}).Select("node_id", "version").Where("enabled = ?", true).
			Order("node_id ASC").Limit(commonmodels.ObservabilityIdentityNodeLimit + 1).Scan(&result.Nodes).Error; err != nil {
			return err
		}
		if len(result.Nodes) > commonmodels.ObservabilityIdentityNodeLimit {
			return ErrObservabilityIdentityBudget
		}
		var candidates []models.ModuleRuntimeInstanceRow
		if err := tx.Table("module_runtime_instances").
			Joins("JOIN module_definitions ON module_definitions.id = module_runtime_instances.module_definition_id").
			Select("module_runtime_instances.module_definition_id, module_runtime_instances.instance_id, module_runtime_instances.role, module_runtime_instances.declared_node_id, module_runtime_instances.registration_client_id, module_runtime_instances.lease_expires_at, module_definitions.module_name").
			Where("module_definitions.enabled = ? AND module_runtime_instances.status = ? AND module_runtime_instances.lease_expires_at > ? AND module_runtime_instances.declared_node_id <> ''", true, models.ModuleRuntimeStatusUp, result.ObservedAt).
			Order("module_definitions.module_name ASC, module_runtime_instances.instance_id ASC").
			Limit(commonmodels.ObservabilityIdentityInstanceLimit + 1).Scan(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) > commonmodels.ObservabilityIdentityInstanceLimit {
			return ErrObservabilityIdentityBudget
		}
		instances := make([]*models.ModuleRuntimeInstance, len(candidates))
		for i := range candidates {
			instances[i] = &candidates[i].ModuleRuntimeInstance
		}
		// Reuse the canonical current binding decision, with the same transaction.
		if err := NewModuleRegistryRepository(tx).resolveNodeBindings(instances); err != nil {
			return err
		}
		for _, candidate := range candidates {
			if candidate.NodeBindingState != "bound" || !candidate.LeaseExpiresAt.After(result.ObservedAt) {
				continue
			}
			result.ModuleInstances = append(result.ModuleInstances, commonmodels.ObservabilityModuleIdentity{
				ModuleName: candidate.ModuleName, InstanceID: candidate.InstanceID, Role: candidate.Role,
				NodeID: candidate.NodeID, LeaseExpiresAt: candidate.LeaseExpiresAt.UTC(),
			})
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if int64(len(encoded)) > commonmodels.ObservabilityIdentityResponseLimit {
			return ErrObservabilityIdentityBudget
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, nil
}
