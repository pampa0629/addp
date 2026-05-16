package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	CatalogTermRoot      = "root"
	CatalogTermDirectory = "directory"
	CatalogTermFile      = "file"

	CatalogKindRoot      = "root"
	CatalogKindDirectory = "directory"
	CatalogKindFile      = "file"
)

type FileCatalogCallbacks struct {
	ListRootsFunc       func(ctx context.Context, connInfo ConnectionInfo) ([]RootEntry, error)
	ListDirectoryFunc   func(ctx context.Context, connInfo ConnectionInfo, path string) (files []FileEntry, directories []DirEntry, err error)
	GetFileMetadataFunc func(ctx context.Context, connInfo ConnectionInfo, path string) (*FileMetadata, error)
}

// FileCatalogModel describes file-system hierarchy: root -> directory? -> file.
func FileCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    CatalogTermRoot,
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermDirectory, Kinds: []string{CatalogKindDirectory}, Container: true, Optional: true, I18nKey: CatalogTermI18nKey(CatalogTermDirectory)},
			{Term: CatalogTermFile, Kinds: []string{CatalogKindFile}, Item: true, I18nKey: CatalogTermI18nKey(CatalogTermFile)},
		},
	}
}

// ListFileCatalogChildren maps filesystem roots, directories and files to CatalogProvider nodes.
func ListFileCatalogChildren(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if len(parent.Segments) == 0 {
		if callbacks.ListRootsFunc == nil {
			return nil, fmt.Errorf("file catalog callbacks ListRootsFunc is nil")
		}
		roots, err := callbacks.ListRootsFunc(ctx, connInfo)
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

	return listFileCatalogChildren(ctx, callbacks, connInfo, engineID, parent, fileCatalogListPath(parent), opts)
}

func listFileCatalogChildren(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, parent CatalogPath, listPath string, opts ListOptions) ([]CatalogNode, error) {
	if callbacks.ListDirectoryFunc == nil {
		return nil, fmt.Errorf("file catalog callbacks ListDirectoryFunc is nil")
	}
	files, dirs, err := callbacks.ListDirectoryFunc(ctx, connInfo, listPath)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogNode, 0, len(dirs)+len(files))
	for _, dir := range dirs {
		dirPath := appendCatalogSegment(parent, engineID, CatalogTermDirectory, CatalogKindDirectory, dir.Name)
		nodes = append(nodes, CatalogNode{
			Name:        dir.Name,
			Path:        dirPath,
			Term:        CatalogTermDirectory,
			Kind:        CatalogKindDirectory,
			IsContainer: true,
			Attributes: map[string]interface{}{
				"path": dir.Path,
			},
		})
		if opts.Recursive {
			childNodes, err := listFileCatalogChildren(ctx, callbacks, connInfo, engineID, dirPath, dir.Path, opts)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, childNodes...)
		}
	}
	for _, file := range files {
		filePath := appendCatalogSegment(parent, engineID, CatalogTermFile, CatalogKindFile, file.Name)
		nodes = append(nodes, CatalogNode{
			Name:   file.Name,
			Path:   filePath,
			Term:   CatalogTermFile,
			Kind:   CatalogKindFile,
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

// ResolveFileCatalogPath resolves a file catalog path.
func ResolveFileCatalogPath(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*CatalogNode, error) {
	if len(path.Segments) == 0 {
		return &CatalogNode{
			Name:        "",
			Path:        CatalogPath{Version: CatalogPathVersion, EngineID: engineID},
			Term:        CatalogTermRoot,
			Kind:        CatalogTermRoot,
			IsContainer: true,
		}, nil
	}

	last := path.Segments[len(path.Segments)-1]
	if last.Kind == CatalogKindFile || last.Term == CatalogTermFile {
		if callbacks.GetFileMetadataFunc == nil {
			return nil, fmt.Errorf("file catalog callbacks GetFileMetadataFunc is nil")
		}
		meta, err := callbacks.GetFileMetadataFunc(ctx, connInfo, path.StringPath())
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

// DescribeFileItem maps file metadata to ItemMetadataProvider output.
func DescribeFileItem(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*ItemMetadata, error) {
	if callbacks.GetFileMetadataFunc == nil {
		return nil, fmt.Errorf("file catalog callbacks GetFileMetadataFunc is nil")
	}
	meta, err := callbacks.GetFileMetadataFunc(ctx, connInfo, path.StringPath())
	if err != nil {
		return nil, err
	}
	updatedAt := meta.ModifiedAt
	return &ItemMetadata{
		Path: path,
		Kind: fileKindFromPath(path),
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

func fileCatalogListPath(path CatalogPath) string {
	if len(path.Segments) == 1 && path.Segments[0].Kind == CatalogKindRoot {
		return "/"
	}
	return path.StringPath()
}

func FileCatalogEntryFromNode(node CatalogNode) (FileEntry, bool) {
	if !node.IsItem {
		return FileEntry{}, false
	}
	return FileEntry{
		Name:        node.Name,
		Path:        catalogNodePath(node),
		CatalogPath: node.Path,
		Size:        catalogNodeInt64Stat(node.Stats, "size_bytes"),
		ModifiedAt:  catalogNodeTimeAttribute(node.Attributes, "modified_at"),
		ContentType: catalogNodeStringAttribute(node.Attributes, "content_type"),
	}, true
}

func FileCatalogDirectoryFromNode(node CatalogNode) (DirEntry, bool) {
	if !node.IsContainer {
		return DirEntry{}, false
	}
	return DirEntry{
		Name:        node.Name,
		Path:        catalogNodePath(node),
		CatalogPath: node.Path,
	}, true
}

func fileKindFromPath(path CatalogPath) string {
	if len(path.Segments) == 0 {
		return CatalogKindFile
	}
	last := path.Segments[len(path.Segments)-1]
	if last.Kind != "" {
		return last.Kind
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
