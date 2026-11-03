package client

import (
	"path"
	"sort"
	"strings"
)

func UploadScanRefGroups(uploadedPaths []string) []MetaScanRefGroup {
	type groupKey struct {
		dir  string
		base string
	}
	grouped := map[groupKey][]string{}
	for _, rawPath := range uploadedPaths {
		catalogPath := strings.Trim(rawPath, "/")
		if catalogPath == "" {
			continue
		}
		dir := path.Dir(catalogPath)
		if dir == "." {
			dir = ""
		}
		base := strings.TrimSuffix(path.Base(catalogPath), path.Ext(catalogPath))
		key := groupKey{dir: dir, base: base}
		grouped[key] = append(grouped[key], catalogPath)
	}

	keys := make([]groupKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].dir != keys[j].dir {
			return keys[i].dir < keys[j].dir
		}
		return keys[i].base < keys[j].base
	})

	groups := make([]MetaScanRefGroup, 0, len(keys))
	for _, key := range keys {
		paths := dedupeAndSortCatalogPaths(grouped[key])
		if len(paths) == 0 {
			continue
		}
		primary := uploadScanPrimaryPath(paths)
		refs := make([]MetaScanRef, 0, len(paths))
		for _, catalogPath := range paths {
			refs = append(refs, MetaScanRef{
				Path:     catalogPath,
				Role:     uploadScanRefRole(catalogPath, primary),
				Required: catalogPath == primary,
				Primary:  catalogPath == primary,
			})
		}
		groups = append(groups, MetaScanRefGroup{
			Primary: primary,
			Refs:    refs,
		})
	}
	return groups
}

func dedupeAndSortCatalogPaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, rawPath := range paths {
		catalogPath := strings.Trim(rawPath, "/")
		if catalogPath == "" || seen[catalogPath] {
			continue
		}
		seen[catalogPath] = true
		result = append(result, catalogPath)
	}
	sort.Strings(result)
	return result
}

func uploadScanPrimaryPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	for _, ext := range []string{".shp", ".geojson", ".json", ".csv", ".parquet"} {
		for _, catalogPath := range paths {
			if strings.EqualFold(path.Ext(catalogPath), ext) {
				return catalogPath
			}
		}
	}
	return paths[0]
}

func uploadScanRefRole(catalogPath, primary string) string {
	if catalogPath == primary {
		return "main"
	}
	return "sidecar"
}
