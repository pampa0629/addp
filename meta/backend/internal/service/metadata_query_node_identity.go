package service

import (
	"context"
	"database/sql"
	"errors"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// ResolveNodeIdentity reads only node identity columns in one committed snapshot.
// It does not query an engine, read item attributes or make a permission decision.
func (s *MetadataQueryService) ResolveNodeIdentity(ctx context.Context, tenantID uint, input authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
	if tenantID == 0 || input.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	var result *authorizationfacts.NodeIdentity
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, _, err = resolveNodeIdentitySnapshot(tx, tenantID, input)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, result.Validate(tenantID, input)
}

func resolveNodeIdentitySnapshot(tx *gorm.DB, tenantID uint, input authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, uint, error) {
	target := input.Target
	target.Segments = append([]plugin.EngineCatalogSegment(nil), target.Segments...)
	result := &authorizationfacts.NodeIdentity{TenantID: tenantID, Target: target}
	var roots []models.MetaNode
	query := tx.Model(&models.MetaNode{}).Select("id,tenant_id,engine_id,node_type,parent_node_id,name,full_name").
		Where("tenant_id=? AND engine_id=? AND node_type=? AND parent_node_id IS NULL AND deleted_at IS NULL", tenantID, target.EngineID, plugin.EngineCatalogTermServer)
	if err := query.Limit(2).Find(&roots).Error; err != nil {
		return nil, 0, err
	}
	if len(roots) == 0 {
		return result, 0, nil
	}
	if len(roots) != 1 || roots[0].FullName != "" {
		return nil, 0, errors.New("ambiguous node root identity")
	}
	if len(target.Segments) == 1 {
		result.Exists = true
		return result, roots[0].ID, nil
	}
	name := target.Segments[1].Name
	var nodes []models.MetaNode
	if err := tx.Model(&models.MetaNode{}).Select("id,name,full_name").
		Where("tenant_id=? AND engine_id=? AND parent_node_id=? AND node_type=? AND name=? AND deleted_at IS NULL", tenantID, target.EngineID, roots[0].ID, plugin.EngineCatalogTermSchema, name).
		Limit(2).Find(&nodes).Error; err != nil {
		return nil, 0, err
	}
	if len(nodes) == 0 {
		return result, 0, nil
	}
	if len(nodes) != 1 || nodes[0].FullName != name {
		return nil, 0, errors.New("ambiguous schema node identity")
	}
	result.Exists = true
	return result, nodes[0].ID, nil
}

// QueryNodeIdentities enumerates only direct owner nodes in one read-only
// snapshot. No MetaItem, technical attributes, count of items or source IO.
func (s *MetadataQueryService) QueryNodeIdentities(ctx context.Context, tenantID uint, input authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error) {
	if tenantID == 0 || input.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	result := &authorizationfacts.NodeIdentityPage{Page: input.Page, PageSize: input.PageSize, Nodes: []authorizationfacts.NodeIdentity{}}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		parent, id, err := resolveNodeIdentitySnapshot(tx, tenantID, authorizationfacts.NodeIdentityQuery{Target: input.Target})
		if err != nil {
			return err
		}
		result.Parent = *parent
		if !parent.Exists || len(input.Target.Segments) != 1 {
			return nil
		}
		query := tx.Model(&models.MetaNode{}).Where("tenant_id=? AND engine_id=? AND parent_node_id=? AND node_type=? AND deleted_at IS NULL", tenantID, input.Target.EngineID, id, plugin.EngineCatalogTermSchema)
		var total int64
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		result.Total = int(total)
		var nodes []models.MetaNode
		if err := query.Select("id,name,full_name").Order("name ASC,id ASC").Limit(input.PageSize).Offset((input.Page - 1) * input.PageSize).Find(&nodes).Error; err != nil {
			return err
		}
		for _, node := range nodes {
			if node.Name != node.FullName {
				return errors.New("inconsistent child node identity")
			}
			result.Nodes = append(result.Nodes, authorizationfacts.NodeIdentity{TenantID: tenantID, Target: plugin.TabularNamespacePath(input.Target.EngineID, plugin.EngineCatalogTermSchema, node.Name), Exists: true})
		}
		return result.Validate(tenantID, input)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, result.Validate(tenantID, input)
}
