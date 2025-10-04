package scanner

// DatabaseInfo 数据库信息
type DatabaseInfo struct {
	Name           string
	Charset        string
	Collation      string
	TableCount     int
	TotalSizeBytes int64
}

// TableInfo 表信息
type TableInfo struct {
	Schema         string
	Name           string
	Type           string
	Engine         string
	RowCount       int64
	DataSize       int64
	IndexSize      int64
	Comment        string
}

// FieldInfo 字段信息
type FieldInfo struct {
	Name         string
	Position     int
	DataType     string
	ColumnType   string
	IsNullable   bool
	ColumnKey    string
	DefaultValue string
	Extra        string
	Comment      string
}

// Scanner 扫描器接口
type Scanner interface {
	ScanDatabases() ([]DatabaseInfo, error)
	ScanTables(database string) ([]TableInfo, error)
	ScanFields(database, table string) ([]FieldInfo, error)
	Close() error
}
