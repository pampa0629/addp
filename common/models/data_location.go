package models

// DataLocation 数据位置定义（输入/输出统一使用）
// 用于在 Orchestrator → Develop → GeoPython Workflow 的数据流中传递数据位置信息
type DataLocation struct {
	// 数据源位置类型；"file" 表示存储路径位置，不是 ADDP data_type=file。
	SourceType string `json:"source_type"` // "table"|"file"|"geojson"|"reference"

	// 存储引擎 ID（除了 geojson 和 reference）
	EngineID *uint `json:"engine_id,omitempty"`

	// 类型特定参数（灵活扩展）
	Params map[string]interface{} `json:"params"`

	// 常用参数说明（实际存储在 Params 中）:
	// - table: string - 表名
	// - schema: string - Schema 名（PostgreSQL）
	// - database: string - 数据库名（MySQL/Doris）
	// - path: string - 文件路径（MinIO/S3）
	// - format: string - 文件格式（geojson/shapefile/geotiff 等）
	// - geojson: object - GeoJSON 对象（直接传递数据）
	// - reference: string - 引用路径（如 "task_123.output"）
}

// NewTableLocation 创建表类型的数据位置
func NewTableLocation(engineID uint, table string, schema string) *DataLocation {
	if schema == "" {
		schema = "public"
	}
	return &DataLocation{
		SourceType: "table",
		EngineID:   &engineID,
		Params: map[string]interface{}{
			"table":  table,
			"schema": schema,
		},
	}
}

// NewFileLocation 创建存储路径型数据位置；这里的 file 是位置类型，不是 ADDP DataType。
func NewFileLocation(engineID uint, path string, format string) *DataLocation {
	return &DataLocation{
		SourceType: "file",
		EngineID:   &engineID,
		Params: map[string]interface{}{
			"path":   path,
			"format": format,
		},
	}
}

// NewGeoJSONLocation 创建 GeoJSON 类型的数据位置（直接传递数据）
func NewGeoJSONLocation(geojson map[string]interface{}) *DataLocation {
	return &DataLocation{
		SourceType: "geojson",
		Params: map[string]interface{}{
			"geojson": geojson,
		},
	}
}

// NewReferenceLocation 创建引用类型的数据位置（引用其他任务输出）
func NewReferenceLocation(reference string) *DataLocation {
	return &DataLocation{
		SourceType: "reference",
		Params: map[string]interface{}{
			"reference": reference,
		},
	}
}

// GetParam 获取参数值（类型安全）
func (d *DataLocation) GetParam(key string) interface{} {
	if d.Params == nil {
		return nil
	}
	return d.Params[key]
}

// GetStringParam 获取字符串类型参数
func (d *DataLocation) GetStringParam(key string) string {
	val := d.GetParam(key)
	if val == nil {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return ""
}

// GetIntParam 获取整数类型参数
func (d *DataLocation) GetIntParam(key string) int {
	val := d.GetParam(key)
	if val == nil {
		return 0
	}
	// 处理 float64（JSON 反序列化时数字默认为 float64）
	if f, ok := val.(float64); ok {
		return int(f)
	}
	if i, ok := val.(int); ok {
		return i
	}
	return 0
}

// SetParam 设置参数值
func (d *DataLocation) SetParam(key string, value interface{}) {
	if d.Params == nil {
		d.Params = make(map[string]interface{})
	}
	d.Params[key] = value
}

// Validate 验证 DataLocation 是否有效
func (d *DataLocation) Validate() error {
	if d.SourceType == "" {
		return ErrInvalidDataLocation("source_type is required")
	}

	switch d.SourceType {
	case "table":
		// 表类型必须有 engine_id 和 table
		if d.EngineID == nil {
			return ErrInvalidDataLocation("engine_id is required for table source")
		}
		if d.GetStringParam("table") == "" {
			return ErrInvalidDataLocation("table name is required for table source")
		}

	case "file":
		// 存储路径位置必须有 engine_id 和 path
		if d.EngineID == nil {
			return ErrInvalidDataLocation("engine_id is required for file source")
		}
		if d.GetStringParam("path") == "" {
			return ErrInvalidDataLocation("path is required for file source")
		}

	case "geojson":
		// GeoJSON 类型必须有 geojson 对象
		if d.GetParam("geojson") == nil {
			return ErrInvalidDataLocation("geojson object is required for geojson source")
		}

	case "reference":
		// 引用类型必须有 reference
		if d.GetStringParam("reference") == "" {
			return ErrInvalidDataLocation("reference is required for reference source")
		}

	default:
		return ErrInvalidDataLocation("unsupported source_type: " + d.SourceType)
	}

	return nil
}

// ErrInvalidDataLocation 无效的数据位置错误
type ErrInvalidDataLocation string

func (e ErrInvalidDataLocation) Error() string {
	return "invalid data location: " + string(e)
}
