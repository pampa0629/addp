package service

import (
	"context"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

type MetadataVisibilityCheck func(context.Context, []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error)
type metadataVisibilityKey struct{}

// WithMetadataVisibility belongs only to the synchronous user query stack.
// Runtime facts and scan readers retain their separately guarded machine paths.
func WithMetadataVisibility(ctx context.Context, check MetadataVisibilityCheck) context.Context {
	return context.WithValue(ctx, metadataVisibilityKey{}, check)
}

func metadataVisibility(ctx context.Context) (MetadataVisibilityCheck, bool) {
	check, ok := ctx.Value(metadataVisibilityKey{}).(MetadataVisibilityCheck)
	return check, ok
}

func filterPGResourceTree(ctx context.Context, root *resourcetree.TreeNode, engineID uint, check MetadataVisibilityCheck) (*resourcetree.TreeNode, error) {
	if root == nil || check == nil {
		return nil, commonapi.ErrForbidden
	}
	targets := []plugin.EngineCatalogPath{}
	keys := map[string]string{}
	var collect func(*resourcetree.TreeNode) error
	collect = func(node *resourcetree.TreeNode) error {
		if node == nil {
			return nil
		}
		loc, err := resourcetree.ParseURI(node.Locator)
		if err != nil || loc.EngineID != engineID {
			return commonapi.ErrForbidden
		}
		if loc.ItemID != nil {
			if loc.Type != resourcetree.TypeTable {
				return nil
			}
			target, err := resourcetree.EngineCatalogPathFromLocator(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), loc)
			if err != nil {
				return commonapi.ErrForbidden
			}
			key, err := authorization.EncodeSharingTarget(target)
			if err != nil {
				return commonapi.ErrForbidden
			}
			keys[node.Locator] = string(key)
			targets = append(targets, target)
		}
		for _, child := range node.Children {
			if err := collect(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(root); err != nil {
		return nil, err
	}
	visible, err := observeMetadataVisibility(ctx, targets, check)
	if err != nil {
		return nil, err
	}
	var prune func(*resourcetree.TreeNode, bool) (*resourcetree.TreeNode, int)
	prune = func(node *resourcetree.TreeNode, isRoot bool) (*resourcetree.TreeNode, int) {
		if node == nil {
			return nil, 0
		}
		loc, _ := resourcetree.ParseURI(node.Locator)
		isItem := loc.ItemID != nil
		if isItem && !visible[keys[node.Locator]] {
			return nil, 0
		}
		children := []*resourcetree.TreeNode{}
		count := 0
		for _, child := range node.Children {
			safe, n := prune(child, false)
			if safe != nil {
				children = append(children, safe)
				count += n
			}
		}
		if !isRoot && !isItem && len(children) == 0 {
			return nil, 0
		}
		if isItem {
			count = 1
		}
		// Explicit navigation/identity projection. Never copy arbitrary attributes,
		// source statistics, samples, comments or a pre-filter item count.
		safe := &resourcetree.TreeNode{ID: node.ID, Locator: node.Locator, Label: node.Label, Type: node.Type, TypeLabel: node.TypeLabel, Icon: node.Icon, Children: children, HasChildren: len(children) > 0, Metadata: map[string]interface{}{}}
		for _, key := range []string{"engine_id", "engine_type", "engine_family", "node_id", "item_id", "full_name", "data_type", "format", "layout"} {
			if value, ok := node.Metadata[key]; ok {
				safe.Metadata[key] = value
			}
		}
		if !isItem {
			safe.Metadata["item_count"] = count
		}
		safe.Metadata["has_children"] = safe.HasChildren
		return safe, count
	}
	safe, _ := prune(root, true)
	return safe, nil
}

func limitVisibleTreeDepth(node *resourcetree.TreeNode, depth int) {
	if node == nil || depth < 0 {
		return
	}
	if depth == 0 {
		node.Children = []*resourcetree.TreeNode{}
		return
	}
	for _, child := range node.Children {
		limitVisibleTreeDepth(child, depth-1)
	}
}

func findVisibleTreePath(root *resourcetree.TreeNode, locator string) []*resourcetree.TreeNode {
	requested, err := resourcetree.ParseURI(locator)
	if err != nil {
		return nil
	}
	var find func(*resourcetree.TreeNode) []*resourcetree.TreeNode
	find = func(node *resourcetree.TreeNode) []*resourcetree.TreeNode {
		if node == nil {
			return nil
		}
		loc, err := resourcetree.ParseURI(node.Locator)
		if err != nil || loc.EngineID != requested.EngineID {
			return nil
		}
		matches := node.Locator == locator
		if requested.ItemID != nil {
			matches = loc.ItemID != nil && *requested.ItemID == *loc.ItemID
		} else if requested.NodeID != nil {
			matches = loc.NodeID != nil && *requested.NodeID == *loc.NodeID
		}
		if matches {
			return []*resourcetree.TreeNode{node}
		}
		for _, child := range node.Children {
			if tail := find(child); len(tail) > 0 {
				return append([]*resourcetree.TreeNode{node}, tail...)
			}
		}
		return nil
	}
	return find(root)
}

// Shared by tree, list and detail consumers. No partial result or cached Allow.
func observeMetadataVisibility(ctx context.Context, targets []plugin.EngineCatalogPath, check MetadataVisibilityCheck) (map[string]bool, error) {
	if check == nil {
		return nil, commonapi.ErrForbidden
	}
	visible := map[string]bool{}
	for offset := 0; offset < len(targets); offset += 200 {
		end := offset + 200
		if end > len(targets) {
			end = len(targets)
		}
		batch := targets[offset:end]
		result, err := check(ctx, batch)
		if err != nil {
			return nil, err
		}
		if !result.Matches(authorization.SourceMetadataCheckRequest{Targets: batch}) {
			return nil, commonapi.ErrForbidden
		}
		for _, item := range result.Targets {
			key, _ := authorization.EncodeSharingTarget(item.Target)
			visible[string(key)] = item.Visible
		}
	}
	return visible, nil
}
