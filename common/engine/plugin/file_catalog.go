package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	EngineCatalogTermRoot      = "root"
	EngineCatalogTermDirectory = "directory"
	EngineCatalogTermFile      = "file"

	EngineCatalogKindRoot      = "root"
	EngineCatalogKindDirectory = "directory"
	EngineCatalogKindFile      = "file"
)

type FileCatalogCallbacks struct {
	ListDirectoryFunc       func(ctx context.Context, connInfo ConnectionInfo, parent EngineCatalogPath) ([]EngineCatalogEntry, error)
	GetFileStorageFactsFunc func(ctx context.Context, connInfo ConnectionInfo, path string) (*StorageObjectFacts, error)
}

// FileCatalogModel describes file-system hierarchy: root -> directory? -> file.
func FileCatalogModel() EngineCatalogModelSpec {
	return EngineCatalogModelSpec{
		PathVersion: EngineCatalogPathVersion,
		RootTerm:    EngineCatalogTermRoot,
		Levels: []EngineCatalogLevelSpec{
			{Term: EngineCatalogTermDirectory, Kinds: []string{EngineCatalogKindDirectory}, Role: EngineCatalogRoleBranch, Optional: true, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermDirectory)},
			{Term: EngineCatalogTermFile, Kinds: []string{EngineCatalogKindFile}, Role: EngineCatalogRoleLeaf, I18nKey: EngineCatalogTermI18nKey(EngineCatalogTermFile)},
		},
	}
}

// ListFileCatalogChildren maps filesystem roots, directories and files to EngineCatalogProvider nodes.
func ListFileCatalogChildren(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, parent EngineCatalogPath, opts ListOptions) ([]EngineCatalogEntry, error) {
	parent = NormalizeFileCatalogSegments(parent)
	model := FileCatalogModel()
	if IsEngineCatalogRootPath(parent) {
		if err := requireCatalogRootPath(parent, model); err != nil {
			return nil, err
		}
		return listFileCatalogChildren(ctx, callbacks, connInfo, parent, opts)
	}
	if _, err := requireCatalogBusinessPath(parent, model); err != nil {
		return nil, err
	}
	return listFileCatalogChildren(ctx, callbacks, connInfo, parent, opts)
}

func listFileCatalogChildren(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, parent EngineCatalogPath, opts ListOptions) ([]EngineCatalogEntry, error) {
	if callbacks.ListDirectoryFunc == nil {
		return nil, fmt.Errorf("file catalog callbacks ListDirectoryFunc is nil")
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
		childNodes, err := listFileCatalogChildren(ctx, callbacks, connInfo, node.Path, opts)
		if err != nil {
			return nil, err
		}
		result = append(result, childNodes...)
	}
	return result, nil
}

// ResolveFileCatalogPath resolves a file catalog path.
func ResolveFileCatalogPath(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path EngineCatalogPath) (*EngineCatalogEntry, error) {
	path = NormalizeFileCatalogSegments(path)
	model := FileCatalogModel()
	if IsEngineCatalogRootPath(path) {
		if err := requireCatalogRootPath(path, model); err != nil {
			return nil, err
		}
		return &EngineCatalogEntry{
			Name: "",
			Path: path,
			Term: EngineCatalogTermRoot,
			Kind: EngineCatalogTermRoot,
			Role: EngineCatalogRoleBranch,
		}, nil
	}
	if _, err := requireCatalogBusinessPath(path, model); err != nil {
		return nil, err
	}

	last := path.Segments[len(path.Segments)-1]
	if last.Kind == EngineCatalogKindFile || last.Term == EngineCatalogTermFile {
		if callbacks.GetFileStorageFactsFunc == nil {
			return nil, fmt.Errorf("file catalog callbacks GetFileStorageFactsFunc is nil")
		}
		storageFacts, err := callbacks.GetFileStorageFactsFunc(ctx, connInfo, path.StringPath())
		if err != nil {
			return nil, err
		}
		return fileStorageFactsCatalogEntry(engineID, path, storageFacts), nil
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

// DescribeFileCatalogFacts maps file storage facts to EngineCatalogFactsProvider output.
func DescribeFileCatalogFacts(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path EngineCatalogPath) (*EngineCatalogFacts, error) {
	path = NormalizeFileCatalogSegments(path)
	if _, err := requireCatalogBusinessPath(path, FileCatalogModel()); err != nil {
		return nil, err
	}
	if callbacks.GetFileStorageFactsFunc == nil {
		return nil, fmt.Errorf("file catalog callbacks GetFileStorageFactsFunc is nil")
	}
	storageFacts, err := callbacks.GetFileStorageFactsFunc(ctx, connInfo, path.StringPath())
	if err != nil {
		return nil, err
	}
	updatedAt := storageFacts.ModifiedAt
	sizeBytes := storageFacts.Size
	return &EngineCatalogFacts{
		Path: path,
		Kind: fileKindFromPath(path),
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

func FileDirectoryCatalogEntry(parent EngineCatalogPath, name, storagePath string) EngineCatalogEntry {
	return EngineCatalogEntry{
		Name: name,
		Path: appendCatalogSegment(parent, parent.EngineID, EngineCatalogTermDirectory, EngineCatalogKindDirectory, name),
		Term: EngineCatalogTermDirectory,
		Kind: EngineCatalogKindDirectory,
		Role: EngineCatalogRoleBranch,
		Storage: &EngineCatalogStorageFacts{
			Path: NormalizeFileCatalogPath(storagePath),
		},
	}
}

func FileLeafCatalogEntry(parent EngineCatalogPath, facts StorageObjectFacts) EngineCatalogEntry {
	sizeBytes := facts.Size
	updatedAt := facts.ModifiedAt
	return EngineCatalogEntry{
		Name: facts.Name,
		Path: appendCatalogSegment(parent, parent.EngineID, EngineCatalogTermFile, EngineCatalogKindFile, facts.Name),
		Term: EngineCatalogTermFile,
		Kind: EngineCatalogKindFile,
		Role: EngineCatalogRoleLeaf,
		Storage: EngineCatalogEntryStorageSummary(&EngineCatalogStorageFacts{
			Path:        NormalizeFileCatalogPath(facts.Path),
			ContentType: facts.ContentType,
			ETag:        facts.ETag,
			SizeBytes:   &sizeBytes,
		}),
		UpdatedAt: &updatedAt,
	}
}

func fileKindFromPath(path EngineCatalogPath) string {
	last := path.Segments[len(path.Segments)-1]
	if last.Kind != "" {
		return last.Kind
	}
	return EngineCatalogKindFile
}

func fileStorageFactsCatalogEntry(engineID uint, path EngineCatalogPath, facts *StorageObjectFacts) *EngineCatalogEntry {
	if path.Version == "" {
		path.Version = EngineCatalogPathVersion
	}
	if path.EngineID == 0 {
		path.EngineID = engineID
	}
	term := EngineCatalogTermFile
	kind := EngineCatalogKindFile
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
