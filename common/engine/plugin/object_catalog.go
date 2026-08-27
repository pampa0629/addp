package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	EngineCatalogTermService = "service"
	EngineCatalogTermBucket  = "bucket"
	EngineCatalogTermPrefix  = "prefix"
	EngineCatalogTermObject  = "object"

	EngineCatalogKindBucket = "bucket"
	EngineCatalogKindPrefix = "prefix"
	EngineCatalogKindObject = "object"
)

type ObjectCatalogCallbacks struct {
	ListBucketsFunc           func(ctx context.Context, connInfo ConnectionInfo, root EngineCatalogPath) ([]EngineCatalogEntry, error)
	ListDirectoryFunc         func(ctx context.Context, connInfo ConnectionInfo, parent EngineCatalogPath) ([]EngineCatalogEntry, error)
	GetObjectStorageFactsFunc func(ctx context.Context, connInfo ConnectionInfo, path string) (*StorageObjectFacts, error)
}

// ObjectCatalogModel describes object storage hierarchy: service -> bucket -> prefix? -> object.
func ObjectCatalogModel() EngineCatalogModelSpec {
	return EngineCatalogModelSpec{
		PathVersion: EngineCatalogPathVersion,
		RootTerm:    EngineCatalogTermService,
		Levels: []EngineCatalogLevelSpec{
			{Term: EngineCatalogTermBucket, Kinds: []string{EngineCatalogKindBucket}, Role: EngineCatalogRoleBranch, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermBucket)},
			{Term: EngineCatalogTermPrefix, Kinds: []string{EngineCatalogKindPrefix}, Role: EngineCatalogRoleBranch, Optional: true, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermPrefix)},
			{Term: EngineCatalogTermObject, Kinds: []string{EngineCatalogKindObject}, Role: EngineCatalogRoleLeaf, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermObject)},
		},
	}
}

