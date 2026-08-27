package plugin

import (
	"fmt"
	"strings"
)

// EngineCatalogRootEntry returns the structural root of an engine catalog.
// The root is part of EngineCatalogPath but never part of the business path string.
func EngineCatalogRootEntry(model EngineCatalogModelSpec, engineID uint, engineName string) EngineCatalogEntry {
	return EngineCatalogEntry{
		Name: engineName,
		Path: EngineCatalogRootPath(model, engineID),
		Term: model.RootTerm,
		Kind: model.RootTerm,
		Role: EngineCatalogRoleBranch,
	}
}

// EngineCatalogRootPath returns the explicit structural root path of an engine catalog.
func EngineCatalogRootPath(model EngineCatalogModelSpec, engineID uint) EngineCatalogPath {
	return EngineCatalogPath{
		Version:  EngineCatalogPathVersion,
		EngineID: engineID,
		Segments: []EngineCatalogSegment{{
			Term: model.RootTerm,
			Kind: model.RootTerm,
		}},
	}
}

// IsEngineCatalogRootSegment reports whether a segment is a structural catalog root.
func IsEngineCatalogRootSegment(segment EngineCatalogSegment) bool {
	switch segment.Term {
	case EngineCatalogTermServer, EngineCatalogTermService, EngineCatalogTermRoot:
		return segment.Kind == "" || segment.Kind == segment.Term
	default:
		return false
	}
}

// IsEngineCatalogRootPath reports whether path points at one explicit structural root.
func IsEngineCatalogRootPath(path EngineCatalogPath) bool {
	return len(path.Segments) == 1 && IsEngineCatalogRootSegment(path.Segments[0])
}

// EngineCatalogPathWithoutRoot returns the business path without the structural root.
func EngineCatalogPathWithoutRoot(path EngineCatalogPath) EngineCatalogPath {
	if len(path.Segments) == 0 || !IsEngineCatalogRootSegment(path.Segments[0]) {
		return path
	}
	path.Segments = append([]EngineCatalogSegment(nil), path.Segments[1:]...)
	return path
}

func requireCatalogRootPath(path EngineCatalogPath, model EngineCatalogModelSpec) error {
	if !IsEngineCatalogRootPath(path) || path.Segments[0].Term != model.RootTerm {
		return WrapEngineCatalogError(EngineCatalogErrorInvalidPath, fmt.Errorf("catalog root path requires explicit %s root segment", model.RootTerm))
	}
	if path.Version != EngineCatalogPathVersion {
		return WrapEngineCatalogError(EngineCatalogErrorInvalidPath, fmt.Errorf("unsupported catalog path version %q", path.Version))
	}
	return nil
}

