package format

import (
	"fmt"

	"github.com/addp/common/engine/plugin"
)

// ExtensionInfo 扩展信息接口（标记接口）
// 所有扩展信息类型都必须实现此接口。
type ExtensionInfo interface {
	// ExtensionType 返回扩展类型标识
	// 常见值: "spatial", "csv", "shapefile"
	ExtensionType() string
}

// SpatialInfo 空间数据扩展信息
// 适用于: PostgreSQL/PostGIS表、Shapefile、JSON 空间扩展、GeoPackage
type SpatialInfo struct {
	GeometryColumn  string      // 几何字段名（如 "geom", "geometry", "shape"）
	GeometryType    string      // 几何类型（Point, LineString, Polygon, MultiPoint, etc.）
	SRID            int         // 空间参考系统 ID（如 4326 for WGS84, 3857 for Web Mercator）
	BoundingBox     *[4]float64 // 边界框 [minX, minY, maxX, maxY]（WGS84 度）
	HasSpatialIndex bool        // 是否有空间索引
	IndexName       string      // 空间索引名称（如果有）
	Dimension       int         // 维度（2D, 3D, 4D）
}

func (s *SpatialInfo) ExtensionType() string {
	return "spatial"
}

// IsSRIDWGS84 判断是否为 WGS84 坐标系
func (s *SpatialInfo) IsSRIDWGS84() bool {
	return s.SRID == 4326
}

// IsSRIDWebMercator 判断是否为 Web Mercator 坐标系
func (s *SpatialInfo) IsSRIDWebMercator() bool {
	return s.SRID == 3857
}

// GetBoundingBoxString 获取边界框字符串表示
func (s *SpatialInfo) GetBoundingBoxString() string {
	if s.BoundingBox == nil {
		return ""
	}
	bbox := *s.BoundingBox
	return fmt.Sprintf("[%.6f, %.6f, %.6f, %.6f]", bbox[0], bbox[1], bbox[2], bbox[3])
}

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

// CSVInfo CSV 格式扩展信息
type CSVInfo struct {
	Delimiter  rune   // 分隔符（',' 或 '\t' 等）
	Encoding   string // 字符编码（如 "utf-8", "gbk"）
	HasHeader  bool   // 是否有表头
	QuoteChar  rune   // 引用字符（通常是 '"'）
	EscapeChar rune   // 转义字符
	LineEnding string // 行结束符（"\n" 或 "\r\n"）
}

func (c *CSVInfo) ExtensionType() string {
	return "csv"
}

// ShapefileInfo Shapefile 格式扩展信息
type ShapefileInfo struct {
	Encoding   string // DBF 编码（如 "utf-8", "gbk", "cp936"）
	ShapeType  string // Shape 类型（POINT, POLYLINE, POLYGON, etc.）
	HasPRJ     bool   // 是否有 .prj 文件（投影信息）
	HasCPG     bool   // 是否有 .cpg 文件（编码信息）
	DBFVersion byte   // DBF 版本号
}

func (s *ShapefileInfo) ExtensionType() string {
	return "shapefile"
}

// ExcelInfo Excel 格式扩展信息
type ExcelInfo struct {
	SheetName  string // 工作表名称
	SheetIndex int    // 工作表索引（从0开始）
	SheetCount int    // 工作表总数
}

func (e *ExcelInfo) ExtensionType() string {
	return "excel"
}
