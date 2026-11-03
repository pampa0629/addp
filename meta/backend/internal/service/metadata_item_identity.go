package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

var errMetadataItemIdentity = errors.New("incomplete metadata item identity")

// metadataItemTarget consumes a complete, tenant-bound persisted parent chain.
// No flattened name, caller locator or source enumeration supplies identity.
func metadataItemTarget(db *gorm.DB, item models.MetaItem) (plugin.EngineCatalogPath, error) {
	invalid := errMetadataItemIdentity
	if item.ID == 0 || item.EngineID == 0 || item.TenantID == 0 || item.Name == "" {
		return plugin.EngineCatalogPath{}, invalid
	}
	ancestors := []models.MetaNode{}
	seen := map[uint]bool{}
	id := item.NodeID
	for id != 0 {
		if seen[id] || len(ancestors) >= 63 {
			return plugin.EngineCatalogPath{}, invalid
		}
		seen[id] = true
		var n models.MetaNode
		if err := db.Select("id", "tenant_id", "engine_id", "parent_node_id", "name", "node_type", "full_name").Where("tenant_id=? AND engine_id=? AND id=? AND deleted_at IS NULL", item.TenantID, item.EngineID, id).First(&n).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return plugin.EngineCatalogPath{}, invalid
			}
			return plugin.EngineCatalogPath{}, err
		}
		ancestors = append(ancestors, n)
		if n.ParentNodeID == nil {
			break
		}
		id = *n.ParentNodeID
	}
	if len(ancestors) == 0 {
		return plugin.EngineCatalogPath{}, invalid
	}
	root := ancestors[len(ancestors)-1]
	p := plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: item.EngineID, Segments: []plugin.EngineCatalogSegment{{Term: root.NodeType, Kind: root.NodeType, Name: ""}}}
	if root.ParentNodeID != nil || root.FullName != "" || !plugin.IsEngineCatalogRootSegment(p.Segments[0]) {
		return plugin.EngineCatalogPath{}, invalid
	}
	names := []string{}
	sep := "."
	if item.ItemType == "file" || item.ItemType == "object" {
		sep = "/"
	}
	for i := len(ancestors) - 2; i >= 0; i-- {
		n := ancestors[i]
		if n.Name == "" {
			return plugin.EngineCatalogPath{}, invalid
		}
		names = append(names, n.Name)
		if n.NodeType == "directory" {
			return plugin.EngineCatalogPath{}, invalid
		} // Catalog terms are not persisted Meta node types.
		term := n.NodeType
		if term == "dir" {
			term = "directory"
		}
		kind := term
		if kind == "catalog" || kind == "schema" || kind == "database" {
			kind = "namespace"
		}
		if n.FullName != strings.Join(names, sep) {
			return plugin.EngineCatalogPath{}, invalid
		}
		p.Segments = append(p.Segments, plugin.EngineCatalogSegment{Term: term, Kind: kind, Name: n.Name})
	}
	whole := commonjson.String(item.Attributes, "item", "layout") == "whole"
	if whole && (ancestors[0].Name != item.Name || ancestors[0].FullName != item.FullName || !authorizationfacts.ValidMetadataScopeRoot(p)) {
		return plugin.EngineCatalogPath{}, invalid
	}
	leafKind := item.ItemType
	if item.ItemType == "table" {
		if declared := commonjson.String(item.Attributes, "type_info.table", "kind"); declared != "" {
			leafKind = declared
		}
	}
	if !whole {
		p.Segments = append(p.Segments, plugin.EngineCatalogSegment{Term: item.ItemType, Kind: leafKind, Name: item.Name})
	}
	kind, fullName, err := authorizationfacts.MetadataTargetIdentity(p)
	if err != nil || kind != item.ItemType || fullName != item.FullName {
		return plugin.EngineCatalogPath{}, invalid
	}
	return p, nil
}

func projectMetadataAuthorizationFacts(tx *gorm.DB, item models.MetaItem, facts *authorizationfacts.AuthorizationFacts) error {
	target, err := metadataItemTarget(tx, item)
	if err != nil {
		if errors.Is(err, errMetadataItemIdentity) {
			return facts.Validate()
		}
		return err
	}
	layout := commonjson.String(item.Attributes, "item", "layout")
	if (layout != "single" && layout != "multi" && layout != "whole") || ((item.ItemType != "file" && item.ItemType != "object") && layout != "single") {
		return facts.Validate()
	}
	identity := map[string]interface{}{}
	if attrs, ok := item.Attributes["item"].(map[string]interface{}); ok {
		for _, key := range []string{"refs", "scope_exclusive", "claim_policy"} {
			if v, found := attrs[key]; found {
				identity[key] = v
			}
		}
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(encoded)
	m := &authorizationfacts.MetadataBoundary{Target: target, Layout: layout, DataType: commonjson.String(item.Attributes, "item", "data_type"), Format: commonjson.String(item.Attributes, "item", "format"), CompositionDigest: hex.EncodeToString(sum[:])}
	facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: layout, MetadataTarget: m}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		return err
	}
	facts.BoundaryHash = &hash
	facts.BasisState = authorizationfacts.AuthorizationBasisComplete
	return facts.Validate()
}
