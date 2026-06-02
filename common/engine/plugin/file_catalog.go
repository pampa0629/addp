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
	ListDirectoryFunc       func(ctx context.Context, connInfo ConnectionInfo, parent CatalogPath) ([]CatalogEntry, error)
	GetFileStorageFactsFunc func(ctx context.Context, connInfo ConnectionInfo, path string) (*StorageObjectFacts, error)
}

// FileCatalogModel describes file-system hierarchy: root -> directory? -> file.
func FileCatalogModel() CatalogModelSpec {
	return CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    CatalogTermRoot,
		Levels: []CatalogLevelSpec{
			{Term: CatalogTermDirectory, Kinds: []string{CatalogKindDirectory}, Role: CatalogRoleBranch, Optional: true, I18nKey: CatalogTermI18nKey(CatalogTermDirectory)},
			{Term: CatalogTermFile, Kinds: []string{CatalogKindFile}, Role: CatalogRoleLeaf, I18nKey: CatalogTermI18nKey(CatalogTermFile)},
		},
	}
}

// ListFileCatalogChildren maps filesystem roots, directories and files to CatalogProvider nodes.
func ListFileCatalogChildren(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, parent CatalogPath, opts ListOptions) ([]CatalogEntry, error) {
	parent = NormalizeFileCatalogSegments(parent)
	model := FileCatalogModel()
	if IsCatalogRootPath(parent) {
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

func listFileCatalogChildren(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogEntry, error) {
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
	result := append([]CatalogEntry(nil), nodes...)
	for _, node := range nodes {
		if node.Role != CatalogRoleBranch {
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
func ResolveFileCatalogPath(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*CatalogEntry, error) {
	path = NormalizeFileCatalogSegments(path)
	model := FileCatalogModel()
	if IsCatalogRootPath(path) {
		if err := requireCatalogRootPath(path, model); err != nil {
			return nil, err
		}
		return &CatalogEntry{
			Name: "",
			Path: path,
			Term: CatalogTermRoot,
			Kind: CatalogTermRoot,
			Role: CatalogRoleBranch,
		}, nil
	}
	if _, err := requireCatalogBusinessPath(path, model); err != nil {
		return nil, err
	}

	last := path.Segments[len(path.Segments)-1]
	if last.Kind == CatalogKindFile || last.Term == CatalogTermFile {
		if callbacks.GetFileStorageFactsFunc == nil {
			return nil, fmt.Errorf("file catalog callbacks GetFileStorageFactsFunc is nil")
		}
		meta, err := callbacks.GetFileStorageFactsFunc(ctx, connInfo, path.StringPath())
		if err != nil {
			return nil, err
		}
		return fileStorageFactsCatalogEntry(engineID, path, meta), nil
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

// DescribeFileCatalogFacts maps file storage facts to CatalogFactsProvider output.
func DescribeFileCatalogFacts(ctx context.Context, callbacks FileCatalogCallbacks, connInfo ConnectionInfo, engineID uint, path CatalogPath) (*CatalogFacts, error) {
	if callbacks.GetFileStorageFactsFunc == nil {
		return nil, fmt.Errorf("file catalog callbacks GetFileStorageFactsFunc is nil")
	}
	path = NormalizeFileCatalogSegments(path)
	if _, err := requireCatalogBusinessPath(path, FileCatalogModel()); err != nil {
		return nil, err
	}
	meta, err := callbacks.GetFileStorageFactsFunc(ctx, connInfo, path.StringPath())
	if err != nil {
		return nil, err
	}
	updatedAt := meta.ModifiedAt
	sizeBytes := meta.Size
	return &CatalogFacts{
		Path: path,
		Kind: fileKindFromPath(path),
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

func FileDirectoryCatalogEntry(parent CatalogPath, name, storagePath string) CatalogEntry {
	return CatalogEntry{
		Name: name,
		Path: appendCatalogSegment(parent, parent.EngineID, CatalogTermDirectory, CatalogKindDirectory, name),
		Term: CatalogTermDirectory,
		Kind: CatalogKindDirectory,
		Role: CatalogRoleBranch,
		Storage: &CatalogStorageFacts{
			Path: NormalizeFileCatalogPath(storagePath),
		},
	}
}

func FileLeafCatalogEntry(parent CatalogPath, facts StorageObjectFacts) CatalogEntry {
	sizeBytes := facts.Size
	updatedAt := facts.ModifiedAt
	return CatalogEntry{
		Name: facts.Name,
		Path: appendCatalogSegment(parent, parent.EngineID, CatalogTermFile, CatalogKindFile, facts.Name),
		Term: CatalogTermFile,
		Kind: CatalogKindFile,
		Role: CatalogRoleLeaf,
		Storage: &CatalogStorageFacts{
			Path:        NormalizeFileCatalogPath(facts.Path),
			ContentType: facts.ContentType,
			ETag:        facts.ETag,
			SizeBytes:   &sizeBytes,
		},
		UpdatedAt: &updatedAt,
	}
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

func fileStorageFactsCatalogEntry(engineID uint, path CatalogPath, meta *StorageObjectFacts) *CatalogEntry {
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