// ListObjectCatalogChildren maps object-storage buckets, prefixes and objects to EngineCatalogProvider nodes.
func ListObjectCatalogChildren(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, parent EngineCatalogPath, opts ListOptions) ([]EngineCatalogEntry, error) {
	model := ObjectCatalogModel()
	if IsEngineCatalogRootPath(parent) {
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

func listObjectCatalogChildren(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, parent EngineCatalogPath, opts ListOptions) ([]EngineCatalogEntry, error) {
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
	result := append([]EngineCatalogEntry(nil), nodes...)
	for _, node := range nodes {
		if node.Role != EngineCatalogRoleBranch {
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
func ResolveObjectCatalogPath(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path EngineCatalogPath) (*EngineCatalogEntry, error) {
	model := ObjectCatalogModel()
	if IsEngineCatalogRootPath(path) {
		if err := requireCatalogRootPath(path, model); err != nil {
			return nil, err
		}
		return &EngineCatalogEntry{
			Name: "",
			Path: path,
			Term: EngineCatalogTermService,
			Kind: EngineCatalogTermService,
			Role: EngineCatalogRoleBranch,
		}, nil
	}
	if _, err := requireCatalogBusinessPath(path, model); err != nil {
		return nil, err
	}

	last := path.Segments[len(path.Segments)-1]
	if last.Kind == EngineCatalogKindObject || last.Term == EngineCatalogTermObject {
		if callbacks.GetObjectStorageFactsFunc == nil {
			return nil, fmt.Errorf("object catalog callbacks GetObjectStorageFactsFunc is nil")
		}
		storageFacts, err := callbacks.GetObjectStorageFactsFunc(ctx, connInfo, path.StringPath())
		if err != nil {
			return nil, err
		}
		return objectStorageFactsCatalogEntry(engineID, path, storageFacts), nil
	}

	return &EngineCatalogEntry{
		Name: last.Name,
		Path: path,
		Term: last.Term,
		Kind: last.Kind,
		Role: EngineCatalogRoleBranch,
		Storage: &EngineCatalogStorageFacts{
			Path: path.StringPath(),
		},
	}, nil
}

// DescribeObjectCatalogFacts maps object storage facts to EngineCatalogFactsProvider output.
func DescribeObjectCatalogFacts(ctx context.Context, callbacks ObjectCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path EngineCatalogPath) (*EngineCatalogFacts, error) {
	if _, err := requireCatalogBusinessPath(path, ObjectCatalogModel()); err != nil {
		return nil, err
	}
	if callbacks.GetObjectStorageFactsFunc == nil {
		return nil, fmt.Errorf("object catalog callbacks GetObjectStorageFactsFunc is nil")
	}
	storageFacts, err := callbacks.GetObjectStorageFactsFunc(ctx, connInfo, path.StringPath())
	if err != nil {
		return nil, err
	}
	updatedAt := storageFacts.ModifiedAt
	sizeBytes := storageFacts.Size
	return &EngineCatalogFacts{
		Path: path,
		Kind: objectKindFromPath(path),
		Storage: &EngineCatalogStorageFacts{
			Name:        storageFacts.Name,
			Path:        storageFacts.Path,
			ContentType: storageFacts.ContentType,
			ETag:        storageFacts.ETag,
			Extension:   strings.ToLower(filepath.Ext(storageFacts.Name)),
			SizeBytes:   &sizeBytes,
		},
		UpdatedAt: &updatedAt,
	}, nil
}

func objectKindFromPath(path EngineCatalogPath) string {
	last := path.Segments[len(path.Segments)-1]
	if last.Kind != "" {
		return last.Kind
	}
	return EngineCatalogKindObject
}

func ObjectBucketCatalogEntry(root EngineCatalogPath, name string) EngineCatalogEntry {
	return EngineCatalogEntry{
		Name: name,
		Path: appendCatalogSegment(root, root.EngineID, EngineCatalogTermBucket, EngineCatalogKindBucket, name),
		Term: EngineCatalogTermBucket,
		Kind: EngineCatalogKindBucket,
		Role: EngineCatalogRoleBranch,
		Storage: &EngineCatalogStorageFacts{
			Path: name + "/",
		},
	}
}

func ObjectPrefixCatalogEntry(parent EngineCatalogPath, name, storagePath string) EngineCatalogEntry {
	return EngineCatalogEntry{
		Name: name,
		Path: appendCatalogSegment(parent, parent.EngineID, EngineCatalogTermPrefix, EngineCatalogKindPrefix, name),
		Term: EngineCatalogTermPrefix,
		Kind: EngineCatalogKindPrefix,
		Role: EngineCatalogRoleBranch,
		Storage: &EngineCatalogStorageFacts{
			Path: storagePath,
		},
	}
}

func ObjectLeafCatalogEntry(parent EngineCatalogPath, facts StorageObjectFacts) EngineCatalogEntry {
	sizeBytes := facts.Size
	updatedAt := facts.ModifiedAt
	return EngineCatalogEntry{
		Name: facts.Name,
		Path: appendCatalogSegment(parent, parent.EngineID, EngineCatalogTermObject, EngineCatalogKindObject, facts.Name),
		Term: EngineCatalogTermObject,
		Kind: EngineCatalogKindObject,
		Role: EngineCatalogRoleLeaf,
		Storage: EngineCatalogEntryStorageSummary(&EngineCatalogStorageFacts{
			Path:        facts.Path,
			ContentType: facts.ContentType,
			ETag:        facts.ETag,
			SizeBytes:   &sizeBytes,
		}),
		UpdatedAt: &updatedAt,
	}
}

func objectStorageFactsCatalogEntry(engineID uint, path EngineCatalogPath, facts *StorageObjectFacts) *EngineCatalogEntry {
	if path.Version == "" {
		path.Version = EngineCatalogPathVersion
	}
	if path.EngineID == 0 {
		path.EngineID = engineID
	}
	term := EngineCatalogTermObject
	kind := EngineCatalogKindObject
	if len(path.Segments) > 0 {
		last := path.Segments[len(path.Segments)-1]
		if last.Term != "" {
			term = last.Term
		}
		if last.Kind != "" {
			kind = last.Kind
		}
	}
	updatedAt := facts.ModifiedAt
	return &EngineCatalogEntry{
		Name: facts.Name,
		Path: path,
		Term: term,
		Kind: kind,
		Role: EngineCatalogRoleLeaf,
		Storage: EngineCatalogEntryStorageSummary(&EngineCatalogStorageFacts{
			Path:        facts.Path,
			ContentType: facts.ContentType,
			ETag:        facts.ETag,
			SizeBytes:   &facts.Size,
		}),
		UpdatedAt: &updatedAt,
	}
}
