package metaitem

import (
	"context"
	"fmt"
	"sort"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
)

var itemResolvers = []ItemResolver{
	&commonDataItemResolver{},
}

func init() {
	if err := validateItemResolvers(); err != nil {
		panic(err)
	}
}

func RegisterResolver(resolver ItemResolver) {
	itemResolvers = append(itemResolvers, resolver)
	if err := validateItemResolver(resolver); err != nil {
		panic(err)
	}
}

func sortedItemResolvers() []ItemResolver {
	result := make([]ItemResolver, len(itemResolvers))
	copy(result, itemResolvers)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority() > result[j].Priority()
	})
	return result
}

func validateItemResolvers() error {
	for _, resolver := range itemResolvers {
		if err := validateItemResolver(resolver); err != nil {
			return err
		}
	}
	return nil
}

func validateItemResolver(resolver ItemResolver) error {
	if provider, ok := resolver.(FormatRulesProvider); ok {
		for _, rule := range provider.Rules() {
			if err := dataitem.ValidateFormatRule(rule); err != nil {
				return fmt.Errorf("invalid meta item resolver rule: %w", err)
			}
		}
	}
	if provider, ok := resolver.(FormatRuleProvider); ok {
		if err := dataitem.ValidateFormatRule(provider.Rule()); err != nil {
			return fmt.Errorf("invalid meta item resolver rule: %w", err)
		}
	}
	return nil
}

// ResolveItems 使用 Meta 模块注册的 resolver 从扫描范围内识别 0..N 个数据项。
func ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
	result := &DetectionResult{
		Items:  []*DetectedItem{},
		Claims: ResourceClaimSet{},
	}
	for _, resolver := range sortedItemResolvers() {
		resolverInput := input
		resolverInput.Files = unclaimedFileEntries(input.Files, result.Claims)
		resolverInput.RecursiveFiles = unclaimedFileEntries(input.RecursiveFiles, result.Claims)
		if len(input.Files) > 0 && len(resolverInput.Files) == 0 &&
			(len(input.RecursiveFiles) == 0 || len(resolverInput.RecursiveFiles) == 0) {
			break
		}

		if scoped, ok := resolver.(ScopeItemResolver); ok {
			scopeResult, err := scoped.ResolveItems(ctx, resolverInput)
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
	}
	return result, nil
}

func ResolveNonExclusiveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
	result := &DetectionResult{
		Items:  []*DetectedItem{},
		Claims: ResourceClaimSet{},
	}
	for _, resolver := range sortedItemResolvers() {
		scoped, ok := resolver.(ScopeItemResolver)
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
			if item == nil || item.Layout == dataitem.LayoutWhole {
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

func unclaimedFileEntries(files []plugin.FileEntry, claims ResourceClaimSet) []plugin.FileEntry {
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
