package plugin

import "time"

// StorageObjectFacts describes a file-like object in file systems or object storage.
type StorageObjectFacts struct {
	Name        string
	Path        string
	Size        int64
	ModifiedAt  time.Time
	ContentType string
	ETag        string
}
