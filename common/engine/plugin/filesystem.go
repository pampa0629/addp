package plugin

import "time"

// FileEntry 文件条目
type FileEntry struct {
	Name        string
	Path        string // 完整路径，供 ContentReadableProvider 使用（格式：bucket/prefix/file.parquet）
	Size        int64
	ModifiedAt  time.Time
	ContentType string
}

// DirEntry 目录条目
type DirEntry struct {
	Name string
	Path string // 完整路径（格式：bucket/prefix/subdir/）
}

// FileMetadata 文件元数据
type FileMetadata struct {
	Name        string
	Path        string
	Size        int64
	ModifiedAt  time.Time
	ContentType string
	ETag        string
}

// RootEntry 根节点条目（Bucket/挂载点/根目录）
type RootEntry struct {
	Name string
	Path string
}
