package extractors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/addp/common/format"
	"github.com/addp/common/format/shapefile"
)

// ShapefileExtractor Shapefile文件的元数据提取器
type ShapefileExtractor struct{}

func (e *ShapefileExtractor) SupportedTypes() []string {
	return []string{
		format.FormatToMIME(format.FormatShapefile),
		"application/x-shapefile",
		"application/octet-stream", // Shapefile组件文件可能被识别为二进制流
	}
}

func (e *ShapefileExtractor) Priority() int {
	return 100 // 高优先级
}

func (e *ShapefileExtractor) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	// 1. 验证是否为Shapefile
	formatType := format.DetectFormat(input.ObjectKey, nil)
	if formatType != format.FormatShapefile {
		return nil, fmt.Errorf("not a shapefile: %s", input.ObjectKey)
	}

	// 2. 只处理 .shp 文件（主文件）
	ext := filepath.Ext(input.ObjectKey)
	if ext != ".shp" {
		return nil, fmt.Errorf("only .shp files are supported for metadata extraction")
	}

	// 3. 保存到临时文件（Shapefile需要文件路径访问）
	tempFile, err := e.saveToTempFile(input)
	if err != nil {
		return nil, fmt.Errorf("failed to save to temp file: %w", err)
	}
	defer os.Remove(tempFile)

	// 4. 打开Shapefile
	reader, err := shapefile.Open(tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	// 5. 构建基础元数据
	metadata := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "Shapefile",
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 6. 提取Schema信息
	schema := reader.GetSchema()
	columns := make([]format.ColumnMetadata, 0, len(schema)+1)

	// 添加几何字段
	shapeType := reader.Reader.GeometryType
	columns = append(columns, format.ColumnMetadata{
		Name:        "geometry",
		Type:        e.mapShapeType(int32(shapeType)),
		Nullable:    false,
		Description: "Geometry column",
	})

	// 添加属性字段
	mapper := format.GetTypeMapper("shapefile")
	if mapper == nil {
		return nil, fmt.Errorf("shapefile type mapper not registered")
	}
	for _, field := range schema {
		// 将Shapefile字段类型映射到通用类型
		commonType := mapper.ToCommon(string(field.RawType[0]))

		columns = append(columns, format.ColumnMetadata{
			Name:        field.Name,
			Type:        string(commonType),
			Nullable:    true, // DBF字段通常可为空
			Description: fmt.Sprintf("DBF field (size: %d, precision: %d)", field.Size, field.Precision),
		})
	}

	// 7. 获取记录数
	recordCount := int64(reader.Reader.AttributeCount())

	// 8. 提取样本数据（前5条）
	sampleSize := 5
	if recordCount < int64(sampleSize) {
		sampleSize = int(recordCount)
	}

	sampleData := make([]map[string]interface{}, 0, sampleSize)
	features, err := reader.ReadAllFeatures(sampleSize)
	if err != nil {
		// 样本数据提取失败不应导致整个元数据提取失败
		metadata.CustomAttrs["sample_error"] = err.Error()
	} else {
		for _, feature := range features {
			sampleData = append(sampleData, feature.Properties)
		}
	}

	// 9. 构建SchemaMetadata
	metadata.SchemaInfo = &format.SchemaMetadata{
		Columns:    columns,
		RowCount:   recordCount,
		SampleData: sampleData,
		Extra: map[string]interface{}{
			"geometry_column": "geometry",
			"shape_type":      e.mapShapeType(int32(shapeType)),
		},
	}

	// 10. 提取地理空间元数据
	bbox := reader.Reader.BBox()
	geoMeta := map[string]interface{}{
		"geometry_type": e.mapShapeType(int32(shapeType)),
		"feature_count": recordCount,
		"bounding_box": []float64{
			bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY,
		},
		"coordinate_system": e.extractCRS(tempFile),
	}

	metadata.CustomAttrs["geo_metadata"] = geoMeta

	// 11. 添加文件组合信息
	metadata.CustomAttrs["shapefile_components"] = e.inferComponents(input.ObjectKey)

	return metadata, nil
}

// saveToTempFile 将输入流保存到临时文件
func (e *ShapefileExtractor) saveToTempFile(input format.ExtractInput) (string, error) {
	// 创建临时文件
	tempFile, err := os.CreateTemp("", "shapefile-*.shp")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	// 写入内容
	if _, err := tempFile.ReadFrom(input.Reader); err != nil {
		os.Remove(tempFile.Name())
		return "", err
	}

	return tempFile.Name(), nil
}

// mapShapeType 将Shapefile的ShapeType映射到几何类型字符串
func (e *ShapefileExtractor) mapShapeType(shapeType int32) string {
	// 参考 go-shp 库的 ShapeType 常量
	// https://github.com/jonas-p/go-shp/blob/master/shapefile.go
	switch shapeType {
	case 0:
		return "Null"
	case 1:
		return "Point"
	case 3:
		return "Polyline"
	case 5:
		return "Polygon"
	case 8:
		return "MultiPoint"
	case 11:
		return "PointZ"
	case 13:
		return "PolylineZ"
	case 15:
		return "PolygonZ"
	case 18:
		return "MultiPointZ"
	case 21:
		return "PointM"
	case 23:
		return "PolylineM"
	case 25:
		return "PolygonM"
	case 28:
		return "MultiPointM"
	case 31:
		return "MultiPatch"
	default:
		return fmt.Sprintf("Unknown(%d)", shapeType)
	}
}

// extractCRS 提取坐标参考系统（从.prj文件）
func (e *ShapefileExtractor) extractCRS(shpFilePath string) string {
	// 构造.prj文件路径
	prjFile := shpFilePath[:len(shpFilePath)-4] + ".prj"

	// 读取.prj文件
	content, err := os.ReadFile(prjFile)
	if err != nil {
		// 没有.prj文件或读取失败，返回默认值
		return "Unknown"
	}

	// .prj文件包含WKT格式的CRS定义
	crs := string(content)

	// 尝试从WKT中提取EPSG代码
	// 例如: GEOGCS["GCS_WGS_1984",...AUTHORITY["EPSG","4326"]]
	if epsg := e.extractEPSG(crs); epsg != "" {
		return epsg
	}

	// 返回完整的WKT（截断以避免过长）
	if len(crs) > 200 {
		return crs[:200] + "..."
	}
	return crs
} 

// extractEPSG 从WKT字符串中提取EPSG代码
func (e *ShapefileExtractor) extractEPSG(wkt string) string {
	// 简单的字符串匹配提取EPSG代码
	// 查找 AUTHORITY["EPSG","xxxx"]
	const prefix = `AUTHORITY["EPSG","`
	const suffix = `"]`

	start := -1
	for i := 0; i < len(wkt)-len(prefix); i++ {
		if wkt[i:i+len(prefix)] == prefix {
			start = i + len(prefix)
			break
		}
	}

	if start == -1 {
		return ""
	}

	end := start
	for end < len(wkt) && wkt[end] != '"' {
		end++
	}

	if end > start {
		return "EPSG:" + wkt[start:end]
	}

	return ""
}

// inferComponents 推断Shapefile的组件文件
func (e *ShapefileExtractor) inferComponents(shpPath string) []string {
	basePath := shpPath[:len(shpPath)-4]
	components := []string{
		filepath.Base(shpPath), // .shp
	}

	// 常见的Shapefile组件文件扩展名
	extensions := []string{".shx", ".dbf", ".prj", ".sbn", ".sbx", ".cpg"}
	for _, ext := range extensions {
		componentPath := basePath + ext
		components = append(components, filepath.Base(componentPath))
	}

	return components
}
