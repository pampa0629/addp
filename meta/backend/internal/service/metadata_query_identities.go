package service

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// QueryDataItemIdentities reads only committed identity columns. Count and page
// share one owner snapshot; no source engine, scan, or authorization decision.
func (s *MetadataQueryService) QueryDataItemIdentities(ctx context.Context, tenantID uint, input authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error) {
	if tenantID == 0 || input.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	result := &authorizationfacts.IdentityPage{Data: []authorizationfacts.DataItemIdentity{}, Page: input.Page, PageSize: input.PageSize}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.MetaItem{}).Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL", tenantID, input.EngineID)
		if input.Search != "" {
			// Literal name/path search: wildcard characters are not user SQL.
			pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(input.Search)) + "%"
			query = query.Where("(LOWER(name) LIKE ? ESCAPE '!' OR LOWER(full_name) LIKE ? ESCAPE '!')", pattern, pattern)
		}
		switch input.FingerprintFilter {
		case "include":
			if len(input.Fingerprints) == 0 {
				query = query.Where("1 = 0")
			} else {
				query = query.Where("fingerprint IN ?", input.Fingerprints)
			}
		case "exclude":
			if len(input.Fingerprints) > 0 {
				query = query.Where("fingerprint NOT IN ?", input.Fingerprints)
			}
		}
		if err := query.Count(&result.Total).Error; err != nil {
			return err
		}
		if err := query.Select("fingerprint AS item_fingerprint, engine_id, name, full_name, item_type").Order("fingerprint ASC").
			Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Scan(&result.Data).Error; err != nil {
			return err
		}
		if err := identityAncestors(tx, tenantID, input.EngineID, result.Data); err != nil {
			return err
		}
		result.TotalPages = result.Total / int64(input.PageSize)
		if result.Total%int64(input.PageSize) != 0 {
			result.TotalPages++
		}
		return result.Validate(input)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

// Batch each ancestry level, rather than issue an RPC or source query per item.
// Every edge is resolved within the same tenant/engine/repeatable-read snapshot.
func identityAncestors(tx *gorm.DB, tenantID, engineID uint, items []authorizationfacts.DataItemIdentity) error {
	if len(items) == 0 {
		return nil
	}
	fingerprints := make([]string, len(items))
	for i, item := range items {
		fingerprints[i] = item.ItemFingerprint
	}
	var leaves []struct {
		Fingerprint string
		NodeID      uint
	}
	if err := tx.Model(&models.MetaItem{}).Select("fingerprint,node_id").Where("tenant_id=? AND engine_id=? AND fingerprint IN ?", tenantID, engineID, fingerprints).Scan(&leaves).Error; err != nil {
		return err
	}
	parents := make(map[string]uint, len(leaves))
	pending := make(map[uint]bool)
	for _, leaf := range leaves {
		parents[leaf.Fingerprint] = leaf.NodeID
		pending[leaf.NodeID] = true
	}
	nodes := make(map[uint]models.MetaNode)
	for depth := 0; len(pending) > 0; depth++ {
		if depth >= 128 {
			return fmt.Errorf("DataItem ancestry exceeds depth limit")
		}
		ids := make([]uint, 0, len(pending))
		for id := range pending {
			ids = append(ids, id)
		}
		var found []models.MetaNode
		if err := tx.Select("id,parent_node_id,name,node_type").Where("tenant_id=? AND engine_id=? AND id IN ?", tenantID, engineID, ids).Find(&found).Error; err != nil {
			return err
		}
		if len(found) != len(ids) {
			return fmt.Errorf("incomplete DataItem ancestry")
		}
		for _, node := range found {
			nodes[node.ID] = node
		}
		pending = make(map[uint]bool)
		for _, node := range found {
			if node.ParentNodeID != nil {
				if _, exists := nodes[*node.ParentNodeID]; !exists {
					pending[*node.ParentNodeID] = true
				}
			}
		}
	}
	for i := range items {
		id := parents[items[i].ItemFingerprint]
		seen := make(map[uint]bool)
		chain := []authorizationfacts.IdentityNode{}
		for {
			node, ok := nodes[id]
			if !ok || seen[id] || len(chain) >= 128 {
				return fmt.Errorf("invalid DataItem ancestry")
			}
			seen[id] = true
			chain = append(chain, authorizationfacts.IdentityNode{ID: node.ID, Name: node.Name, NodeType: node.NodeType})
			if node.ParentNodeID == nil {
				break
			}
			id = *node.ParentNodeID
		}
		slices.Reverse(chain)
		items[i].Ancestors = chain
	}
	return nil
}
