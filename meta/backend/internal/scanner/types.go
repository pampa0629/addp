package scanner

// SchemaInfo Schema信息（PostgreSQL schema / MySQL database）
type SchemaInfo struct {
	Name           string
	TableCount     int
	TotalSizeBytes int64
}

// TableInfo 表信息
type TableInfo struct {
	Name           string
	Type           string
	Comment        string
	RowCount       int64
	SizeBytes      int64
}

// FieldInfo 字段信息
type FieldInfo struct {
	Name             string
	OrdinalPosition  int
	DataType         string
	ColumnType       string
	IsNullable       bool
	DefaultValue     string
	Comment          string
	IsPrimaryKey     bool
	IsUniqueKey      bool
	CharacterSet     string
	Collation        string
	NumericPrecision int
	NumericScale     int
}

// Scanner 扫描器接口
type Scanner interface {
	// ListSchemas 列出所有Schema（PostgreSQL）或数据库（MySQL）
	ListSchemas() ([]SchemaInfo, error)
	// ScanTables 扫描指定Schema的所有表
	ScanTables(schemaName string) ([]TableInfo, error)
	// ScanFields 扫描指定表的所有字段
	ScanFields(schemaName, tableName string) ([]FieldInfo, error)
	// Close 关闭连接
	Close() error
}
