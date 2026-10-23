package scanflow

import (
	"context"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/scanresource"
)

type ObjectCatalogCompositeDetectionError struct {
	Bucket string
	Prefix string
	Err    error
}

func DetectObjectCatalogCompositeItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	structureReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	resources []scanresource.StorageResource,
	deepScan bool,
) (map[string]bool, []scanresource.ObjectCompositeItem, []ObjectCatalogCompositeDetectionError) {
	skipPaths := map[string]bool{}
	if !deepScan {
		contentReader = nil
	}

	groups := scanresource.ObjectResourcesByParentPrefix(resources)
	for key, group := range scanresource.ObjectResourcesByPartitionRootPrefix(resources) {
		groups[key] = append(groups[key], group...)
	}
	items := make([]scanresource.ObjectCompositeItem, 0)
	warnings := make([]ObjectCatalogCompositeDetectionError, 0)
	rasterMosaicItems, rasterMosaicWarnings := detectRasterMosaicCompositeItems(ctx, contentReader, connInfo, engineID, resources, skipPaths)
	items = append(items, rasterMosaicItems...)
	warnings = append(warnings, rasterMosaicWarnings...)
	groupKeys := make([]string, 0, len(groups))
	for groupKey := range groups {
		groupKeys = append(groupKeys, groupKey)
	}
	sort.Slice(groupKeys, func(i, j int) bool {
		_, leftPrefix := scanresource.SplitObjectCompositeGroupKey(groupKeys[i])
		_, rightPrefix := scanresource.SplitObjectCompositeGroupKey(groupKeys[j])
		leftDepth := strings.Count(strings.Trim(leftPrefix, "/"), "/")
		rightDepth := strings.Count(strings.Trim(rightPrefix, "/"), "/")
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return groupKeys[i] < groupKeys[j]
	})

	for _, groupKey := range groupKeys {
		group := scanresource.UnclaimedObjectResources(groups[groupKey], skipPaths)
		if len(group) == 0 || (len(group) < 2 && structureReader == nil) {
			continue
		}
		bucket, prefix := scanresource.SplitObjectCompositeGroupKey(groupKey)
		candidates := ContentCandidateSet{
			DirPath:              prefix,
			StructureReader:      structureReader,
			Files:                scanresource.StorageResourcesToFileRefs(scanresource.ObjectResourcesByParentPrefix(group)[groupKey]),
			RecursiveFiles:       scanresource.StorageResourcesToFileRefs(group),
			EngineCatalogPathFor: plugin.ObjectItemPathForBucket(engineID, bucket),
		}
		resolve := ResolveContentCandidates
		if prefix == "" {
			resolve = ResolveNonExclusiveContentCandidates
		}
		detection, err := resolve(ctx, contentReader, connInfo, engineID, candidates)
		if err != nil {
			warnings = append(warnings, ObjectCatalogCompositeDetectionError{
				Bucket: bucket,
				Prefix: prefix,
				Err:    err,
			})
			continue
		}
		if detection == nil || len(detection.Items) == 0 {
			continue
		}
		acceptedAny := false
		for _, detected := range detection.Items {
			if detected == nil {
				continue
			}
			if prefix == "" && detected.Layout != format.LayoutMulti && !(detected.Layout == format.LayoutSingle && (detected.Format == "3ds" || detected.Format == "dae" || detected.Format == "obj") && structureReader != nil) {
				continue
			}
			for _, path := range detected.RefFilePaths() {
				skipPaths[strings.Trim(path, "/")] = true
			}
			acceptedAny = true
			items = append(items, scanresource.ObjectCompositeItem{
				Bucket: bucket,
				Prefix: prefix,
				Item:   detected,
				Claims: detection.Claims,
			})
		}
		if acceptedAny {
			for path, claimed := range detection.Claims {
				if claimed {
					skipPaths[strings.Trim(path, "/")] = true
				}
			}
		}
	}
	return skipPaths, items, warnings
}
