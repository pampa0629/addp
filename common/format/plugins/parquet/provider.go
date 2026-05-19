package parquet

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
)

func listParquetResources(ctx context.Context, reader contentio.Reader, scope contentio.Ref) ([]contentio.Ref, error) {
	if reader == nil {
		return nil, contentio.ErrContentNotFound
	}
	lister, ok := reader.(contentio.Lister)
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	refs, err := listParquetResourcesRecursive(ctx, reader, lister, scope, map[string]bool{})
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, contentio.ErrContentNotFound
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Path < refs[j].Path
	})
	return refs, nil
}

func listParquetResourcesRecursive(ctx context.Context, reader contentio.Reader, lister contentio.Lister, scope contentio.Ref, seen map[string]bool) ([]contentio.Ref, error) {
	if seen[scope.Path] {
		return nil, nil
	}
	seen[scope.Path] = true
	refs, err := lister.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	result := make([]contentio.Ref, 0, len(refs))
	for _, ref := range refs {
		if ref.Role == contentio.RoleScope {
			children, err := listParquetResourcesRecursive(ctx, reader, lister, ref, seen)
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
