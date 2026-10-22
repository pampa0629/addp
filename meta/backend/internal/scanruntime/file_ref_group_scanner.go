package scanruntime

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

func scanFileRefGroups(
	ctx context.Context,
	runtime *FilesystemCatalogRuntime,
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	reporter scanflow.ProgressReporter,
) (scanflow.DispatchResult, error) {
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}
	catalogProvider, ok := enginePlugin.(plugin.EngineCatalogProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement EngineCatalogProvider", resource.EngineType)
	}
	itemTerm := scanflow.EngineCatalogLeafTermForPlugin(enginePlugin, plugin.EngineCatalogTermFile)
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	result := scanflow.DispatchResult{}
	seenNodes := map[uint]bool{}

	for i, group := range groups {
		primary := scanflow.ScanRefGroupPrimaryPath(group)
		if primary == "" {
			continue
		}
		candidates := scanflow.FileRefGroupCandidateSet(resource.ID, primary, group)
		parentDirPath := candidates.DirPath
		if len(candidates.Subdirs) > 1 {
			return result, fmt.Errorf("file ref group contains multiple scope directories: %s", primary)
		}
		// Refs define the boundary; storage facts must come from the engine,
		// just as they do for an exact leaf or a full directory scan.
		for j, file := range candidates.Files {
			entry, err := resolveRefGroupSource(ctx, catalogProvider, connInfo, file.EngineCatalogPath)
			if err != nil {
				return result, err
			}
			resolved, ok := scanresource.StorageFileRefFromEntry(*entry)
			if !ok || resolved.Path != file.Path {
				return result, fmt.Errorf("invalid content leaf facts for file ref group source %s", file.Path)
			}
			candidates.Files[j] = resolved
		}
		if len(candidates.Subdirs) == 1 {
			scope := candidates.Subdirs[0]
			recursiveFiles, recursiveSubdirs, err := runtime.listDirectoryRecursive(ctx, resource, catalogProvider, connInfo, scope.Path)
			if err != nil {
				return result, fmt.Errorf("failed to list file ref group scope %s: %w", scope.Path, err)
			}
			candidates.DirPath = scope.Path
			candidates.Files = append(candidates.Files, recursiveFiles...)
			candidates.Subdirs = recursiveSubdirs
			candidates.RecursiveFiles = candidates.Files
			candidates.RecursiveSubdirs = recursiveSubdirs
		}
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描内容引用组 %s", primary))
		}

		_, parentNode, err := runtime.ensureFilesystemScanRoot(tenantID, resource, enginePlugin, parentDirPath)
		if err != nil {
			return result, err
		}
		if parentNode != nil && !seenNodes[parentNode.ID] {
			seenNodes[parentNode.ID] = true
			result.CatalogNodes++
		}

		candidates.StructureReader = modelStructureReader(resource, contentReader, scanDepth)
		detection, err := scanflow.ResolveContentCandidates(ctx, scanflow.ContentReaderForDepth(contentReader, scanDepth), connInfo, resource.ID, candidates)
		if err != nil {
			return result, err
		}
		for _, detected := range detection.Items {
			persisted, fullName, itemExtractionStats, persistErr := runtime.persistFileCatalogDetectedItem(ctx, resource, tenantID, parentNode, candidates.DirPath, detected, candidates.Files, itemTerm, contentReader, connInfo, scanDepth)
			if persistErr != nil {
				return result, fmt.Errorf("failed to persist file ref group item %s: %w", fullName, persistErr)
			}
			if persisted {
				result.Items++
			}
			result.Extraction = scanflow.MergeExtractionCounts(result.Extraction, itemExtractionStats)
		}
		if reporter != nil {
			reporter.Advance(primary, i+1, len(groups), map[string]interface{}{"items": result.Items})
		}
	}
	return result, nil
}

func resolveRefGroupSource(ctx context.Context, catalogProvider plugin.EngineCatalogProvider, connInfo plugin.ConnectionInfo, expected plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	entry, err := catalogProvider.ResolvePath(ctx, connInfo, expected)
	if err != nil {
		return nil, fmt.Errorf("resolve ref group source %s: %w", expected.StringPath(), err)
	}
	if entry == nil {
		return nil, fmt.Errorf("ref group source %s does not exist", expected.StringPath())
	}
	leaf := expected.Segments[len(expected.Segments)-1]
	if entry.Role != plugin.EngineCatalogRoleLeaf || entry.Term != leaf.Term || entry.Kind != leaf.Kind || entry.Path.EngineID != expected.EngineID || entry.Path.StringPath() != expected.StringPath() || entry.Storage == nil || entry.Storage.SizeBytes == nil || *entry.Storage.SizeBytes < 0 {
		return nil, fmt.Errorf("invalid ref group source facts for %s", expected.StringPath())
	}
	return entry, nil
}
