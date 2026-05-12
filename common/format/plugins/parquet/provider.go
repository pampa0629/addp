package parquet

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/resource"
)

func listParquetResources(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef) ([]resource.ResourceRef, error) {
	if reader == nil {
		return nil, resource.ErrResourceNotFound
	}
	refs, err := listParquetResourcesRecursive(ctx, reader, scope, map[string]bool{})
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, resource.ErrResourceNotFound
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Path < refs[j].Path
	})
	return refs, nil
}

func listParquetResourcesRecursive(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, seen map[string]bool) ([]resource.ResourceRef, error) {
	if seen[scope.Path] {
		return nil, nil
	}
	seen[scope.Path] = true
	refs, err := reader.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	result := make([]resource.ResourceRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Role == resource.ResourceRoleScope {
			children, err := listParquetResourcesRecursive(ctx, reader, ref, seen)
			if err != nil {
				return nil, err
			}
			result = append(result, children...)
			continue
		}
		if strings.EqualFold(filepath.Ext(ref.Path), ".parquet") {
			result = append(result, ref)
		}
	}
	return result, nil
}
