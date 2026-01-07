package format

import "github.com/addp/common/engine/plugin"

// DocCollectionInfo 文档数据库集合扩展信息
// 存储 MongoDB、CouchDB 等文档数据库的特有元数据
type DocCollectionInfo struct {
	IsSampled      bool               // 是否为采样推断（true: Schema 不完整）
	SampleSize     int                // 采样大小（采样的文档数量）
	SchemaType     string             // Schema 类型: "dynamic" (MongoDB) | "flexible" (CouchDB)
	Indexes        []plugin.IndexInfo // 索引列表（复用 plugin.IndexInfo）
	TotalDocuments int64              // 总文档数
}

// ExtensionType 实现 ExtensionInfo 接口
func (d *DocCollectionInfo) ExtensionType() string {
	return "document_collection"
}
