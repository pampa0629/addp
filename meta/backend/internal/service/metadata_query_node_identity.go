package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

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
	rootType, childType := "server", "schema"
	if target.Segments[0].Term == "service" {
		rootType, childType = "service", "bucket"
	}
	if target.Segments[0].Term == "root" {
		rootType, childType = "root", "dir"
	}
	var roots []models.MetaNode
	query := tx.Model(&models.MetaNode{}).Select("id,tenant_id,engine_id,node_type,parent_node_id,name,full_name").
		Where("tenant_id=? AND engine_id=? AND node_type=? AND parent_node_id IS NULL AND deleted_at IS NULL", tenantID, target.EngineID, rootType)
	if err := query.Limit(2).Find(&roots).Error; err != nil {
		return nil, 0, err
	}
	if len(roots) == 0 {
		return result, 0, nil
	}
	if len(roots) != 1 || roots[0].FullName != "" {
		return nil, 0, errors.New("ambiguous node root identity")
	}
	parent := roots[0].ID
	for i, segment := range target.Segments[1:] {
		if rootType == "service" && i > 0 {
			childType = "prefix"
		}
		var nodes []models.MetaNode
		if err := tx.Model(&models.MetaNode{}).Select("id,name,full_name").Where("tenant_id=? AND engine_id=? AND parent_node_id=? AND node_type=? AND name=? AND deleted_at IS NULL", tenantID, target.EngineID, parent, childType, segment.Name).Limit(2).Find(&nodes).Error; err != nil {
			return nil, 0, err
		}
		if len(nodes) == 0 {
			return result, 0, nil
		}
		names := []string{}
		for _, p := range target.Segments[1 : i+2] {
			names = append(names, p.Name)
		}
		expected := segment.Name
		if childType == "dir" || childType == "prefix" {
			expected = strings.Join(names, "/")
		}
		if len(nodes) != 1 || nodes[0].FullName != expected {
			return nil, 0, errors.New("ambiguous node identity")
		}
		parent = nodes[0].ID
		if childType == "dir" || childType == "prefix" || childType == "bucket" {
			var whole int64
			if err := tx.Model(&models.MetaItem{}).Where("tenant_id=? AND engine_id=? AND node_id=? AND attributes -> 'item' ->> 'layout'='whole' AND deleted_at IS NULL", tenantID, target.EngineID, parent).Count(&whole).Error; err != nil {
				return nil, 0, err
			}
			if whole > 0 {
				return result, 0, nil
			}
		}
	}
	result.Exists = true
	return result, parent, nil
}

// QueryNodeIdentities enumerates only direct owner nodes in one read-only
// snapshot. Whole-container ownership is checked internally, but item identities,
// technical attributes, item counts and source IO are never returned.
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
		if !parent.Exists || (input.Target.Segments[0].Term == "server" && len(input.Target.Segments) != 1) {
			return nil
		}
		childType := "schema"
		if input.Target.Segments[0].Term == "service" {
			childType = "bucket"
			if len(input.Target.Segments) > 1 {
				childType = "prefix"
			}
		}
		if input.Target.Segments[0].Term == "root" {
			childType = "dir"
		}
		query := tx.Model(&models.MetaNode{}).Where("tenant_id=? AND engine_id=? AND parent_node_id=? AND node_type=? AND deleted_at IS NULL", tenantID, input.Target.EngineID, id, childType)
		if childType == "dir" || childType == "prefix" || childType == "bucket" {
			query = query.Where("NOT EXISTS (SELECT 1 FROM meta.meta_item i WHERE i.tenant_id=meta.meta_node.tenant_id AND i.engine_id=meta.meta_node.engine_id AND i.node_id=meta.meta_node.id AND i.attributes -> 'item' ->> 'layout'='whole' AND i.deleted_at IS NULL)")
		}
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
			expected := node.Name
			if childType == "dir" || childType == "prefix" {
				expected = strings.TrimPrefix(input.Target.StringPath()+"/"+node.Name, "/")
			}
			if expected != node.FullName {
				return errors.New("inconsistent child node identity")
			}
			target := plugin.TabularNamespacePath(input.Target.EngineID, plugin.EngineCatalogTermSchema, node.Name)
			if childType == "dir" || childType == "prefix" {
				target = plugin.FileDirectoryPath(input.Target.EngineID, node.FullName)
			}
			if childType == "bucket" {
				target = plugin.ObjectDirectoryPath(input.Target.EngineID, node.Name, "")
			}
			if childType == "prefix" {
				target = plugin.ObjectDirectoryPath(input.Target.EngineID, input.Target.Segments[1].Name, strings.TrimPrefix(node.FullName, input.Target.Segments[1].Name+"/"))
			}
			result.Nodes = append(result.Nodes, authorizationfacts.NodeIdentity{TenantID: tenantID, Target: target, Exists: true})
		}
		return result.Validate(tenantID, input)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, result.Validate(tenantID, input)
}
