package format

const (
	ContentIndexKindSparseRow = "sparse_row_index"
	ContentIndexDataTypeTable = "table"
	ContentIndexUnitRow       = "row"
	ContentIndexOffsetByte    = "byte"
)

// ContentIndex 描述面向内容读取的通用访问索引。
//
// 索引本身不是 TableInfo 的核心语义，也不是格式私有元数据；
// 上层通常将其写入 attributes.content_index.<data_type>。
type ContentIndex struct {
	Kind        string                 `json:"kind"`
	DataType    string                 `json:"data_type,omitempty"`
	Format      string                 `json:"format,omitempty"`
	Unit        string                 `json:"unit,omitempty"`
	OffsetUnit  string                 `json:"offset_unit,omitempty"`
	Step        int64                  `json:"step,omitempty"`
	RowCount    int64                  `json:"row_count,omitempty"`
	HeaderBytes int64                  `json:"header_bytes,omitempty"`
	Source      map[string]interface{} `json:"source,omitempty"`
	Anchors     []ContentIndexAnchor   `json:"anchors,omitempty"`
}

type ContentIndexAnchor struct {
	Row        int64 `json:"row"`
	ByteOffset int64 `json:"byte_offset"`
}

// ContentIndexInfo 允许 info provider 在返回 TableInfo 时夹带通用访问索引。
// Meta 层消费后应将其写入 attributes.content_index，而不是 format_info。
type ContentIndexInfo struct {
	Table *ContentIndex
}

func (t *TableInfo) GetContentIndexInfo() *ContentIndexInfo {
	if t == nil {
		return nil
	}
	return t.ContentIndex
}
