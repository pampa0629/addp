package metaitem

import (
	"context"
	"fmt"
	"sort"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
)

var metaItemDetectors = []dataitem.CompositeItemDetector{
	&shapefileItemDetector{},
	&lakeTableItemDetector{},
}

func init() {
	if err := validateMetaItemDetectors(); err != nil {
		panic(err)
	}
}

func sortedMetaItemDetectors() []dataitem.CompositeItemDetector {
	result := make([]dataitem.CompositeItemDetector, len(metaItemDetectors))
	copy(result, metaItemDetectors)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority() > result[j].Priority()
	})
	return result
}

func validateMetaItemDetectors() error {
	for _, detector := range metaItemDetectors {
		if provider, ok := detector.(dataitem.FormatRulesProvider); ok {
			for _, rule := range provider.Rules() {
				if err := dataitem.ValidateFormatRule(rule); err != nil {
					return fmt.Errorf("invalid meta item detector rule: %w", err)
				}
			}
		}
		if provider, ok := detector.(dataitem.FormatRuleProvider); ok {
			if err := dataitem.ValidateFormatRule(provider.Rule()); err != nil {
				return fmt.Errorf("invalid meta item detector rule: %w", err)
			}
		}
	}
	return nil
}

// ResolveItems 使用 Meta 模块注册的 detector 从扫描范围内识别 0..N 个数据项。
func ResolveItems(ctx context.Context, input dataitem.DirectoryResolveInput) (*dataitem.DetectionResult, error) {
	result := &dataitem.DetectionResult{
		Items:  []*dataitem.DetectedItem{},
		Claims: dataitem.ResourceClaimSet{},
	}
	for _, detector := range sortedMetaItemDetectors() {
		detectorInput := input
		detectorInput.Files = unclaimedFileEntries(input.Files, result.Claims)
		detectorInput.RecursiveFiles = unclaimedFileEntries(input.RecursiveFiles, result.Claims)
		if len(input.Files) > 0 && len(detectorInput.Files) == 0 &&
			(len(input.RecursiveFiles) == 0 || len(detectorInput.RecursiveFiles) == 0) {
			break
		}

		if scoped, ok := detector.(dataitem.ScopeItemDetector); ok {
			scopeResult, err := scoped.ResolveItems(ctx, detectorInput)
			if err != nil {
				return nil, err
			}
			if scopeResult == nil {
				continue
			}
			for _, item := range scopeResult.Items {
				if item != nil {
					result.Items = append(result.Items, item)
				}
			}
			for path, claimed := range scopeResult.Claims {
				if claimed {
					result.Claims[path] = true
				}
			}
			if scopeResult.Exclusive {
				result.Exclusive = true
				return result, nil
			}
			continue
		}

		if !detector.Detect(ctx, detectorInput.Files, detectorInput.Subdirs) {
			continue
		}
		info, err := detector.ExtractItemInfo(
			ctx,
			detectorInput.ContentReader,
			detectorInput.ConnInfo,
			detectorInput.EngineID,
			detectorInput.DirPath,
			detectorInput.Files,
		)
		if err != nil {
			return nil, err
		}
		if info == nil {
			info = &dataitem.CompositeItemInfo{}
		}

		totalSize := sumFileEntrySize(detectorInput.Files)
		if info.SizeBytes != nil {
			totalSize = *info.SizeBytes
		}

		organization := info.Organization
		if organization == "" {
			organization = dataitem.OrganizationWhole
		}

		dataType := info.DataType
		if dataType == "" {
			dataType = dataitem.InferDataType(info.Format, "")
		}

		entryPath := info.EntryPath
		if entryPath == "" {
			entryPath = detectorInput.DirPath
		}

		componentFiles := info.ComponentFiles
		if len(componentFiles) == 0 {
			componentFiles = fileEntryPaths(detectorInput.Files)
		}

		item := &dataitem.DetectedItem{
			ItemType:       detector.ItemType(),
			Organization:   organization,
			DataType:       dataType,
			Format:         info.Format,
			PhysicalPath:   detectorInput.DirPath,
			EntryPath:      entryPath,
			ComponentFiles: componentFiles,
			SizeBytes:      totalSize,
			Fields:         info.Fields,
			Attributes:     info.Attributes,
		}
		result.Items = append(result.Items, item)
		for _, path := range componentFiles {
			result.Claims[path] = true
		}
		result.Exclusive = organization == dataitem.OrganizationWhole
		if result.Exclusive {
			return result, nil
		}
	}
	return result, nil
}

func ResolveNonExclusiveItems(ctx context.Context, input dataitem.DirectoryResolveInput) (*dataitem.DetectionResult, error) {
	result := &dataitem.DetectionResult{
		Items:  []*dataitem.DetectedItem{},
		Claims: dataitem.ResourceClaimSet{},
	}
	for _, detector := range sortedMetaItemDetectors() {
		scoped, ok := detector.(dataitem.ScopeItemDetector)
		if !ok {
			continue
		}
		scopeResult, err := scoped.ResolveItems(ctx, input)
		if err != nil {
			return nil, err
		}
		if scopeResult == nil || scopeResult.Exclusive {
			continue
		}
		for _, item := range scopeResult.Items {
			if item == nil || item.Organization == dataitem.OrganizationWhole {
				continue
			}
			result.Items = append(result.Items, item)
		}
		for path, claimed := range scopeResult.Claims {
			if claimed {
				result.Claims[path] = true
			}
		}
	}
	return result, nil
}

func unclaimedFileEntries(files []plugin.FileEntry, claims dataitem.ResourceClaimSet) []plugin.FileEntry {
	if len(files) == 0 || len(claims) == 0 {
		return files
	}
	filtered := make([]plugin.FileEntry, 0, len(files))
	for _, file := range files {
		if claims[file.Path] {
			continue
		}
		filtered = append(filtered, file)
	}
	return filtered
}

func sumFileEntrySize(files []plugin.FileEntry) int64 {
	var total int64
	for _, file := range files {
		total += file.Size
	}
	return total
}

func fileEntryPaths(files []plugin.FileEntry) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if file.Path != "" {
			paths = append(paths, file.Path)
		}
	}
	return paths
}
