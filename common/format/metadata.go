package format

import "time"

// ObjectMetadata 表示对象存储或文件系统扫描得到的轻量资源元数据。
type ObjectMetadata struct {
	Bucket              string
	Path                string
	NodeType            string
	FileType            string
	SizeBytes           int64
	ObjectCount         int64
	LastModified        *time.Time
	ExtractedAttributes map[string]interface{}
}
