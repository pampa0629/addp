package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	CatalogTermService = "service"
	CatalogTermBucket  = "bucket"
	CatalogTermRoot    = "root"
	CatalogTermPrefix  = "prefix"
	CatalogTermPath    = "path"
	CatalogTermFile    = "file"
	CatalogTermObject  = "object"

	CatalogKindBucket = "bucket"
	CatalogKindRoot   = "root"
	CatalogKindPrefix = "prefix"
	CatalogKindFile   = "file"
	CatalogKindObject = "object"
)

// ObjectCatalogModel describes object storage hierarchy: service -> bucket -> prefix? -> object.
func ObjectCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    CatalogTermService,
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermBucket, Kinds: []string{CatalogKindBucket}, Container: true},
			{Term: CatalogTermPrefix, Kinds: []string{CatalogKindPrefix}, Container: true, Optional: true},
			{Term: CatalogTermObject, Kinds: []string{CatalogKindObject}, Item: true},
		},
	}
}

// FileCatalogModel describes file-system hierarchy: service -> root -> path? -> file.
func FileCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    CatalogTermService,
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermRoot, Kinds: []string{CatalogKindRoot}, Container: true},
			{Term: CatalogTermPath, Kinds: []string{CatalogKindPrefix}, Container: true, Optional: true},
			{Term: CatalogTermFile, Kinds: []string{CatalogKindFile}, Item: true},
		},
	}
}

// ListFileSystemCatalogChildren adapts FileSystemPlugin list operations to CatalogProvider.
func ListFileSystemCatalogChildren(ctx context.Context, fs FileSystemPlugin, connInfo ConnectionInfo, engineID uint, parent CatalogPath, rootTerm string, opts ListOptions) ([]CatalogNode, error) {
	if fs == nil {
		return nil, fmt.Errorf("filesystem plugin cannot be nil")
	}
	if len(parent.Segments) == 0 {
		roots, err := fs.ListRoots(ctx, connInfo)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogNode, 0, len(roots))
		for _, root := range roots {
			kind := CatalogKindRoot
			term := rootTerm
			if rootTerm == CatalogTermBucket {
				kind = CatalogKindBucket
			}
			nodes = append(nodes, CatalogNode{
				Name:        root.Name,
				Path:        appendCatalogSegment(parent, engineID, term, kind, root.Name),
				Term:        term,
				Kind:        kind,
				IsContainer: true,
				Attributes: map[string]interface{}{
					"path": root.Path,
				},
			})
		}
		return nodes, nil
	}

	itemTerm := CatalogTermFile
	itemKind := CatalogKindFile
	if rootTerm == CatalogTermBucket {
		itemTerm = CatalogTermObject
		itemKind = CatalogKindObject
	}
	return listFileSystemCatalogChildren(ctx, fs, connInfo, engineID, parent, parent.StringPath(), itemTerm, itemKind, opts)
}

func listFileSystemCatalogChildren(ctx context.Context, fs FileSystemPlugin, connInfo ConnectionInfo, engineID uint, parent CatalogPath, listPath, itemTerm, itemKind string, opts ListOptions) ([]CatalogNode, error) {
	files, dirs, err := fs.ListDirectory(ctx, connInfo, listPath)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogNode, 0, len(dirs)+len(files))
	for _, dir := range dirs {
		dirPath := appendCatalogSegment(parent, engineID, CatalogTermPrefix, CatalogKindPrefix, dir.Name)
		nodes = append(nodes, CatalogNode{
			Name:        dir.Name,
			Path:        dirPath,
			Term:        CatalogTermPrefix,
			Kind:        CatalogKindPrefix,
			IsContainer: true,
			Attributes: map[string]interface{}{
				"path": dir.Path,
			},
		})
		if opts.Recursive {
			childNodes, err := listFileSystemCatalogChildren(ctx, fs, connInfo, engineID, dirPath, dir.Path, itemTerm, itemKind, opts)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, childNodes...)
		}
	}
	for _, file := range files {
		nodes = append(nodes, CatalogNode{
			Name:   file.Name,
			Path:   appendCatalogSegment(parent, engineID, itemTerm, itemKind, file.Name),
			Term:   itemTerm,
			Kind:   itemKind,
			IsItem: true,
			Stats: map[string]interface{}{
				"size_bytes": file.Size,
			},
			Attributes: map[string]interface{}{
				"path":         file.Path,
				"content_type": file.ContentType,
				"modified_at":  file.ModifiedAt,
			},
		})
	}
	return nodes, nil
}

