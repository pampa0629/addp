package service

import (
	"context"
	"database/sql"
	"errors"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// GetDataItemAuthorizationFacts reads one committed Meta snapshot without source I/O.
func (s *MetadataQueryService) GetDataItemAuthorizationFacts(ctx context.Context, tenantID uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
	if tenantID == 0 || !authorizationfacts.ValidItemFingerprint(fingerprint) {
		return nil, commonapi.ErrBadRequest
	}
	var facts *authorizationfacts.AuthorizationFacts
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item models.MetaItem
		if err := tx.Where("tenant_id = ? AND fingerprint = ? AND deleted_at IS NULL", tenantID, fingerprint).First(&item).Error; err != nil {
			return err
		}
		facts = &authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1,
			ItemFingerprint: item.Fingerprint, EngineID: item.EngineID, ItemType: item.ItemType, FullName: item.FullName,
			ObservedAt: tx.NowFunc().UTC(), BasisState: authorizationfacts.AuthorizationBasisIncomplete}
		layout := commonjson.String(item.Attributes, "item", "layout")
		if item.ItemType == plugin.EngineCatalogTermFile || item.ItemType == plugin.EngineCatalogTermObject {
			return projectFileAuthorizationFacts(tx, item, facts)
		}
		if (item.ItemType != "table" && item.ItemType != "collection") || (layout != "" && layout != "single") {
			facts.BasisState = authorizationfacts.AuthorizationBasisUnsupported
			return facts.Validate()
		}
		if layout == "" || item.Name == "" {
			return facts.Validate()
		}
		var parent models.MetaNode
		err := tx.Where("tenant_id = ? AND engine_id = ? AND id = ? AND deleted_at IS NULL", tenantID, item.EngineID, item.NodeID).First(&parent).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return facts.Validate()
		}
		if err != nil {
			return err
		}
		if (parent.NodeType != plugin.EngineCatalogTermSchema && parent.NodeType != plugin.EngineCatalogTermDatabase) ||
			(item.ItemType == "collection" && parent.NodeType != plugin.EngineCatalogTermDatabase) {
			facts.BasisState = authorizationfacts.AuthorizationBasisUnsupported
			return facts.Validate()
		}
		if parent.Name == "" || parent.ParentNodeID == nil {
			return facts.Validate()
		}
		var root models.MetaNode
		err = tx.Where("tenant_id = ? AND engine_id = ? AND id = ? AND deleted_at IS NULL", tenantID, item.EngineID, *parent.ParentNodeID).First(&root).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return facts.Validate()
		}
		if err != nil {
			return err
		}
		if root.NodeType != plugin.EngineCatalogTermServer || root.ParentNodeID != nil || root.FullName != "" ||
			parent.FullName != parent.Name || item.FullName != parent.Name+"."+item.Name ||
			(item.ItemType == "table" && commonjson.String(item.Attributes, "storage", "schema_name") != parent.Name) {
			facts.BasisState = authorizationfacts.AuthorizationBasisNeedsRefresh
			return facts.Validate()
		}
		target := plugin.TabularItemPath(item.EngineID, parent.NodeType, parent.Name, item.Name)
		if item.ItemType == "collection" {
			target = plugin.EngineCatalogBranchLeafPath(plugin.DynamicSchemaCatalogModel(), item.EngineID, parent.NodeType, parent.Name, plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, item.Name)
		}
		facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: target}
		hash, err := facts.ComputeBoundaryHash()
		if err != nil {
			return err
		}
		facts.BoundaryHash, facts.BasisState = &hash, authorizationfacts.AuthorizationBasisComplete
		return facts.Validate()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return facts, nil
}
