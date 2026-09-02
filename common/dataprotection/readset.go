package dataprotection

import (
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

// DataItemTargetsFromQueryReadSet converts provider-owned Engine Catalog leaves
// to the professional resource identity used by DataItem protection projections.
func DataItemTargetsFromQueryReadSet(
	model plugin.EngineCatalogModelSpec,
	readSet *plugin.QueryReadSet,
) ([]ResourceReference, error) {
	if readSet == nil {
		return nil, fmt.Errorf("query read set is required")
	}
	targets := make([]ResourceReference, 0, len(readSet.Paths))
	for _, path := range readSet.Paths {
		target, err := DataItemTargetFromCatalogPath(model, path)
		if err != nil {
			return nil, fmt.Errorf("resolve query read DataItem identity: %w", err)
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// DataItemTargetFromCatalogPath converts one validated provider-owned leaf to
// the stable professional resource reference used by protection projections.
func DataItemTargetFromCatalogPath(model plugin.EngineCatalogModelSpec, path plugin.EngineCatalogPath) (ResourceReference, error) {
	identity, err := resourcetree.DataItemIdentityFromCatalogPath(model, path)
	if err != nil {
		return ResourceReference{}, err
	}
	return ResourceReference{
		OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity.Fingerprint,
	}, nil
}
