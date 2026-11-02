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
	target := input.Target
	target.Segments = append([]plugin.EngineCatalogSegment(nil), target.Segments...)
	result := &authorizationfacts.NodeIdentity{TenantID: tenantID, Target: target}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var roots []models.MetaNode
		query := tx.Model(&models.MetaNode{}).Select("id,tenant_id,engine_id,node_type,parent_node_id,name,full_name").
			Where("tenant_id=? AND engine_id=? AND node_type=? AND parent_node_id IS NULL AND deleted_at IS NULL", tenantID, target.EngineID, plugin.EngineCatalogTermServer)
		if err := query.Limit(2).Find(&roots).Error; err != nil {
			return err
		}
		if len(roots) == 0 {
			return nil
		}
		if len(roots) != 1 || roots[0].FullName != "" {
			return errors.New("ambiguous node root identity")
		}
		if len(target.Segments) == 1 {
			result.Exists = true
			return nil
		}
		name := target.Segments[1].Name
		var nodes []models.MetaNode
		if err := tx.Model(&models.MetaNode{}).Select("id,name,full_name").
			Where("tenant_id=? AND engine_id=? AND parent_node_id=? AND node_type=? AND name=? AND deleted_at IS NULL", tenantID, target.EngineID, roots[0].ID, plugin.EngineCatalogTermSchema, name).
			Limit(2).Find(&nodes).Error; err != nil {
			return err
		}
		if len(nodes) == 0 {
			return nil
		}
		if len(nodes) != 1 || nodes[0].FullName != name {
			return errors.New("ambiguous schema node identity")
		}
		result.Exists = true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, result.Validate(tenantID, input)
}
