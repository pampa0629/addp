package plugin

import (
	"fmt"
	"strings"
)

// CatalogRootEntry returns the structural root of an engine catalog.
// The root is part of CatalogPath but never part of the business path string.
func CatalogRootEntry(model CatalogModelSpec, engineID uint, engineName string) CatalogEntry {
	return CatalogEntry{
		Name: engineName,
		Path: CatalogRootPath(model, engineID),
		Term: model.RootTerm,
		Kind: model.RootTerm,
		Role: CatalogRoleBranch,
	}
}

// CatalogRootPath returns the explicit structural root path of an engine catalog.
func CatalogRootPath(model CatalogModelSpec, engineID uint) CatalogPath {
	return CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: engineID,
		Segments: []CatalogSegment{{
			Term: model.RootTerm,
			Kind: model.RootTerm,
		}},
	}
}

// IsCatalogRootSegment reports whether a segment is a structural catalog root.
func IsCatalogRootSegment(segment CatalogSegment) bool {
	switch segment.Term {
	case CatalogTermServer, CatalogTermService, CatalogTermRoot:
		return segment.Kind == "" || segment.Kind == segment.Term
	default:
		return false
	}
}

// IsCatalogRootPath reports whether path points at one explicit structural root.
func IsCatalogRootPath(path CatalogPath) bool {
	return len(path.Segments) == 1 && IsCatalogRootSegment(path.Segments[0])
}

// CatalogPathWithoutRoot returns the business path without the structural root.
func CatalogPathWithoutRoot(path CatalogPath) CatalogPath {
	if len(path.Segments) == 0 || !IsCatalogRootSegment(path.Segments[0]) {
		return path
	}
	path.Segments = append([]CatalogSegment(nil), path.Segments[1:]...)
	return path
}

func requireCatalogRootPath(path CatalogPath, model CatalogModelSpec) error {
	if !IsCatalogRootPath(path) || path.Segments[0].Term != model.RootTerm {
		return fmt.Errorf("catalog root path requires explicit %s root segment", model.RootTerm)
	}
	return nil
}

func requireCatalogBusinessPath(path CatalogPath, model CatalogModelSpec) ([]CatalogSegment, error) {
	if len(path.Segments) == 0 || !IsCatalogRootSegment(path.Segments[0]) || path.Segments[0].Term != model.RootTerm {
		return nil, fmt.Errorf("catalog path requires explicit %s root segment", model.RootTerm)
	}
	if len(path.Segments) == 1 {
		return nil, fmt.Errorf("catalog business path requires segments below %s root", model.RootTerm)
	}
	return path.Segments[1:], nil
}

