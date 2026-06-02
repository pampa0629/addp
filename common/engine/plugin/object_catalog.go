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
	ListBucketsFunc           func(ctx context.Context, connInfo ConnectionInfo, root CatalogPath) ([]CatalogEntry, error)
	ListDirectoryFunc         func(ctx context.Context, connInfo ConnectionInfo, parent CatalogPath) ([]CatalogEntry, error)
	GetObjectStorageFactsFunc func(ctx context.Context, connInfo ConnectionInfo, path string) (*StorageObjectFacts, error)
}

// ObjectCatalogModel describes object storage hierarchy: service -> bucket -> prefix? -> object.
func ObjectCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    CatalogTermService,
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermBucket, Kinds: []string{CatalogKindBucket}, Role: CatalogRoleBranch, I18nKey: CatalogTermI18nKey(CatalogTermBucket)},
			{Term: CatalogTermPrefix, Kinds: []string{CatalogKindPrefix}, Role: CatalogRoleBranch, Optional: true, I18nKey: CatalogTermI18nKey(CatalogTermPrefix)},
			{Term: CatalogTermObject, Kinds: []string{CatalogKindObject}, Role: CatalogRoleLeaf, I18nKey: CatalogTermI18nKey(CatalogTermObject)},
		},
	}
}

// ListObjectCatalogChildren maps object-storage buckets, prefixes and objects to CatalogProvider nodes.
func ListObjectCatalogChildren(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, parent CatalogPath, opts ListOptions) ([]CatalogEntry, error) {
	model := ObjectCatalogModel()
	if IsCatalogRootPath(parent) {
		if err := requireCatalogRootPath(parent, model); err != nil {
			return nil, err
		}
		if callbacks.ListBucketsFunc == nil {
			return nil, fmt.Errorf("object catalog callbacks ListBucketsFunc is nil")
		}
		buckets, err := callbacks.ListBucketsFunc(ctx, connInfo, parent)
		if err != nil {
			return nil, err
		}
		return buckets, nil
	}
	if _, err := requireCatalogBusinessPath(parent, model); err != nil {
		return nil, err
	}

	return listObjectCatalogChildren(ctx, callbacks, connInfo, parent, opts)
}

func listObjectCatalogChildren(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogEntry, error) {
	if callbacks.ListDirectoryFunc == nil {
		return nil, fmt.Errorf("object catalog callbacks ListDirectoryFunc is nil")
	}
	nodes, err := callbacks.ListDirectoryFunc(ctx, connInfo, parent)
	if err != nil {
		return nil, err
	}
	if !opts.Recursive {
		return nodes, nil
	}
	result := append([]CatalogEntry(nil), nodes...)
	for _, node := range nodes {
		if node.Role != CatalogRoleBranch {
			continue
		}
		childNodes, err := listObjectCatalogChildren(ctx, callbacks, connInfo, node.Path, opts)
		if err != nil {
			return nil, err
		}
		result = append(result, childNodes...)
	}
	return result, nil
}

// ResolveObjectCatalogPath resolves an object catalog path.
func ResolveObjectCatalogPath(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*CatalogEntry, error) {
	model := ObjectCatalogModel()
	if IsCatalogRootPath(path) {
		if err := requireCatalogRootPath(path, model); err != nil {
			return nil, err
		}
		return &CatalogEntry{
			Name: "",
			Path: path,
			Term: CatalogTermService,
			Kind: CatalogTermService,
			Role: CatalogRoleBranch,
		}, nil
	}
	if _, err := requireCatalogBusinessPath(path, model); err != nil {
		return nil, err
	}

	last := path.Segments[len(path.Segments)-1]
	if last.Kind == CatalogKindObject || last.Term == CatalogTermObject {
		if callbacks.GetObjectStorageFactsFunc == nil {
			return nil, fmt.Errorf("object catalog callbacks GetObjectStorageFactsFunc is nil")
		}
		meta, err := callbacks.GetObjectStorageFactsFunc(ctx, connInfo, path.StringPath())
		if err != nil {
			return nil, err
		}
		return objectStorageFactsCatalogEntry(engineID, path, meta), nil
	}

	return &CatalogEntry{
		Name: last.Name,
		Path: path,
		Term: last.Term,
		Kind: last.Kind,
		Role: CatalogRoleBranch,
		Storage: &CatalogStorageFacts{
			Path: path.StringPath(),
		},
	}, nil
}

