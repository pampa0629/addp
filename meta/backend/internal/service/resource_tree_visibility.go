package service

import (
	"context"
	"errors"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
	"gorm.io/gorm"
	"slices"
)

type MetadataVisibilityCheck func(context.Context, []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error)
type metadataVisibilityKey struct{}
type navigationVisibilityKey struct{}
type NavigationVisibilityCheck func(context.Context, authorization.SourceNavigationCheckRequest) (*authorization.SourceMetadataCheck, error)

func WithNavigationVisibility(ctx context.Context, check NavigationVisibilityCheck) context.Context {
	return context.WithValue(ctx, navigationVisibilityKey{}, check)
}

// WithMetadataVisibility belongs only to the synchronous user query stack.
// Runtime facts and scan readers retain their separately guarded machine paths.
func WithMetadataVisibility(ctx context.Context, check MetadataVisibilityCheck) context.Context {
	return context.WithValue(ctx, metadataVisibilityKey{}, check)
}

func metadataVisibility(ctx context.Context) (MetadataVisibilityCheck, bool) {
	check, ok := ctx.Value(metadataVisibilityKey{}).(MetadataVisibilityCheck)
	return check, ok
}

func filterMetadataResourceTree(ctx context.Context, root *resourcetree.TreeNode, engineID uint, resolve func(context.Context, uint) (plugin.EngineCatalogPath, error), check MetadataVisibilityCheck) (*resourcetree.TreeNode, error) {
	if root == nil || check == nil || resolve == nil {
		return nil, commonapi.ErrForbidden
	}
	targets := []plugin.EngineCatalogPath{}
	keys := map[string]string{}
	nodeCheck, checkNodes := ctx.Value(navigationVisibilityKey{}).(NavigationVisibilityCheck)
	engineType, _ := root.Metadata["engine_type"].(string)
	rootPath, managedRoot := authorizationfacts.NavigationRoot(engineType, engineID)
	nodes := []*resourcetree.TreeNode{}
	paths := []plugin.EngineCatalogPath{}
	var collect func(*resourcetree.TreeNode, plugin.EngineCatalogPath) error
	collect = func(node *resourcetree.TreeNode, parent plugin.EngineCatalogPath) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if node == nil {
			return nil
		}
		loc, err := resourcetree.ParseURI(node.Locator)
		if err != nil || loc.EngineID != engineID {
			return commonapi.ErrForbidden
		}
		if loc.ItemID != nil {
			target, err := resolve(ctx, *loc.ItemID)
			if errors.Is(err, errMetadataItemIdentity) || errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if target.EngineID != engineID {
				return commonapi.ErrForbidden
			}
			key, err := authorization.EncodeSharingTarget(target)
			if err != nil {
				return commonapi.ErrForbidden
			}
			keys[node.Locator] = string(key)
			targets = append(targets, target)
		}
		var path plugin.EngineCatalogPath
		if checkNodes && managedRoot && loc.NodeID != nil && loc.ItemID == nil {
			if node == root {
				path = rootPath
			} else if parent.EngineID != 0 {
				part := plugin.EngineCatalogSegment{Term: node.Type, Kind: node.Type, Name: node.Label}
				switch node.Type {
				case "schema":
					part.Kind = "namespace"
				case "dir":
					part.Term = "directory"
					part.Kind = "directory"
				}
				path = parent
				path.Segments = append(slices.Clone(parent.Segments), part)
			}
			fullName, _ := node.Metadata["full_name"].(string)
			expected := path.StringPath()
			if node.Type == "schema" {
				expected = node.Label
			}
			if authorizationfacts.NodeIdentityMatchesEngine(engineType, path) && expected == fullName {
				paths = append(paths, path)
				nodes = append(nodes, node)
			} else {
				path = plugin.EngineCatalogPath{}
			}
		}
		for _, child := range node.Children {
			if err := collect(child, path); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(root, plugin.EngineCatalogPath{}); err != nil {
		return nil, err
	}
	visible, err := observeMetadataVisibility(ctx, targets, check)
	if err != nil {
		return nil, err
	}
	navigation := map[string]bool{}
	if checkNodes && managedRoot {
		for offset := 0; offset < len(paths); offset += 200 {
			end := offset + 200
			if end > len(paths) {
				end = len(paths)
			}
			query := authorization.SourceNavigationCheckRequest{Targets: paths[offset:end]}
			result, err := nodeCheck(ctx, query)
			if err != nil {
				return nil, err
			}
			if !result.MatchesNavigation(query) {
				return nil, commonapi.ErrForbidden
			}
			for i, target := range result.Targets {
				navigation[nodes[offset+i].Locator] = target.Visible
			}
		}
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
		if (!isRoot || managedRoot) && !isItem && len(children) == 0 && !navigation[node.Locator] {
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
