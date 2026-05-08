package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	CatalogTermService   = "service"
	CatalogTermBucket    = "bucket"
	CatalogTermRoot      = "root"
	CatalogTermPrefix    = "prefix"
	CatalogTermDirectory = "directory"
	CatalogTermPath      = "path"
	CatalogTermFile      = "file"
	CatalogTermObject    = "object"

	CatalogKindBucket    = "bucket"
	CatalogKindRoot      = "root"
	CatalogKindPrefix    = "prefix"
	CatalogKindDirectory = "directory"
	CatalogKindFile      = "file"
	CatalogKindObject    = "object"
)

type ObjectCatalogAdapter struct {
	ListRootsFunc       func(ctx context.Context, connInfo ConnectionInfo) ([]RootEntry, error)
	ListDirectoryFunc   func(ctx context.Context, connInfo ConnectionInfo, path string) (files []FileEntry, subdirs []DirEntry, err error)
	GetFileMetadataFunc func(ctx context.Context, connInfo ConnectionInfo, path string) (*FileMetadata, error)
}

type FileCatalogAdapter struct {
	ListRootsFunc       func(ctx context.Context, connInfo ConnectionInfo) ([]RootEntry, error)
	ListDirectoryFunc   func(ctx context.Context, connInfo ConnectionInfo, path string) (files []FileEntry, subdirs []DirEntry, err error)
	GetFileMetadataFunc func(ctx context.Context, connInfo ConnectionInfo, path string) (*FileMetadata, error)
}

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

// FileCatalogModel describes file-system hierarchy: root -> directory? -> file.
func FileCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    CatalogTermRoot,
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermDirectory, Kinds: []string{CatalogKindDirectory}, Container: true, Optional: true},
			{Term: CatalogTermFile, Kinds: []string{CatalogKindFile}, Item: true},
		},
	}
}

// ListObjectCatalogChildren adapts object-storage listing callbacks to CatalogProvider.
func ListObjectCatalogChildren(ctx context.Context, adapter ObjectCatalogAdapter, connInfo ConnectionInfo, engineID uint, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if len(parent.Segments) == 0 {
		if adapter.ListRootsFunc == nil {
			return nil, fmt.Errorf("object catalog adapter ListRootsFunc is nil")
		}
		roots, err := adapter.ListRootsFunc(ctx, connInfo)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogNode, 0, len(roots))
		for _, root := range roots {
			nodes = append(nodes, CatalogNode{
				Name:        root.Name,
				Path:        appendCatalogSegment(parent, engineID, CatalogTermBucket, CatalogKindBucket, root.Name),
				Term:        CatalogTermBucket,
				Kind:        CatalogKindBucket,
				IsContainer: true,
				Attributes: map[string]interface{}{
					"path": root.Path,
				},
			})
		}
		return nodes, nil
	}

	return listCatalogChildren(ctx, objectListAdapter(adapter), connInfo, engineID, parent, parent.StringPath(), CatalogTermPrefix, CatalogKindPrefix, CatalogTermObject, CatalogKindObject, opts)
}

// ListFileCatalogChildren adapts filesystem listing callbacks to CatalogProvider.
func ListFileCatalogChildren(ctx context.Context, adapter FileCatalogAdapter, connInfo ConnectionInfo, engineID uint, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if len(parent.Segments) == 0 {
		if adapter.ListRootsFunc == nil {
			return nil, fmt.Errorf("file catalog adapter ListRootsFunc is nil")
		}
		roots, err := adapter.ListRootsFunc(ctx, connInfo)
		if err != nil {
			return nil, err
		}
		nodes := make([]CatalogNode, 0, len(roots))
		for _, root := range roots {
			nodes = append(nodes, CatalogNode{
				Name:        root.Name,
				Path:        appendCatalogSegment(parent, engineID, CatalogTermRoot, CatalogKindRoot, root.Name),
				Term:        CatalogTermRoot,
				Kind:        CatalogKindRoot,
				IsContainer: true,
				Attributes: map[string]interface{}{
					"path": root.Path,
				},
			})
		}
		return nodes, nil
	}

	return listCatalogChildren(ctx, fileListAdapter(adapter), connInfo, engineID, parent, catalogListPath(parent), CatalogTermDirectory, CatalogKindDirectory, CatalogTermFile, CatalogKindFile, opts)
}

