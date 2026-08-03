package scanruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
)

// DirectLeafRuntime 扫描 root -> leaf catalog，并把 leaf 直接投影到结构 root 下。
type DirectLeafRuntime struct {
	log  *slog.Logger
	repo *metaRepo.ScanRepository
}

func NewDirectLeafRuntime(log *slog.Logger, repo *metaRepo.ScanRepository) *DirectLeafRuntime {
	return &DirectLeafRuntime{log: log, repo: repo}
}

func (s *DirectLeafRuntime) ScanRoot(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	scanDepth string,
	_ bool,
) (int, error) {
	if resource == nil {
		return 0, fmt.Errorf("scan resource is nil")
	}
	catalogProvider, ok := enginePlugin.(plugin.CatalogProvider)
	if !ok {
		return 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	model := scanflow.CatalogModelForPlugin(enginePlugin)
	if model == nil || len(model.Levels) != 1 || model.Levels[0].Role != plugin.CatalogRoleLeaf {
		return 0, fmt.Errorf("engine %s does not expose a direct leaf catalog model", resource.EngineType)
	}

	rootNode, err := metaRepo.EnsureCatalogRootNode(s.repo, tenantID, resource, enginePlugin)
	if err != nil {
		return 0, err
	}
	if err := s.repo.ResetNodeState(rootNode, "running"); err != nil {
		return 0, err
	}
	fail := func(scanErr error) (int, error) {
		_ = s.repo.FinalizeNodeState(rootNode, "pending", 0, 0, scanErr.Error())
		return 0, scanErr
	}

	entries, err := catalogProvider.ListChildren(
		ctx,
		plugin.ConnectionInfo(resource.ConnectionInfo),
		plugin.CatalogRootPath(*model, resource.ID),
		plugin.ListOptions{},
	)
	if err != nil {
		return fail(fmt.Errorf("failed to list direct catalog leaves: %w", err))
	}

	keepFingerprints := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Role != plugin.CatalogRoleLeaf {
			continue
		}
		itemType := catalogLeafItemType(entry)
		fullName := entry.Path.StringPath()
		if itemType == "" || strings.TrimSpace(entry.Name) == "" || strings.TrimSpace(fullName) == "" {
			return fail(fmt.Errorf("direct catalog leaf %q has incomplete identity", entry.Name))
		}
		attrs := models.JSONMap(metaattr.BuildAttributes(metaattr.DataItemAttributesInput{
			Layout:   "single",
			DataType: datatype.Unknown,
		}))
		item, err := s.repo.UpsertItemWithDepth(
			tenantID,
			resource.ID,
			rootNode,
			itemType,
			entry.Name,
			fullName,
			attrs,
			nil,
			nil,
			entry.UpdatedAt,
			scanDepth,
		)
		if err != nil {
			return fail(fmt.Errorf("failed to save direct catalog leaf %q: %w", entry.Name, err))
		}
		keepFingerprints = append(keepFingerprints, item.Fingerprint)
	}

	if err := s.repo.SoftDeleteItemsNotInList(rootNode.ID, keepFingerprints); err != nil {
		return fail(fmt.Errorf("failed to delete missing direct catalog leaves: %w", err))
	}
	if err := s.repo.FinalizeNodeStateWithDepth(rootNode, "completed", len(keepFingerprints), 0, "", scanDepth); err != nil {
		return 0, err
	}
	s.log.Info("direct catalog leaf 扫描完成",
		"engine_id", resource.ID,
		"tenant_id", tenantID,
		"items_scanned", len(keepFingerprints),
	)
	return len(keepFingerprints), nil
}
