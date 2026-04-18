package plugin

import (
	"context"
	"io"
	"time"
)

// FileSystemPlugin 文件系统语义存储的统一接口
// 所有基于文件系统语义的存储（对象存储、NAS、HDFS）都应实现此接口
// ObjectStoragePlugin 继承此接口
type FileSystemPlugin interface {
	StoragePlugin

	// ListDirectory 列出路径下的直接子内容（非递归）
	// path 格式：bucket/prefix/（对象存储），/mount/path/（NAS）
	ListDirectory(ctx context.Context, connInfo ConnectionInfo, path string) (files []FileEntry, subdirs []DirEntry, err error)

	// ReadFile 流式读取文件内容（调用方负责关闭）
	ReadFile(ctx context.Context, connInfo ConnectionInfo, path string) (io.ReadCloser, error)

	// GetFileMetadata 获取文件元数据
	GetFileMetadata(ctx context.Context, connInfo ConnectionInfo, path string) (*FileMetadata, error)

	// ListRoots 列出根节点（对象存储=Bucket，NAS=挂载点，HDFS=根目录）
	ListRoots(ctx context.Context, connInfo ConnectionInfo) ([]RootEntry, error)
}

// FileEntry 文件条目
type FileEntry struct {
	Name        string
	Path        string    // 完整路径，供 ReadFile 使用（格式：bucket/prefix/file.parquet）
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
