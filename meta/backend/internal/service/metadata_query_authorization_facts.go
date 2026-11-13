package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonjson "github.com/addp/common/jsonmap"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// Single-item internal consumers use the same committed batch projection.
func (s *MetadataQueryService) GetDataItemAuthorizationFacts(ctx context.Context, tenantID uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
	batch, err := s.QueryDataItemAuthorizationFacts(ctx, tenantID, authorizationfacts.AuthorizationFactsQuery{Fingerprints: []string{fingerprint}})
	if err != nil {
		return nil, err
	}
	if batch.Data[0].Facts == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return batch.Data[0].Facts, nil
}

func projectDataItemAuthorizationFacts(item models.MetaItem, observedAt time.Time, lookup metadataNodeLookup) (*authorizationfacts.AuthorizationFacts, error) {
	facts := &authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1,
		ItemFingerprint: item.Fingerprint, EngineID: item.EngineID, ItemType: item.ItemType, FullName: item.FullName,
		ObservedAt: observedAt, BasisState: authorizationfacts.AuthorizationBasisIncomplete}
	err := func() error {
		layout := commonjson.String(item.Attributes, "item", "layout")
		if item.ItemType == plugin.EngineCatalogTermFile || item.ItemType == plugin.EngineCatalogTermObject {
			if err := projectFileAuthorizationFacts(lookup, item, facts); err != nil {
				return err
			}
			if facts.BasisState != authorizationfacts.AuthorizationBasisComplete {
				return projectMetadataAuthorizationFacts(lookup, item, facts)
			}
			return nil
		}
		tableKind := commonjson.String(item.Attributes, "type_info.table", "kind")
		if (item.ItemType == "table" && tableKind != "" && tableKind != "table") || (item.ItemType != "table" && item.ItemType != "collection") || (layout != "" && layout != "single") {
			facts.BasisState = authorizationfacts.AuthorizationBasisUnsupported
			return projectMetadataAuthorizationFacts(lookup, item, facts)
		}
		if layout == "" || item.Name == "" {
			return projectMetadataAuthorizationFacts(lookup, item, facts)
		}
		parent, err := lookup(item.TenantID, item.EngineID, item.NodeID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return projectMetadataAuthorizationFacts(lookup, item, facts)
		}
		if err != nil {
			return err
		}
		if (parent.NodeType != plugin.EngineCatalogTermSchema && parent.NodeType != plugin.EngineCatalogTermDatabase) ||
			(item.ItemType == "collection" && parent.NodeType != plugin.EngineCatalogTermDatabase) {
			facts.BasisState = authorizationfacts.AuthorizationBasisUnsupported
			return projectMetadataAuthorizationFacts(lookup, item, facts)
		}
		if parent.Name == "" || parent.ParentNodeID == nil {
			return projectMetadataAuthorizationFacts(lookup, item, facts)
		}
		root, err := lookup(item.TenantID, item.EngineID, *parent.ParentNodeID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return projectMetadataAuthorizationFacts(lookup, item, facts)
		}
		if err != nil {
			return err
		}
		if root.NodeType != plugin.EngineCatalogTermServer || root.ParentNodeID != nil || root.FullName != "" ||
			parent.FullName != parent.Name || item.FullName != parent.Name+"."+item.Name ||
			(item.ItemType == "table" && commonjson.String(item.Attributes, "storage", "schema_name") != parent.Name) {
			facts.BasisState = authorizationfacts.AuthorizationBasisNeedsRefresh
			return projectMetadataAuthorizationFacts(lookup, item, facts)
		}
		target := plugin.TabularItemPath(item.EngineID, parent.NodeType, parent.Name, item.Name)
		if item.ItemType == "collection" {
			target = plugin.EngineCatalogBranchLeafPath(plugin.DynamicSchemaCatalogModel(), item.EngineID, parent.NodeType, parent.Name, plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, item.Name)
		}
		if item.ItemType == "table" && parent.NodeType == plugin.EngineCatalogTermSchema {
			binding, err := authorizationfacts.NativeTableBindingFromMeta(commonmodels.MetaItem{ID: item.ID, NodeID: item.NodeID, TenantID: item.TenantID, EngineID: item.EngineID, Name: item.Name, FullName: item.FullName, ItemType: item.ItemType, Fingerprint: item.Fingerprint, Attributes: item.Attributes}, target)
			if err != nil {
				facts.BasisState = authorizationfacts.AuthorizationBasisNeedsRefresh
				return projectMetadataAuthorizationFacts(lookup, item, facts)
			}
			facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: binding.NativeTarget}
		} else {
			facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: target}
		}
		hash, err := facts.ComputeBoundaryHash()
		if err != nil {
			return err
		}
		facts.BoundaryHash, facts.BasisState = &hash, authorizationfacts.AuthorizationBasisComplete
		return facts.Validate()
	}()
	if err != nil {
		return nil, err
	}
	return facts, nil
}
