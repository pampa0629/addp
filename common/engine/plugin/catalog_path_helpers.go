package plugin

import "strings"

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
	return appendCatalogSegment(CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: engineID,
	}, engineID, CatalogTermRoot, CatalogKindRoot, "/")
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
	path := CatalogPath{
		Version:  CatalogPathVersion,
		EngineID: engineID,
	}
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
