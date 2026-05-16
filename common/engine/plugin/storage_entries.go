package plugin

import "time"

// FileEntry describes a file-like leaf in file systems or object storage.
type FileEntry struct {
	Name        string
	Path        string // Complete physical path, e.g. bucket/prefix/file.parquet.
	CatalogPath CatalogPath
	Size        int64
	ModifiedAt  time.Time
	ContentType string
}

// DirEntry describes a directory-like container or object storage prefix.
type DirEntry struct {
	Name        string
	Path        string // Complete physical path, e.g. bucket/prefix/subdir/.
	CatalogPath CatalogPath
}

// FileMetadata describes a file-like object in file systems or object storage.
type FileMetadata struct {
	Name        string
	Path        string
	Size        int64
	ModifiedAt  time.Time
	ContentType string
	ETag        string
}

// RootEntry describes a storage root, such as a bucket, mount point, or filesystem root.
type RootEntry struct {
	Name string
	Path string
}
