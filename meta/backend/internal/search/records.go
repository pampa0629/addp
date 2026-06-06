package search

import "time"

// FieldRecord 用于索引字段信息
type FieldRecord struct {
	Name            string `json:"name"`
	DataType        string `json:"data_type,omitempty"`
	ColumnType      string `json:"column_type,omitempty"`
	Comment         string `json:"comment,omitempty"`
	OrdinalPosition int    `json:"ordinal_position,omitempty"`
	IsNullable      bool   `json:"nullable,omitempty"`
	IsPrimaryKey    bool   `json:"primary_key,omitempty"`
	IsUniqueKey     bool   `json:"is_unique_key,omitempty"`
}

// AssetRecord 统一资产记录（包含表、catalog leaf、文档内容）
// 基础扫描只填充基本字段，深度扫描填充完整内容
type AssetRecord struct {
	// ===== 基础字段（所有资产，基础扫描即写） =====
	AssetID     string   `json:"asset_id"`
	DocumentID  string   `json:"document_id,omitempty"`  // item fingerprint，用于全文/向量检索结果合并
	ContentHash string   `json:"content_hash,omitempty"` // storage.content_hash，用于判断内容是否变化
	Locator     string   `json:"locator,omitempty"`      // 标准 ResourceLocator URI
	TenantID    uint     `json:"tenant_id"`
	EngineID    uint     `json:"engine_id"`
	EngineName  string   `json:"engine_name,omitempty"`
	EngineType  string   `json:"engine_type,omitempty"`
	AssetType   string   `json:"asset_type"` // "table" | catalog leaf type, e.g. "object" or "file"
	Name        string   `json:"name"`
	FullName    string   `json:"full_name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`

	// ===== 表特有字段 =====
	Schema    string        `json:"schema,omitempty"`
	TableKind string        `json:"table_kind,omitempty"`
	Fields    []FieldRecord `json:"fields,omitempty"`
	RowCount  *int64        `json:"row_count,omitempty"`

	// ===== 内容资源字段 =====
	Bucket        string     `json:"bucket,omitempty"`
	Path          string     `json:"path,omitempty"` // 目录路径（如 "image/"）
	SizeBytes     *int64     `json:"size_bytes,omitempty"`
	ContentType   string     `json:"content_type,omitempty"`
	DataUpdatedAt *time.Time `json:"data_updated_at,omitempty"`

	// ===== 文档内容字段（深度扫描才写） =====
	Content        string     `json:"content,omitempty"`         // 全文内容
	ContentPreview string     `json:"content_preview,omitempty"` // 内容预览
	DocumentType   string     `json:"document_type,omitempty"`   // pdf/docx/txt
	Title          string     `json:"title,omitempty"`
	Author         string     `json:"author,omitempty"`
	Keywords       []string   `json:"keywords,omitempty"`
	WordCount      int        `json:"word_count,omitempty"`
	PageCount      int        `json:"page_count,omitempty"`
	CreatedDate    *time.Time `json:"created_date,omitempty"`
	ModifiedDate   *time.Time `json:"modified_date,omitempty"`

	// ===== 通用字段 =====
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	UpdatedAt time.Time              `json:"updated_at"`
}
