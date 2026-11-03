package scanresource

import (
	"context"
	"reflect"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
)

type metadataRefreshScopeKey struct{}
type directDirectoryRefreshKey struct{}
type verifiedRefreshItemRefsKey struct{}
type uploadedContentTargetsKey struct{}

// The upload owner has proved successful writes of exactly these paths. This
// is a transient scan bound, never a DataItem Grant or a directory capability.
func WithUploadedContentTargets(ctx context.Context, targets []plugin.EngineCatalogPath) context.Context {
	return context.WithValue(ctx, uploadedContentTargetsKey{}, targets)
}
func hasUploadedContentTargets(ctx context.Context) bool {
	_, present := ctx.Value(uploadedContentTargetsKey{}).([]plugin.EngineCatalogPath)
	return present
}

// Only the Meta owner installs refs from the same item snapshot whose complete
// metadata boundary digest matched the frozen binding. Never from an HTTP body.
func WithVerifiedRefreshItemRefs(ctx context.Context, refs []plugin.EngineCatalogPath) context.Context {
	return context.WithValue(ctx, verifiedRefreshItemRefsKey{}, refs)
}

// A nonrecursive node enumeration may observe child names, but must never
// descend into those children or reconcile their stored subtrees.
func WithDirectDirectoryRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, directDirectoryRefreshKey{}, true)
}
func DirectDirectoryRefresh(ctx context.Context) bool {
	value, _ := ctx.Value(directDirectoryRefreshKey{}).(bool)
	return value
}

func WithMetadataRefreshScope(ctx context.Context, scope *authorization.MetadataRefreshScope) context.Context {
	return context.WithValue(ctx, metadataRefreshScopeKey{}, scope)
}

func MetadataRefreshScope(ctx context.Context) *authorization.MetadataRefreshScope {
	scope, _ := ctx.Value(metadataRefreshScopeKey{}).(*authorization.MetadataRefreshScope)
	return scope
}

// CheckRefreshEnumeration is a local maximum-range check; the current source
// authorization callback must also pass before every actual Catalog call.
func CheckRefreshEnumeration(ctx context.Context, target plugin.EngineCatalogPath, recursive bool) error {
	if hasUploadedContentTargets(ctx) {
		return commonapi.ErrForbidden
	}
	scope := MetadataRefreshScope(ctx)
	if scope == nil {
		return nil
	}
	for _, r := range scope.Ranges {
		if r.Node != nil && r.Node.CoversNode(authorization.SourceNodeScope{Target: target, Recursive: recursive}) {
			return nil
		}
		if r.Item != nil {
			if r.Item.Scope != nil && (authorization.SourceNodeScope{Target: r.Item.Scope.Root, Recursive: true}).CoversNode(authorization.SourceNodeScope{Target: target, Recursive: recursive}) {
				return nil
			}
			if r.Item.MetadataTarget != nil && r.Item.MetadataTarget.Layout == "whole" && (authorization.SourceNodeScope{Target: r.Item.MetadataTarget.Target, Recursive: true}).CoversNode(authorization.SourceNodeScope{Target: target, Recursive: recursive}) {
				return nil
			}
		}
	}
	return commonapi.ErrForbidden
}

func CheckRefreshContent(ctx context.Context, target plugin.EngineCatalogPath) error {
	if targets, present := ctx.Value(uploadedContentTargetsKey{}).([]plugin.EngineCatalogPath); present {
		for _, allowed := range targets {
			if reflect.DeepEqual(allowed, target) {
				return nil
			}
		}
		return commonapi.ErrForbidden
	}
	scope := MetadataRefreshScope(ctx)
	if scope == nil {
		return nil
	}
	if len(scope.Ranges) == 1 && scope.Ranges[0].Item != nil && scope.Ranges[0].Item.MetadataTarget != nil && scope.Ranges[0].Item.MetadataTarget.Layout == "multi" {
		refs, _ := ctx.Value(verifiedRefreshItemRefsKey{}).([]plugin.EngineCatalogPath)
		for _, ref := range refs {
			if reflect.DeepEqual(ref, target) {
				return nil
			}
		}
	}
	for _, r := range scope.Ranges {
		if r.Node != nil && r.Node.Covers(target) {
			return nil
		}
		if r.Item == nil {
			continue
		}
		if r.Item.Scope != nil && (authorization.SourceNodeScope{Target: r.Item.Scope.Root, Recursive: true}).Covers(target) {
			return nil
		}
		if r.Item.FileComposition != nil {
			for _, ref := range r.Item.FileComposition.Refs {
				if reflect.DeepEqual(ref.Target, target) {
					return nil
				}
			}
		}
		primary, err := r.Item.PrimaryTarget()
		if err == nil && reflect.DeepEqual(primary, target) {
			return nil
		}
		if r.Item.MetadataTarget != nil && r.Item.MetadataTarget.Layout == "whole" && (authorization.SourceNodeScope{Target: primary, Recursive: true}).Covers(target) {
			return nil
		}
	}
	return commonapi.ErrForbidden
}