// NormalizeFileCatalogPath maps filesystem catalog paths to ADDP semantic paths.
// The storage root is represented by an empty path; "." and "/" are only
// tolerated as external/root spellings and must not become catalog segments.
func NormalizeFileCatalogPath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" || trimmed == "." {
		return ""
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(trimmed, "/") {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

func normalizeFileCatalogRootName(name string) string {
	if NormalizeFileCatalogPath(name) == "" {
		return "/"
	}
	return strings.Trim(name, "/")
}

func NormalizeFileCatalogSegments(path CatalogPath) CatalogPath {
	if len(path.Segments) == 0 {
		return path
	}
	normalized := CatalogPath{
		Version:  path.Version,
		EngineID: path.EngineID,
		Segments: make([]CatalogSegment, 0, len(path.Segments)),
	}
	for i, segment := range path.Segments {
		if segment.Kind == CatalogKindRoot || segment.Term == CatalogTermRoot {
			segment.Name = normalizeFileCatalogRootName(segment.Name)
			normalized.Segments = append(normalized.Segments, segment)
			continue
		}
		if NormalizeFileCatalogPath(segment.Name) == "" {
			continue
		}
		if i > 0 {
			segment.Name = strings.Trim(segment.Name, "/")
		}
		normalized.Segments = append(normalized.Segments, segment)
	}
	return normalized
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

// FileRootPath returns the catalog root for a filesystem-like engine.
func FileRootPath(engineID uint) CatalogPath {
	return CatalogRootPath(FileCatalogModel(), engineID)
}

// FileDirectoryPath maps an engine-relative filesystem path to root -> directory segments.
func FileDirectoryPath(engineID uint, rawPath string) CatalogPath {
	path := FileRootPath(engineID)
	trimmed := NormalizeFileCatalogPath(rawPath)
	if trimmed == "" {
		return path
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" {
			continue
		}
		path = appendCatalogSegment(path, engineID, CatalogTermDirectory, CatalogKindDirectory, part)
	}
	return path
}

// FileItemPath maps an engine-relative filesystem path to root -> directory? -> file.
func FileItemPath(engineID uint, rawPath string) CatalogPath {
	trimmed := NormalizeFileCatalogPath(rawPath)
	if trimmed == "" {
		return FileRootPath(engineID)
	}
	parts := strings.Split(trimmed, "/")
	path := FileRootPath(engineID)
	for i, part := range parts {
		if part == "" {
			continue
		}
		term := CatalogTermDirectory
		kind := CatalogKindDirectory
		if i == len(parts)-1 {
			term = CatalogTermFile
			kind = CatalogKindFile
		}
		path = appendCatalogSegment(path, engineID, term, kind, part)
	}
	return path
}

func FileItemPathForEngine(engineID uint) func(path string) CatalogPath {
	return func(path string) CatalogPath {
		return FileItemPath(engineID, path)
	}
}

// ObjectDirectoryPath maps bucket/prefix coordinates to bucket -> prefix segments.
func ObjectDirectoryPath(engineID uint, bucket, prefix string) CatalogPath {
	return buildObjectPath(engineID, bucket, prefix, true)
}

// ObjectItemPath maps bucket/object coordinates to bucket -> prefix? -> object segments.
func ObjectItemPath(engineID uint, bucket, objectPath string) CatalogPath {
	return buildObjectPath(engineID, bucket, objectPath, false)
}

func ObjectItemPathForBucket(engineID uint, bucket string) func(path string) CatalogPath {
	return func(path string) CatalogPath {
		return ObjectItemPath(engineID, bucket, path)
	}
}

func buildObjectPath(engineID uint, bucket, objectPath string, isContainer bool) CatalogPath {
	path := CatalogRootPath(ObjectCatalogModel(), engineID)
	bucket = strings.Trim(bucket, "/")
	if bucket == "" {
		return path
	}
	path = appendCatalogSegment(path, engineID, CatalogTermBucket, CatalogKindBucket, bucket)
	trimmed := strings.Trim(objectPath, "/")
	if trimmed == "" {
		return path
	}
	parts := strings.Split(trimmed, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		term := CatalogTermPrefix
		kind := CatalogKindPrefix
		if i == len(parts)-1 && !isContainer {
			term = CatalogTermObject
			kind = CatalogKindObject
		}
		path = appendCatalogSegment(path, engineID, term, kind, part)
	}
	return path
}

// ObjectRootPath returns the structural service root for object storage.
func ObjectRootPath(engineID uint) CatalogPath {
	return CatalogRootPath(ObjectCatalogModel(), engineID)
}

// TabularNamespacePath returns server -> namespace for a tabular engine.
func TabularNamespacePath(engineID uint, namespaceTerm, namespace string) CatalogPath {
	return appendCatalogSegment(CatalogRootPath(TabularCatalogModel(namespaceTerm), engineID), engineID, namespaceTerm, CatalogKindNamespace, namespace)
}

// TabularItemPath returns root -> namespace -> table for tabular engines.
func TabularItemPath(engineID uint, namespaceTerm, namespace, table string) CatalogPath {
	path := TabularNamespacePath(engineID, namespaceTerm, namespace)
	return appendCatalogSegment(path, engineID, CatalogTermTable, CatalogKindTable, table)
}

// BranchCatalogPath returns root -> branch for branch/leaf engines.
func BranchCatalogPath(model CatalogModelSpec, engineID uint, branchTerm, branchName string) CatalogPath {
	branchKind := CatalogKindNamespace
	if branchLevel, ok := CatalogFirstBusinessBranch(model); ok && len(branchLevel.Kinds) > 0 && branchLevel.Kinds[0] != "" {
		branchKind = branchLevel.Kinds[0]
	}
	return appendCatalogSegment(CatalogRootPath(model, engineID), engineID, branchTerm, branchKind, branchName)
}

// BranchLeafCatalogPath returns root -> branch -> leaf for branch/leaf engines.
func BranchLeafCatalogPath(model CatalogModelSpec, engineID uint, branchTerm, branchName, leafTerm, leafKind, leafName string) CatalogPath {
	path := BranchCatalogPath(model, engineID, branchTerm, branchName)
	return appendCatalogSegment(path, engineID, leafTerm, leafKind, leafName)
}

// RequireObjectLeafPath validates that path points to an object-storage leaf.
func RequireObjectLeafPath(path CatalogPath) (string, error) {
	segments, err := requireCatalogBusinessPath(path, ObjectCatalogModel())
	if err != nil {
		return "", err
	}
	last := segments[len(segments)-1]
	if last.Term != CatalogTermObject && last.Kind != CatalogKindObject {
		return "", fmt.Errorf("object content path requires object leaf")
	}
	objectPath := path.StringPath()
	if objectPath == "" {
		return "", fmt.Errorf("object content path cannot be empty")
	}
	return objectPath, nil
}

// RequireFileLeafPath validates that path points to a filesystem leaf.
func RequireFileLeafPath(path CatalogPath) (string, error) {
	path = NormalizeFileCatalogSegments(path)
	segments, err := requireCatalogBusinessPath(path, FileCatalogModel())
	if err != nil {
		return "", err
	}
	last := segments[len(segments)-1]
	if last.Term != CatalogTermFile && last.Kind != CatalogKindFile {
		return "", fmt.Errorf("file content path requires file leaf")
	}
	filePath := path.StringPath()
	if filePath == "" {
		return "", fmt.Errorf("file content path cannot be empty")
	}
	return filePath, nil
}
