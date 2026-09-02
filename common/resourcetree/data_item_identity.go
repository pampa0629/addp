package resourcetree

import (
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// DataItemIdentity is the stable identity projection derived from a validated
// Engine Catalog leaf. It intentionally contains no Meta row ID.
type DataItemIdentity struct {
	EngineID    uint
	ItemType    string
	FullName    string
	Fingerprint string
}

// DataItemIdentityFromCatalogPath converts one provider-owned catalog leaf to
// the same engine_id + full_name fingerprint used by Meta DataItems.
func DataItemIdentityFromCatalogPath(model plugin.EngineCatalogModelSpec, path plugin.EngineCatalogPath) (DataItemIdentity, error) {
	if path.Version != plugin.EngineCatalogPathVersion || path.EngineID == 0 {
		return DataItemIdentity{}, fmt.Errorf("data item identity requires a versioned catalog path and engine")
	}
	if len(path.Segments) < 2 || !plugin.IsEngineCatalogRootSegment(path.Segments[0]) || path.Segments[0].Term != model.RootTerm {
		return DataItemIdentity{}, fmt.Errorf("data item identity requires the model structural root")
	}
	leafLevel, ok := catalogLeafLevel(model)
	if !ok {
		return DataItemIdentity{}, fmt.Errorf("catalog model does not declare a leaf")
	}
	last := path.Segments[len(path.Segments)-1]
	if !segmentMatchesCatalogLevel(last, leafLevel) || strings.TrimSpace(last.Name) == "" {
		return DataItemIdentity{}, fmt.Errorf("catalog path does not identify the model leaf")
	}
	names := make([]string, 0, len(path.Segments)-1)
	for _, segment := range path.Segments[1:] {
		name := strings.TrimSpace(segment.Name)
		if name == "" {
			return DataItemIdentity{}, fmt.Errorf("catalog path contains an empty business segment")
		}
		names = append(names, name)
	}
	separator := "."
	if UsesSlashFullName("", leafLevel.Term) || UsesSlashFullName("", last.Kind) {
		separator = "/"
	}
	fullName := strings.Join(names, separator)
	if fullName == "" {
		return DataItemIdentity{}, fmt.Errorf("catalog leaf full_name is empty")
	}
	itemType := strings.TrimSpace(last.Kind)
	if itemType == "" {
		itemType = leafLevel.Term
	}
	return DataItemIdentity{
		EngineID: path.EngineID, ItemType: itemType, FullName: fullName,
		Fingerprint: models.GenerateItemFingerprint(path.EngineID, fullName),
	}, nil
}

func catalogLeafLevel(model plugin.EngineCatalogModelSpec) (plugin.EngineCatalogLevelSpec, bool) {
	for _, level := range model.Levels {
		if level.Role == plugin.EngineCatalogRoleLeaf {
			return level, true
		}
	}
	return plugin.EngineCatalogLevelSpec{}, false
}

func segmentMatchesCatalogLevel(segment plugin.EngineCatalogSegment, level plugin.EngineCatalogLevelSpec) bool {
	if segment.Term != level.Term {
		return false
	}
	if len(level.Kinds) == 0 {
		return segment.Kind == level.Term
	}
	for _, kind := range level.Kinds {
		if segment.Kind == kind {
			return true
		}
	}
	return false
}
