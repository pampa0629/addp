package scanruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
)

// DirectLeafRuntime 扫描 root -> leaf catalog，并把 leaf 直接投影到结构 root 下。
type DirectLeafRuntime struct {
	log     *slog.Logger
	repo    *metaRepo.ScanRepository
	indexer scanprocessor.TechnicalMetadataIndexer
}

func NewDirectLeafRuntime(log *slog.Logger, repo *metaRepo.ScanRepository, indexer scanprocessor.TechnicalMetadataIndexer) *DirectLeafRuntime {
	return &DirectLeafRuntime{log: log, repo: repo, indexer: indexer}
}

func (s *DirectLeafRuntime) ScanRoot(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	scanDepth string,
	_ bool,
) (int, error) {
	return s.scan(ctx, enginePlugin, resource, tenantID, scanDepth, nil)
}

func (s *DirectLeafRuntime) ScanLeaf(ctx context.Context, p plugin.EnginePlugin, resource *commonModels.Engine, tenantID uint, entry plugin.EngineCatalogEntry, scanDepth string, _ bool) (int, int, int, error) {
	items, err := s.scan(ctx, p, resource, tenantID, scanDepth, &entry)
	return 0, items, 0, err
}

func (s *DirectLeafRuntime) scan(ctx context.Context, enginePlugin plugin.EnginePlugin, resource *commonModels.Engine, tenantID uint, scanDepth string, target *plugin.EngineCatalogEntry) (int, error) {
	if resource == nil {
		return 0, fmt.Errorf("scan resource is nil")
	}
	catalogProvider, ok := enginePlugin.(plugin.EngineCatalogProvider)
	if !ok {
		return 0, fmt.Errorf("engine %s does not implement EngineCatalogProvider", resource.EngineType)
	}
	model := scanflow.EngineCatalogModelForPlugin(enginePlugin)
	if model == nil || len(model.Levels) != 1 || model.Levels[0].Role != plugin.EngineCatalogRoleLeaf {
		return 0, fmt.Errorf("engine %s does not expose a direct leaf catalog model", resource.EngineType)
	}

	rootNode, err := metaRepo.EnsureEngineCatalogRootNode(s.repo, tenantID, resource, enginePlugin)
	if err != nil {
		return 0, err
	}
	if target == nil {
		if err := s.repo.ResetNodeState(rootNode, "running"); err != nil {
			return 0, err
		}
	}
	fail := func(scanErr error) (int, error) {
		if target == nil {
			_ = s.repo.FinalizeNodeState(rootNode, "failed", scanErr.Error())
		}
		return 0, scanErr
	}

	var entries []plugin.EngineCatalogEntry
	if target != nil {
		entries = []plugin.EngineCatalogEntry{*target}
	} else {
		entries, err = catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.EngineCatalogRootPath(*model, resource.ID), plugin.ListOptions{})
		if err != nil {
			return fail(fmt.Errorf("failed to list direct catalog leaves: %w", err))
		}
	}

	keepFingerprints := make([]string, 0, len(entries))
	failures := &scanflow.FailedTargetCollector{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if entry.Role != plugin.EngineCatalogRoleLeaf {
			continue
		}
		identity, identityErr := resourcetree.DataItemIdentityFromCatalogPath(*model, entry.Path)
		if identityErr != nil {
			failures.Add(entry.Name, fmt.Errorf("invalid direct catalog leaf identity: %w", identityErr))
			continue
		}
		if strings.TrimSpace(entry.Name) == "" {
			failures.Add(entry.Name, fmt.Errorf("direct catalog leaf has incomplete identity"))
			continue
		}
		attrs := models.JSONMap(metaattr.BuildAttributes(metaattr.DataItemAttributesInput{
			Layout:   "single",
			DataType: datatype.Unknown,
		}))
		var rowCount, sizeBytes *int64
		updatedAt := entry.UpdatedAt
		if scanDepth != models.ScannedDepthDeep {
			existing, found, err := s.repo.FindItemByFullName(tenantID, resource.ID, identity.FullName)
			if err != nil {
				failures.Add(entry.Name, err)
				continue
			}
			if found {
				attrs = metaattr.JSONMap(cloneJSONMap(existing.Attributes))
				rowCount, sizeBytes = existing.RowCount, existing.SizeBytes
				if updatedAt == nil {
					updatedAt = existing.DataUpdatedAt
				}
			}
			if entry.Table != nil {
				if entry.Table.RowCount != nil {
					rowCount = entry.Table.RowCount
				}
				if entry.Table.SizeBytes != nil {
					sizeBytes = entry.Table.SizeBytes
				}
			}
			if entry.Storage != nil && entry.Storage.SizeBytes != nil {
				sizeBytes = entry.Storage.SizeBytes
			}
		}
		if identity.ItemType == plugin.EngineCatalogTermGraph {
			metaattr.ApplyGraphItemAttributes(attrs, nil)
		}
		if factsProvider, ok := enginePlugin.(plugin.EngineCatalogFactsProvider); ok && scanDepth == models.ScannedDepthDeep {
			facts, factsErr := factsProvider.DescribeEngineCatalogFacts(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), entry.Path, plugin.EngineCatalogFactsOptions{IncludeStatistics: scanDepth == models.ScannedDepthDeep})
			if factsErr != nil {
				failures.Add(entry.Name, fmt.Errorf("failed to describe direct catalog leaf: %w", factsErr))
				continue
			}
			if table := plugin.EngineCatalogFactsTableInfo(facts); table != nil {
				metaattr.ApplyTableItemAttributes(attrs, table)
				rowCount, sizeBytes = table.RowCount, table.SizeBytes
			}
			if graph := plugin.EngineCatalogFactsGraphInfo(facts); graph != nil {
				metaattr.ApplyGraphItemAttributes(attrs, graph)
			}
			if facts != nil && facts.Keyspace != nil {
				metaattr.SetItem(attrs, "data_type", string(datatype.KeyValue))
			}
			metaattr.ApplyEngineCatalogFactsCapabilities(attrs, facts)
		}
		item, err := s.repo.UpsertItemWithDepth(
			tenantID,
			resource.ID,
			rootNode,
			identity.ItemType,
			entry.Name,
			identity.FullName,
			attrs,
			rowCount,
			sizeBytes,
			updatedAt,
			scanDepth,
		)
		if err != nil {
			failures.Add(entry.Name, fmt.Errorf("failed to save direct catalog leaf: %w", err))
			continue
		}
		keepFingerprints = append(keepFingerprints, item.Fingerprint)
		if s.indexer != nil {
			s.indexer.IndexTechnicalMetadata(ctx, resource, tenantID, rootNode, item)
		}
	}

	if err := failures.Err(); target == nil && err == nil {
		existing, err := s.repo.GetItemsByNode(rootNode.ID)
		if err != nil {
			failures.Add(resource.Name, err)
		} else if err := s.repo.SoftDeleteItemsNotInList(rootNode.ID, keepFingerprints); err != nil {
			failures.Add(resource.Name, fmt.Errorf("failed to delete missing direct catalog leaves: %w", err))
		} else if s.indexer != nil {
			keep := make(map[string]bool, len(keepFingerprints))
			for _, fingerprint := range keepFingerprints {
				keep[fingerprint] = true
			}
			for _, item := range existing {
				if !keep[item.Fingerprint] {
					_ = s.indexer.DeleteItemsFromIndex(ctx, tenantID, resource.ID, []string{item.Fingerprint})
				}
			}
		}
	}
	if scanErr := failures.Err(); scanErr != nil {
		if target == nil {
			_ = s.repo.FinalizeNodeState(rootNode, "failed", scanErr.Error())
		}
		return len(keepFingerprints), scanErr
	}
	if target == nil {
		if err := s.repo.FinalizeNodeStateWithDepth(rootNode, "completed", "", scanDepth); err != nil {
			return 0, err
		}
	}
	s.log.Info("direct catalog leaf 扫描完成",
		"engine_id", resource.ID,
		"tenant_id", tenantID,
		"items_scanned", len(keepFingerprints),
	)
	return len(keepFingerprints), nil
}