func requireCatalogBusinessPath(path EngineCatalogPath, model EngineCatalogModelSpec) ([]EngineCatalogSegment, error) {
	if len(path.Segments) == 0 || !IsEngineCatalogRootSegment(path.Segments[0]) || path.Segments[0].Term != model.RootTerm {
		return nil, WrapEngineCatalogError(EngineCatalogErrorInvalidPath, fmt.Errorf("catalog path requires explicit %s root segment", model.RootTerm))
	}
	if len(path.Segments) == 1 {
		return nil, WrapEngineCatalogError(EngineCatalogErrorInvalidPath, fmt.Errorf("catalog business path requires segments below %s root", model.RootTerm))
	}
	if path.Version != EngineCatalogPathVersion {
		return nil, WrapEngineCatalogError(EngineCatalogErrorInvalidPath, fmt.Errorf("unsupported catalog path version %q", path.Version))
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

func NormalizeFileCatalogSegments(path EngineCatalogPath) EngineCatalogPath {
	if len(path.Segments) == 0 {
		return path
	}
	normalized := EngineCatalogPath{
		Version:  path.Version,
		EngineID: path.EngineID,
		Segments: make([]EngineCatalogSegment, 0, len(path.Segments)),
	}
	for i, segment := range path.Segments {
		if segment.Kind == EngineCatalogKindRoot || segment.Term == EngineCatalogTermRoot {
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

func appendCatalogSegment(parent EngineCatalogPath, engineID uint, term, kind, name string) EngineCatalogPath {
	next := EngineCatalogPath{
		Version:  parent.Version,
		EngineID: parent.EngineID,
		Segments: append([]EngineCatalogSegment{}, parent.Segments...),
	}
	if next.Version == "" {
		next.Version = EngineCatalogPathVersion
	}
	if next.EngineID == 0 {
		next.EngineID = engineID
	}
	next.Segments = append(next.Segments, EngineCatalogSegment{Term: term, Kind: kind, Name: name})
	return next
}

// FileRootPath returns the catalog root for a filesystem-like engine.
func FileRootPath(engineID uint) EngineCatalogPath {
	return EngineCatalogRootPath(FileCatalogModel(), engineID)
}

// FileDirectoryPath maps an engine-relative filesystem path to root -> directory segments.
func FileDirectoryPath(engineID uint, rawPath string) EngineCatalogPath {
	path := FileRootPath(engineID)
	trimmed := NormalizeFileCatalogPath(rawPath)
	if trimmed == "" {
		return path
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" {
			continue
		}
		path = appendCatalogSegment(path, engineID, EngineCatalogTermDirectory, EngineCatalogKindDirectory, part)
	}
	return path
}

// FileItemPath maps an engine-relative filesystem path to root -> directory? -> file.
func FileItemPath(engineID uint, rawPath string) EngineCatalogPath {
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
		term := EngineCatalogTermDirectory
		kind := EngineCatalogKindDirectory
		if i == len(parts)-1 {
			term = EngineCatalogTermFile
			kind = EngineCatalogKindFile
		}
		path = appendCatalogSegment(path, engineID, term, kind, part)
	}
	return path
}

func FileItemPathForEngine(engineID uint) func(path string) EngineCatalogPath {
	return func(path string) EngineCatalogPath {
		return FileItemPath(engineID, path)
	}
}

// ObjectDirectoryPath maps bucket/prefix coordinates to bucket -> prefix segments.
func ObjectDirectoryPath(engineID uint, bucket, prefix string) EngineCatalogPath {
	return buildObjectPath(engineID, bucket, prefix, true)
}

// ObjectItemPath maps bucket/object coordinates to bucket -> prefix? -> object segments.
func ObjectItemPath(engineID uint, bucket, objectPath string) EngineCatalogPath {
	return buildObjectPath(engineID, bucket, objectPath, false)
}

func ObjectItemPathForBucket(engineID uint, bucket string) func(path string) EngineCatalogPath {
	return func(path string) EngineCatalogPath {
		return ObjectItemPath(engineID, bucket, path)
	}
}

// SplitObjectRefPath parses an external object ref path in bucket/object form.
// The returned objectPath is bucket-relative and never includes the bucket.
func SplitObjectRefPath(refPath string) (bucket, objectPath string, err error) {
	trimmed := strings.Trim(strings.TrimSpace(refPath), "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("object ref path must be bucket/object: %s", refPath)
	}
	bucket = strings.TrimSpace(parts[0])
	objectPath = strings.Trim(parts[1], "/")
	if bucket == "" || strings.TrimSpace(objectPath) == "" {
		return "", "", fmt.Errorf("object ref path must be bucket/object: %s", refPath)
	}
	return bucket, objectPath, nil
}

// ObjectItemPathFromRefPath maps an external bucket/object ref to an object
// catalog leaf path.
func ObjectItemPathFromRefPath(engineID uint, refPath string) (EngineCatalogPath, error) {
	bucket, objectPath, err := SplitObjectRefPath(refPath)
	if err != nil {
		return EngineCatalogPath{}, err
	}
	return ObjectItemPath(engineID, bucket, objectPath), nil
}

// ObjectItemPathFromBucketRef maps either a bucket-relative object key or an
// external bucket/object ref under the same bucket to one object catalog path.
// Use it at boundaries where metadata paths may already be bucket-qualified.
func ObjectItemPathFromBucketRef(engineID uint, bucket, pathValue string) EngineCatalogPath {
	bucket = strings.Trim(bucket, "/")
	objectPath := strings.Trim(pathValue, "/")
	if refBucket, parsedPath, err := SplitObjectRefPath(objectPath); err == nil && refBucket == bucket {
		objectPath = parsedPath
	}
	return ObjectItemPath(engineID, bucket, objectPath)
}

func ObjectItemPathForBucketRef(engineID uint, bucket string) func(path string) EngineCatalogPath {
	return func(path string) EngineCatalogPath {
		return ObjectItemPathFromBucketRef(engineID, bucket, path)
	}
}

func buildObjectPath(engineID uint, bucket, objectPath string, isContainer bool) EngineCatalogPath {
	path := EngineCatalogRootPath(ObjectCatalogModel(), engineID)
	bucket = strings.Trim(bucket, "/")
	if bucket == "" {
		return path
	}
	path = appendCatalogSegment(path, engineID, EngineCatalogTermBucket, EngineCatalogKindBucket, bucket)
	trimmed := strings.Trim(objectPath, "/")
	if trimmed == "" {
		return path
	}
	parts := strings.Split(trimmed, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		term := EngineCatalogTermPrefix
		kind := EngineCatalogKindPrefix
		if i == len(parts)-1 && !isContainer {
			term = EngineCatalogTermObject
			kind = EngineCatalogKindObject
		}
		path = appendCatalogSegment(path, engineID, term, kind, part)
	}
	return path
}

// ObjectRootPath returns the structural service root for object storage.
func ObjectRootPath(engineID uint) EngineCatalogPath {
	return EngineCatalogRootPath(ObjectCatalogModel(), engineID)
}

// TabularNamespacePath returns server -> namespace for a tabular engine.
func TabularNamespacePath(engineID uint, namespaceTerm, namespace string) EngineCatalogPath {
	return appendCatalogSegment(EngineCatalogRootPath(TabularCatalogModel(namespaceTerm), engineID), engineID, namespaceTerm, EngineCatalogKindNamespace, namespace)
}

// TabularItemPath returns root -> namespace -> table for tabular engines.
func TabularItemPath(engineID uint, namespaceTerm, namespace, table string) EngineCatalogPath {
	path := TabularNamespacePath(engineID, namespaceTerm, namespace)
	return appendCatalogSegment(path, engineID, EngineCatalogTermTable, EngineCatalogKindTable, table)
}

// EngineCatalogBranchPath returns root -> branch for branch/leaf engines.
func EngineCatalogBranchPath(model EngineCatalogModelSpec, engineID uint, branchTerm, branchName string) EngineCatalogPath {
	branchKind := EngineCatalogKindNamespace
	if branchLevel, ok := EngineCatalogFirstBusinessBranch(model); ok && len(branchLevel.Kinds) > 0 && branchLevel.Kinds[0] != "" {
		branchKind = branchLevel.Kinds[0]
	}
	return appendCatalogSegment(EngineCatalogRootPath(model, engineID), engineID, branchTerm, branchKind, branchName)
}

// EngineCatalogBranchLeafPath returns root -> branch -> leaf for branch/leaf engines.
func EngineCatalogBranchLeafPath(model EngineCatalogModelSpec, engineID uint, branchTerm, branchName, leafTerm, leafKind, leafName string) EngineCatalogPath {
	path := EngineCatalogBranchPath(model, engineID, branchTerm, branchName)
	return appendCatalogSegment(path, engineID, leafTerm, leafKind, leafName)
}

// RequireObjectLeafPath validates that path points to an object-storage leaf.
func RequireObjectLeafPath(path EngineCatalogPath) (string, error) {
	segments, err := requireCatalogBusinessPath(path, ObjectCatalogModel())
	if err != nil {
		return "", err
	}
	last := segments[len(segments)-1]
	if last.Term != EngineCatalogTermObject && last.Kind != EngineCatalogKindObject {
		return "", fmt.Errorf("object content path requires object leaf")
	}
	objectPath := path.StringPath()
	if objectPath == "" {
		return "", fmt.Errorf("object content path cannot be empty")
	}
	return objectPath, nil
}

// RequireFileLeafPath validates that path points to a filesystem leaf.
func RequireFileLeafPath(path EngineCatalogPath) (string, error) {
	path = NormalizeFileCatalogSegments(path)
	segments, err := requireCatalogBusinessPath(path, FileCatalogModel())
	if err != nil {
		return "", err
	}
	last := segments[len(segments)-1]
	if last.Term != EngineCatalogTermFile && last.Kind != EngineCatalogKindFile {
		return "", fmt.Errorf("file content path requires file leaf")
	}
	filePath := path.StringPath()
	if filePath == "" {
		return "", fmt.Errorf("file content path cannot be empty")
	}
	return filePath, nil
}
