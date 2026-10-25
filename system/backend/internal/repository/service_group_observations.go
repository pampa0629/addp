package repository

import (
	"encoding/json"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func observeServiceGroups(tx *gorm.DB, plan *commonmodels.ServiceExpectationDocument, at time.Time) (*commonmodels.ServiceGroupSnapshot, error) {
	result := &commonmodels.ServiceGroupSnapshot{State: "unavailable", Reason: "invalid_intent", ObservedAt: at, Groups: []commonmodels.ServiceGroupObservation{}}
	if plan.Validate(at) != nil {
		return result, nil
	}
	result.State, result.Reason, result.DeploymentID, result.Revision = "available", "", plan.DeploymentID, plan.Revision
	if len(plan.Groups) == 0 {
		return result, nil
	}
	modules := make([]string, 0, len(plan.Groups))
	seen := map[string]bool{}
	for _, group := range plan.Groups {
		if !seen[group.ModuleName] {
			modules = append(modules, group.ModuleName)
			seen[group.ModuleName] = true
		}
	}
	var definitions []models.ModuleDefinition
	if err := tx.Select("module_name", "enabled").Where("module_name IN ?", modules).Find(&definitions).Error; err != nil {
		return nil, err
	}
	enabled := map[string]bool{}
	for _, module := range definitions {
		enabled[module.ModuleName] = module.Enabled
	}
	var members []struct {
		ModuleName     string
		Role           string
		Status         string
		LeaseExpiresAt time.Time
		Metadata       datatypes.JSON
	}
	if err := tx.Table("module_runtime_instances AS i").Joins("JOIN module_definitions AS m ON m.id=i.module_definition_id").
		Select("m.module_name, i.role, i.status, i.lease_expires_at, i.metadata").
		Where("m.module_name IN ? AND i.role IN ?", modules, []string{"backend", "worker"}).
		Order("i.id ASC").Limit(commonmodels.ObservabilityIdentityInstanceLimit + 1).Scan(&members).Error; err != nil {
		return nil, err
	}
	if len(members) > commonmodels.ObservabilityIdentityInstanceLimit {
		result.State, result.Reason = "unavailable", "instance_budget"
		return result, nil
	}
	counts, online, unknownPurpose := map[string]int{}, map[string]int{}, map[string]bool{}
	for _, member := range members {
		active := member.Status == models.ModuleRuntimeStatusUp && member.LeaseExpiresAt.After(at)
		key := commonmodels.ServiceGroupExpectation{ModuleName: member.ModuleName, Role: member.Role}
		if member.Role == "worker" {
			var metadata map[string]any
			if json.Unmarshal(member.Metadata, &metadata) != nil {
				unknownPurpose[member.ModuleName] = unknownPurpose[member.ModuleName] || active
				continue
			}
			key.RuntimeName, _ = metadata["runtime_name"].(string)
			check := key
			check.ExpectedReplicas, check.DesiredState, check.ChangedAt = 1, "running", at
			if !check.Valid(at) {
				unknownPurpose[member.ModuleName] = unknownPurpose[member.ModuleName] || active
				continue
			}
		}
		counts[key.Key()]++
		if active {
			online[key.Key()]++
		}
	}
	for _, group := range plan.Groups {
		row := commonmodels.ServiceGroupObservation{ServiceGroupExpectation: group, State: "observed",
			OnlineReplicas: online[group.Key()], RegisteredCount: counts[group.Key()]}
		switch value, exists := enabled[group.ModuleName]; {
		case group.DesiredState == "planned_stopped":
			row.State, row.Reason = "paused", "planned_stopped"
		case !exists:
			row.State, row.Reason = "unknown", "module_unregistered"
		case !value:
			row.State, row.Reason = "paused", "module_disabled"
		case group.Role == "worker" && unknownPurpose[group.ModuleName]:
			row.State, row.Reason = "unknown", "runtime_name_unavailable"
		}
		result.Groups = append(result.Groups, row)
	}
	return result, nil
}
