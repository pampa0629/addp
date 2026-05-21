package parquet

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
)

type scopedParquetRef struct {
	Ref        contentio.Ref
	Partitions []partitionValue
}

type partitionValue struct {
	Name  string
	Value string
}

func listParquetResources(ctx context.Context, reader contentio.Reader, scope contentio.Ref) ([]contentio.Ref, error) {
	scopedRefs, err := listParquetScopeResources(ctx, reader, scope)
	if err != nil {
		return nil, err
	}
	refs := make([]contentio.Ref, 0, len(scopedRefs))
	for _, scopedRef := range scopedRefs {
		refs = append(refs, scopedRef.Ref)
	}
	return refs, nil
}

func listParquetScopeResources(ctx context.Context, reader contentio.Reader, scope contentio.Ref) ([]scopedParquetRef, error) {
	if reader == nil {
		return nil, contentio.ErrContentNotFound
	}
	lister, ok := reader.(contentio.Lister)
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	rootPath := strings.Trim(scope.Path, "/")
	refs, err := listParquetResourcesRecursive(ctx, reader, lister, scope, rootPath, map[string]bool{})
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, contentio.ErrContentNotFound
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Ref.Path < refs[j].Ref.Path
	})
	return refs, nil
}

func listParquetResourcesRecursive(ctx context.Context, reader contentio.Reader, lister contentio.Lister, scope contentio.Ref, rootPath string, seen map[string]bool) ([]scopedParquetRef, error) {
	if seen[scope.Path] {
		return nil, nil
	}
	seen[scope.Path] = true
	refs, err := lister.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	result := make([]scopedParquetRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Role == contentio.RoleScope {
			children, err := listParquetResourcesRecursive(ctx, reader, lister, ref, rootPath, seen)
			if err != nil {
				return nil, err
			}
			result = append(result, children...)
			continue
		}
		if strings.EqualFold(filepath.Ext(ref.Path), ".parquet") {
			result = append(result, scopedParquetRef{
				Ref:        ref,
				Partitions: partitionValuesForPath(rootPath, ref.Path),
			})
		}
	}
	return result, nil
}

func partitionValuesForPath(scopePath, filePath string) []partitionValue {
	scopePath = strings.Trim(scopePath, "/")
	filePath = strings.Trim(filePath, "/")
	relativePath := filePath
	if scopePath != "" {
		prefix := scopePath + "/"
		if !strings.HasPrefix(filePath, prefix) {
			return nil
		}
		relativePath = strings.TrimPrefix(filePath, prefix)
	}
	relativePath = strings.Trim(relativePath, "/")
	segments := strings.Split(relativePath, "/")
	if len(segments) <= 1 {
		return nil
	}
	partitions := make([]partitionValue, 0, len(segments)-1)
	for _, segment := range segments[:len(segments)-1] {
		key, value, ok := strings.Cut(segment, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" {
			continue
		}
		partitions = append(partitions, partitionValue{Name: key, Value: value})
	}
	return partitions
}