// DescribeObjectCatalogFacts maps object storage facts to CatalogFactsProvider output.
func DescribeObjectCatalogFacts(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*CatalogFacts, error) {
	if callbacks.GetObjectStorageFactsFunc == nil {
		return nil, fmt.Errorf("object catalog callbacks GetObjectStorageFactsFunc is nil")
	}
	if _, err := requireCatalogBusinessPath(path, ObjectCatalogModel()); err != nil {
		return nil, err
	}
	meta, err := callbacks.GetObjectStorageFactsFunc(ctx, connInfo, path.StringPath())
	if err != nil {
		return nil, err
	}
	updatedAt := meta.ModifiedAt
	sizeBytes := meta.Size
	return &CatalogFacts{
		Path: path,
		Kind: objectKindFromPath(path),
		Storage: &CatalogStorageFacts{
			Name:        meta.Name,
			Path:        meta.Path,
			ContentType: meta.ContentType,
			ETag:        meta.ETag,
			Extension:   strings.ToLower(filepath.Ext(meta.Name)),
			SizeBytes:   &sizeBytes,
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

func ObjectBucketCatalogEntry(root CatalogPath, name string) CatalogEntry {
	return CatalogEntry{
		Name: name,
		Path: appendCatalogSegment(root, root.EngineID, CatalogTermBucket, CatalogKindBucket, name),
		Term: CatalogTermBucket,
		Kind: CatalogKindBucket,
		Role: CatalogRoleBranch,
		Storage: &CatalogStorageFacts{
			Path: name + "/",
		},
	}
}

func ObjectPrefixCatalogEntry(parent CatalogPath, name, storagePath string) CatalogEntry {
	return CatalogEntry{
		Name: name,
		Path: appendCatalogSegment(parent, parent.EngineID, CatalogTermPrefix, CatalogKindPrefix, name),
		Term: CatalogTermPrefix,
		Kind: CatalogKindPrefix,
		Role: CatalogRoleBranch,
		Storage: &CatalogStorageFacts{
			Path: storagePath,
		},
	}
}

func ObjectLeafCatalogEntry(parent CatalogPath, facts StorageObjectFacts) CatalogEntry {
	sizeBytes := facts.Size
	updatedAt := facts.ModifiedAt
	return CatalogEntry{
		Name: facts.Name,
		Path: appendCatalogSegment(parent, parent.EngineID, CatalogTermObject, CatalogKindObject, facts.Name),
		Term: CatalogTermObject,
		Kind: CatalogKindObject,
		Role: CatalogRoleLeaf,
		Storage: &CatalogStorageFacts{
			Path:        facts.Path,
			ContentType: facts.ContentType,
			ETag:        facts.ETag,
			SizeBytes:   &sizeBytes,
		},
		UpdatedAt: &updatedAt,
	}
}

func objectStorageFactsCatalogEntry(engineID uint, path CatalogPath, meta *StorageObjectFacts) *CatalogEntry {
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
	updatedAt := meta.ModifiedAt
	return &CatalogEntry{
		Name: meta.Name,
		Path: path,
		Term: term,
		Kind: kind,
		Role: CatalogRoleLeaf,
		Storage: &CatalogStorageFacts{
			Path:        meta.Path,
			ContentType: meta.ContentType,
			ETag:        meta.ETag,
			SizeBytes:   &meta.Size,
		},
		UpdatedAt: &updatedAt,
	}
}