type catalogListAdapter struct {
	ListDirectoryFunc   func(ctx context.Context, connInfo ConnectionInfo, path string) (files []FileEntry, subdirs []DirEntry, err error)
	GetFileMetadataFunc func(ctx context.Context, connInfo ConnectionInfo, path string) (*FileMetadata, error)
}

func listCatalogChildren(ctx context.Context, adapter catalogListAdapter, connInfo ConnectionInfo, engineID uint, parent CatalogPath, listPath, containerTerm, containerKind, itemTerm, itemKind string, opts ListOptions) ([]CatalogNode, error) {
	if adapter.ListDirectoryFunc == nil {
		return nil, fmt.Errorf("catalog adapter ListDirectoryFunc is nil")
	}
	files, dirs, err := adapter.ListDirectoryFunc(ctx, connInfo, listPath)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogNode, 0, len(dirs)+len(files))
	for _, dir := range dirs {
		dirPath := appendCatalogSegment(parent, engineID, containerTerm, containerKind, dir.Name)
		nodes = append(nodes, CatalogNode{
			Name:        dir.Name,
			Path:        dirPath,
			Term:        containerTerm,
			Kind:        containerKind,
			IsContainer: true,
			Attributes: map[string]interface{}{
				"path": dir.Path,
			},
		})
		if opts.Recursive {
			childNodes, err := listCatalogChildren(ctx, adapter, connInfo, engineID, dirPath, dir.Path, containerTerm, containerKind, itemTerm, itemKind, opts)
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

// ResolveObjectCatalogPath resolves an object catalog path.
func ResolveObjectCatalogPath(ctx context.Context, adapter ObjectCatalogAdapter, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*CatalogNode, error) {
	return resolveContentCatalogPath(ctx, objectListAdapter(adapter), connInfo, engineID, path, CatalogTermObject, CatalogKindObject)
}

// ResolveFileCatalogPath resolves a file catalog path.
func ResolveFileCatalogPath(ctx context.Context, adapter FileCatalogAdapter, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*CatalogNode, error) {
	return resolveContentCatalogPath(ctx, fileListAdapter(adapter), connInfo, engineID, path, CatalogTermFile, CatalogKindFile)
}

func resolveContentCatalogPath(ctx context.Context, adapter catalogListAdapter, connInfo ConnectionInfo, engineID uint, path CatalogPath, itemTerm, itemKind string) (*CatalogNode, error) {
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
	if last.Kind == itemKind || last.Term == itemTerm {
		if adapter.GetFileMetadataFunc == nil {
			return nil, fmt.Errorf("catalog adapter GetFileMetadataFunc is nil")
		}
		meta, err := adapter.GetFileMetadataFunc(ctx, connInfo, path.StringPath())
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

// DescribeObjectItem adapts object metadata callback to ItemMetadataProvider.
func DescribeObjectItem(ctx context.Context, adapter ObjectCatalogAdapter, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*ItemMetadata, error) {
	return describeContentItem(ctx, objectListAdapter(adapter), connInfo, engineID, path)
}

// DescribeFileItem adapts file metadata callback to ItemMetadataProvider.
func DescribeFileItem(ctx context.Context, adapter FileCatalogAdapter, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*ItemMetadata, error) {
	return describeContentItem(ctx, fileListAdapter(adapter), connInfo, engineID, path)
}

func describeContentItem(ctx context.Context, adapter catalogListAdapter, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*ItemMetadata, error) {
	if adapter.GetFileMetadataFunc == nil {
		return nil, fmt.Errorf("catalog adapter GetFileMetadataFunc is nil")
	}
	meta, err := adapter.GetFileMetadataFunc(ctx, connInfo, path.StringPath())
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

func objectListAdapter(adapter ObjectCatalogAdapter) catalogListAdapter {
	return catalogListAdapter{
		ListDirectoryFunc:   adapter.ListDirectoryFunc,
		GetFileMetadataFunc: adapter.GetFileMetadataFunc,
	}
}

func fileListAdapter(adapter FileCatalogAdapter) catalogListAdapter {
	return catalogListAdapter{
		ListDirectoryFunc:   adapter.ListDirectoryFunc,
		GetFileMetadataFunc: adapter.GetFileMetadataFunc,
	}
}

func catalogListPath(path CatalogPath) string {
	if len(path.Segments) == 1 && path.Segments[0].Kind == CatalogKindRoot {
		return "/"
	}
	return path.StringPath()
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