// ResolveFileSystemCatalogPath resolves a catalog path as a container or file item.
func ResolveFileSystemCatalogPath(ctx context.Context, fs FileSystemPlugin, connInfo ConnectionInfo, engineID uint, path CatalogPath, rootTerm string) (*CatalogNode, error) {
	if len(path.Segments) == 0 {
		return &CatalogNode{
			Name:        "",
			Path:        CatalogPath{Version: CatalogPathVersion, EngineID: engineID},
			Term:        CatalogTermService,
			Kind:        "service",
			IsContainer: true,
		}, nil
	}

	last := path.Segments[len(path.Segments)-1]
	if last.Kind == CatalogKindFile || last.Kind == CatalogKindObject || last.Term == CatalogTermFile || last.Term == CatalogTermObject {
		meta, err := fs.GetFileMetadata(ctx, connInfo, path.StringPath())
		if err != nil {
			return nil, err
		}
		return fileMetadataCatalogNode(engineID, path, meta), nil
	}

	return &CatalogNode{
		Name:        last.Name,
		Path:        path,
		Term:        last.Term,
		Kind:        last.Kind,
		IsContainer: true,
		Attributes: map[string]interface{}{
			"path": path.StringPath(),
		},
	}, nil
}

// DescribeFileSystemItem adapts FileSystemPlugin file metadata to ItemMetadataProvider.
func DescribeFileSystemItem(ctx context.Context, fs FileSystemPlugin, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*ItemMetadata, error) {
	meta, err := fs.GetFileMetadata(ctx, connInfo, path.StringPath())
	if err != nil {
		return nil, err
	}
	updatedAt := meta.ModifiedAt
	return &ItemMetadata{
		Path: path,
		Kind: itemKindFromPath(path),
		Stats: map[string]interface{}{
			"size_bytes": meta.Size,
		},
		Attributes: map[string]interface{}{
			"name":         meta.Name,
			"path":         meta.Path,
			"content_type": meta.ContentType,
			"etag":         meta.ETag,
			"extension":    strings.ToLower(filepath.Ext(meta.Name)),
		},
		UpdatedAt: &updatedAt,
	}, nil
}

func appendCatalogSegment(parent CatalogPath, engineID uint, term, kind, name string) CatalogPath {
	next := CatalogPath{
		Version:  parent.Version,
		EngineID: parent.EngineID,
		Segments: append([]CatalogSegment{}, parent.Segments...),
	}
	if next.Version == "" {
		next.Version = CatalogPathVersion
	}
	if next.EngineID == 0 {
		next.EngineID = engineID
	}
	next.Segments = append(next.Segments, CatalogSegment{Term: term, Kind: kind, Name: name})
	return next
}

func itemKindFromPath(path CatalogPath) string {
	if len(path.Segments) == 0 {
		return CatalogKindFile
	}
	last := path.Segments[len(path.Segments)-1]
	if last.Kind != "" {
		return last.Kind
	}
	if last.Term == CatalogTermObject {
		return CatalogKindObject
	}
	return CatalogKindFile
}

func fileMetadataCatalogNode(engineID uint, path CatalogPath, meta *FileMetadata) *CatalogNode {
	if path.Version == "" {
		path.Version = CatalogPathVersion
	}
	if path.EngineID == 0 {
		path.EngineID = engineID
	}
	term := CatalogTermFile
	kind := CatalogKindFile
	if len(path.Segments) > 0 {
		last := path.Segments[len(path.Segments)-1]
		if last.Term != "" {
			term = last.Term
		}
		if last.Kind != "" {
			kind = last.Kind
		}
	}
	return &CatalogNode{
		Name:   meta.Name,
		Path:   path,
		Term:   term,
		Kind:   kind,
		IsItem: true,
		Stats: map[string]interface{}{
			"size_bytes": meta.Size,
		},
		Attributes: map[string]interface{}{
			"path":         meta.Path,
			"content_type": meta.ContentType,
			"etag":         meta.ETag,
			"modified_at":  meta.ModifiedAt,
		},
	}
}
