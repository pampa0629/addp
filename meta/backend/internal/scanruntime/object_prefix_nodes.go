package scanruntime

import (
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func (s *ObjectStorageCatalogRuntime) ensureObjectCatalogPrefixNodes(
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	parentPath string,
	scanPathPrefix string,
	stats map[uint]*scanflow.ObjectCatalogNodeAggregate,
) (*models.MetaNode, error) {
	parent := bucketNode
	if basePrefixNode != nil {
		parent = basePrefixNode
	}
	parentNode, createdNodes, err := s.repo.EnsureObjectCatalogPrefixRelativePath(tenantID, engineID, bucketNode, parent, parentPath, scanPathPrefix)
	if err != nil {
		return nil, err
	}
	for _, node := range createdNodes {
		scanflow.EnsureObjectCatalogNodeAggregate(stats, node)
	}
	return parentNode, nil
}
