package service

import (
	"context"
	"database/sql"
	"errors"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// GetDataItemAuthorizationFacts reads one committed Meta snapshot without source I/O.
func (s *MetadataQueryService) GetDataItemAuthorizationFacts(ctx context.Context, tenantID uint, fingerprint string) (*dataitem.AuthorizationFacts, error) {
	if tenantID == 0 || !dataitem.ValidItemFingerprint(fingerprint) {
		return nil, commonapi.ErrBadRequest
	}
	var facts *dataitem.AuthorizationFacts
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item models.MetaItem
		if err := tx.Where("tenant_id = ? AND fingerprint = ? AND deleted_at IS NULL", tenantID, fingerprint).First(&item).Error; err != nil {
			return err
		}
		facts = &dataitem.AuthorizationFacts{SchemaVersion: dataitem.AuthorizationFactsSchemaV1,
			ItemFingerprint: item.Fingerprint, EngineID: item.EngineID, ItemType: item.ItemType, FullName: item.FullName,
			ObservedAt: tx.NowFunc().UTC(), BasisState: dataitem.AuthorizationBasisIncomplete}
		layout := commonjson.String(item.Attributes, "item", "layout")
		if item.ItemType != "table" || (layout != "" && layout != "single") {
			facts.BasisState = dataitem.AuthorizationBasisUnsupported
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
		if parent.NodeType != plugin.EngineCatalogTermSchema {
			facts.BasisState = dataitem.AuthorizationBasisUnsupported
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
			commonjson.String(item.Attributes, "storage", "schema_name") != parent.Name {
			facts.BasisState = dataitem.AuthorizationBasisNeedsRefresh
			return facts.Validate()
		}
		facts.Boundary = &dataitem.AuthorizationBoundary{Layout: "single", NativeTarget: plugin.TabularItemPath(item.EngineID, parent.NodeType, parent.Name, item.Name)}
		hash, err := facts.ComputeBoundaryHash()
		if err != nil {
			return err
		}
		facts.BoundaryHash, facts.BasisState = &hash, dataitem.AuthorizationBasisComplete
		return facts.Validate()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return facts, nil
}
