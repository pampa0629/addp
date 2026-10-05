package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type HostNodeRepository struct{ db *gorm.DB }

func NewHostNodeRepository(db *gorm.DB) *HostNodeRepository { return &HostNodeRepository{db: db} }
func (r *HostNodeRepository) Get(ctx context.Context, id string) (*models.HostNode, error) {
	var node models.HostNode
	err := r.db.WithContext(ctx).Where("node_id = ?", id).First(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, commonapi.ErrNotFound
	}
	return &node, err
}
func (r *HostNodeRepository) List(ctx context.Context, page, size int, search string) ([]models.HostNode, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.HostNode{})
	if search != "" {
		// Escape SQL LIKE metacharacters; search is a literal fragment, not a pattern language.
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search)) + "%"
		query = query.Where("LOWER(display_name) LIKE ? ESCAPE '!' OR LOWER(CAST(addresses AS TEXT)) LIKE ? ESCAPE '!'", pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	nodes := make([]models.HostNode, 0)
	err := query.Order("created_at DESC, node_id ASC").Offset((page - 1) * size).Limit(size).Find(&nodes).Error
	return nodes, total, err
}

// Save atomically replaces the aggregate and records its audit in the same transaction.
func (r *HostNodeRepository) Save(ctx context.Context, node *models.HostNode, expected int64, audit func(*gorm.DB, *models.HostNode, models.HostNode) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before *models.HostNode
		now := time.Now().UTC()
		if expected == 0 {
			node.Version = 1
			node.CreatedAt = now
			node.UpdatedAt = now
			if err := tx.Create(node).Error; err != nil {
				return err
			}
		} else {
			var old models.HostNode
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", node.NodeID).First(&old).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return commonapi.ErrNotFound
				}
				return err
			}
			if old.Version != expected {
				return commonapi.ErrConflict
			}
			before = &old
			node.Version = expected + 1
			node.CreatedAt = old.CreatedAt
			node.UpdatedAt = now
			result := tx.Model(&models.HostNode{}).Where("node_id = ? AND version = ?", node.NodeID, expected).Select("display_name", "node_kind", "addresses", "enabled", "allowed_module_bindings", "version", "updated_at").Updates(node)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return commonapi.ErrConflict
			}
		}
		return audit(tx, before, *node)
	})
}

// resolveNodeBindings applies the current node allowlist, never registration-time cached status.
func (r *ModuleRegistryRepository) resolveNodeBindings(instances []*models.ModuleRuntimeInstance) error {
	ids := make([]string, 0)
	definitions := make([]uint, 0)
	seenNodes := make(map[string]bool)
	seenDefinitions := make(map[uint]bool)
	for _, instance := range instances {
		instance.NodeID = ""
		instance.NodeBindingState = "unbound"
		instance.NodeBindingReason = ""
		if instance.DeclaredNodeID != "" {
			if !seenNodes[instance.DeclaredNodeID] {
				ids = append(ids, instance.DeclaredNodeID)
				seenNodes[instance.DeclaredNodeID] = true
			}
			if !seenDefinitions[instance.ModuleDefinitionID] {
				definitions = append(definitions, instance.ModuleDefinitionID)
				seenDefinitions[instance.ModuleDefinitionID] = true
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var nodes []models.HostNode
	if err := r.db.Where("node_id IN ?", ids).Find(&nodes).Error; err != nil {
		return err
	}
	byID := make(map[string]models.HostNode, len(nodes))
	for _, node := range nodes {
		byID[node.NodeID] = node
	}
	var modules []models.ModuleDefinition
	if err := r.db.Select("id", "module_name").Where("id IN ?", definitions).Find(&modules).Error; err != nil {
		return err
	}
	byDefinition := make(map[uint]string, len(modules))
	for _, module := range modules {
		byDefinition[module.ID] = module.ModuleName
	}
	for _, instance := range instances {
		if instance.DeclaredNodeID == "" {
			continue
		}
		instance.NodeBindingState = "rejected"
		node, exists := byID[instance.DeclaredNodeID]
		if !exists {
			instance.NodeBindingReason = "node_unknown"
			continue
		}
		if !node.Enabled {
			instance.NodeBindingReason = "node_disabled"
			continue
		}
		instance.NodeBindingReason = "source_not_allowed"
		for _, binding := range node.AllowedModuleBindings {
			if binding.ClientID == instance.RegistrationClientID && binding.ModuleName == byDefinition[instance.ModuleDefinitionID] && binding.ClientID == "addp-"+binding.ModuleName {
				instance.NodeID = node.NodeID
				instance.NodeBindingState = "bound"
				instance.NodeBindingReason = ""
				break
			}
		}
	}
	return nil
}
