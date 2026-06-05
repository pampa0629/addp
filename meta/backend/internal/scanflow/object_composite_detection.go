package scanflow

import (
	"context"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metacatalog"
)

type ObjectCatalogCompositeDetectionError struct {
	Bucket string
	Prefix string
	Err    error
}

func DetectObjectCatalogCompositeItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	resources []metacatalog.StorageResource,
	includeWholeScope bool,
) (map[string]bool, []metacatalog.ObjectCatalogCompositeItem, []ObjectCatalogCompositeDetectionError) {
	skipPaths := map[string]bool{}
	if contentReader == nil {
		return skipPaths, nil, nil
	}

	groups := metacatalog.ObjectResourcesByParentPrefix(resources)
	if includeWholeScope {
		for key, group := range metacatalog.ObjectResourcesByPartitionRootPrefix(resources) {
			groups[key] = append(groups[key], group...)
		}
	}
	items := make([]metacatalog.ObjectCatalogCompositeItem, 0)
	warnings := make([]ObjectCatalogCompositeDetectionError, 0)
	groupKeys := make([]string, 0, len(groups))
	for groupKey := range groups {
		groupKeys = append(groupKeys, groupKey)
	}
	sort.Slice(groupKeys, func(i, j int) bool {
		_, leftPrefix := metacatalog.SplitObjectCompositeGroupKey(groupKeys[i])
		_, rightPrefix := metacatalog.SplitObjectCompositeGroupKey(groupKeys[j])
		leftDepth := strings.Count(strings.Trim(leftPrefix, "/"), "/")
		rightDepth := strings.Count(strings.Trim(rightPrefix, "/"), "/")
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return groupKeys[i] < groupKeys[j]
	})

	for _, groupKey := range groupKeys {
		group := metacatalog.UnclaimedObjectResources(groups[groupKey], skipPaths)
		if len(group) < 2 {
			continue
		}
		bucket, prefix := metacatalog.SplitObjectCompositeGroupKey(groupKey)
		detection, err := ResolveContentCandidates(ctx, contentReader, connInfo, engineID, ContentCandidateSet{
			DirPath:        prefix,
			Files:          metacatalog.StorageResourcesToFileRefs(group),
			CatalogPathFor: plugin.ObjectItemPathForBucket(engineID, bucket),
		})
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
			if (!includeWholeScope || prefix == "") && detected.Layout != format.LayoutMulti {
				continue
			}
			for _, path := range detected.RefFilePaths() {
				skipPaths[metacatalog.ObjectPathFromClaim(bucket, path)] = true
			}
			acceptedAny = true
			items = append(items, metacatalog.ObjectCatalogCompositeItem{
				Bucket: bucket,
				Prefix: prefix,
				Item:   detected,
				Claims: detection.Claims,
			})
		}
		if includeWholeScope && acceptedAny {
			for path, claimed := range detection.Claims {
				if claimed {
					skipPaths[metacatalog.ObjectPathFromClaim(bucket, path)] = true
				}
			}
		}
	}
	return skipPaths, items, warnings
}
