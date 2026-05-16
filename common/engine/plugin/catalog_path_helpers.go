package plugin

import "strings"

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
	trimmed := strings.Trim(rawPath, "/")
	if trimmed == "" || trimmed == "." {
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
	trimmed := strings.Trim(rawPath, "/")
	if trimmed == "" || trimmed == "." {
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
