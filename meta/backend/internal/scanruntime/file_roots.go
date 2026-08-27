package scanruntime

import (
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanresource"
)

func (s *FilesystemCatalogRuntime) ensureFilesystemScanRoot(tenantID uint, resource *commonModels.Engine, enginePlugin plugin.EnginePlugin, scanPath string) (*models.MetaNode, *models.MetaNode, error) {
	rootNode, err := metaRepo.EnsureEngineCatalogRootNodeWithNativeName(s.repo, tenantID, resource, enginePlugin, "/")
	if err != nil {
		return nil, nil, err
	}
	scanPath = metapath.SanitizeFSPath(scanPath)
	if scanPath == "" {
		return rootNode, rootNode, nil
	}
	current := rootNode
	parts := strings.Split(scanPath, "/")
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		fullName := strings.Join(parts[:i+1], "/")
		node, err := s.repo.UpsertNode(
			tenantID,
			resource.ID,
			current,
			"dir",
			part,
			&fullName,
			scanresource.FileDirectoryNodeAttributes(fullName),
		)
		if err != nil {
			return rootNode, nil, err
		}
		current = node
	}
	return rootNode, current, nil
}
