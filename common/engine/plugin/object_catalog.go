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
	CatalogTermPrefix  = "prefix"
	CatalogTermObject  = "object"

	CatalogKindBucket = "bucket"
	CatalogKindPrefix = "prefix"
	CatalogKindObject = "object"
)

type ObjectCatalogCallbacks struct {
	ListRootsFunc         func(ctx context.Context, connInfo ConnectionInfo) ([]RootEntry, error)
	ListDirectoryFunc     func(ctx context.Context, connInfo ConnectionInfo, path string) (files []FileEntry, prefixes []DirEntry, err error)
	GetObjectMetadataFunc func(ctx context.Context, connInfo ConnectionInfo, path string) (*FileMetadata, error)
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

// ListObjectCatalogChildren maps object-storage buckets, prefixes and objects to CatalogProvider nodes.
func ListObjectCatalogChildren(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, parent CatalogPath, opts ListOptions) ([]CatalogNode, error) {
	if len(parent.Segments) == 0 {
		if callbacks.ListRootsFunc == nil {
			return nil, fmt.Errorf("object catalog callbacks ListRootsFunc is nil")
		}
		roots, err := callbacks.ListRootsFunc(ctx, connInfo)
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

	return listObjectCatalogChildren(ctx, callbacks, connInfo, engineID, parent, parent.StringPath(), opts)
}

func listObjectCatalogChildren(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, parent CatalogPath, listPath string, opts ListOptions) ([]CatalogNode, error) {
	if callbacks.ListDirectoryFunc == nil {
		return nil, fmt.Errorf("object catalog callbacks ListDirectoryFunc is nil")
	}
	objects, prefixes, err := callbacks.ListDirectoryFunc(ctx, connInfo, listPath)
	if err != nil {
		return nil, err
	}
	nodes := make([]CatalogNode, 0, len(prefixes)+len(objects))
	for _, prefix := range prefixes {
		prefixPath := appendCatalogSegment(parent, engineID, CatalogTermPrefix, CatalogKindPrefix, prefix.Name)
		nodes = append(nodes, CatalogNode{
			Name:        prefix.Name,
			Path:        prefixPath,
			Term:        CatalogTermPrefix,
			Kind:        CatalogKindPrefix,
			IsContainer: true,
			Attributes: map[string]interface{}{
				"path": prefix.Path,
			},
		})
		if opts.Recursive {
			childNodes, err := listObjectCatalogChildren(ctx, callbacks, connInfo, engineID, prefixPath, prefix.Path, opts)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, childNodes...)
		}
	}
	for _, object := range objects {
		nodes = append(nodes, CatalogNode{
			Name:   object.Name,
			Path:   appendCatalogSegment(parent, engineID, CatalogTermObject, CatalogKindObject, object.Name),
			Term:   CatalogTermObject,
			Kind:   CatalogKindObject,
			IsItem: true,
			Stats: map[string]interface{}{
				"size_bytes": object.Size,
			},
			Attributes: map[string]interface{}{
				"path":         object.Path,
				"content_type": object.ContentType,
				"modified_at":  object.ModifiedAt,
			},
		})
	}
	return nodes, nil
}

// ResolveObjectCatalogPath resolves an object catalog path.
func ResolveObjectCatalogPath(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*CatalogNode, error) {
	if len(path.Segments) == 0 {
		return &CatalogNode{
			Name:        "",
			Path:        CatalogPath{Version: CatalogPathVersion, EngineID: engineID},
			Term:        CatalogTermService,
			Kind:        CatalogTermService,
			IsContainer: true,
		}, nil
	}

	last := path.Segments[len(path.Segments)-1]
	if last.Kind == CatalogKindObject || last.Term == CatalogTermObject {
		if callbacks.GetObjectMetadataFunc == nil {
			return nil, fmt.Errorf("object catalog callbacks GetObjectMetadataFunc is nil")
		}
		meta, err := callbacks.GetObjectMetadataFunc(ctx, connInfo, path.StringPath())
		if err != nil {
			return nil, err
		}
		return objectMetadataCatalogNode(engineID, path, meta), nil
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

// DescribeObjectItem maps object metadata to ItemMetadataProvider output.
func DescribeObjectItem(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*ItemMetadata, error) {
	if callbacks.GetObjectMetadataFunc == nil {
		return nil, fmt.Errorf("object catalog callbacks GetObjectMetadataFunc is nil")
	}
	meta, err := callbacks.GetObjectMetadataFunc(ctx, connInfo, path.StringPath())
	if err != nil {
		return nil, err
	}
	updatedAt := meta.ModifiedAt
	return &ItemMetadata{
		Path: path,
		Kind: objectKindFromPath(path),
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

func objectKindFromPath(path CatalogPath) string {
	if len(path.Segments) == 0 {
		return CatalogKindObject
	}
	last := path.Segments[len(path.Segments)-1]
	if last.Kind != "" {
		return last.Kind
	}
	return CatalogKindObject
}

func objectMetadataCatalogNode(engineID uint, path CatalogPath, meta *FileMetadata) *CatalogNode {
	if path.Version == "" {
		path.Version = CatalogPathVersion
	}
	if path.EngineID == 0 {
		path.EngineID = engineID
	}
	term := CatalogTermObject
	kind := CatalogKindObject
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
