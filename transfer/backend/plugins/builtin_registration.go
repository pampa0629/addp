package plugins

import (
	"github.com/addp/transfer/pkg/postprocessor"
	"github.com/addp/transfer/plugins/readers"
	"github.com/addp/transfer/plugins/writers"
)

func init() {
	// JDBC 系列（兼容 PostgreSQL/MySQL）
	MustRegisterConnector("jdbc", readers.NewJDBCReader, writers.NewJDBCWriter)
	MustRegisterConnector("postgresql", readers.NewJDBCReader, writers.NewJDBCWriter)
	MustRegisterConnector("mysql", readers.NewJDBCReader, writers.NewJDBCWriter)

	// 空间数据格式
	MustRegisterConnector("shapefile", readers.NewShapefileReader, writers.NewShapefileWriter)
	MustRegisterConnector("geopackage", readers.NewGeoPackageReader, writers.NewGeoPackageWriter)
	MustRegisterConnector("geojson", readers.NewGeoJSONReader, writers.NewGeoJSONWriter)
	// SpatiaLite（仅 Reader）：输出几何为 WKB
	MustRegisterConnector("spatialite", readers.NewSpatiaLiteReader, nil)
	// 兼容别名：sqlite -> 使用同一 SpatiaLite Reader（要求文件具备空间扩展）
	MustRegisterConnector("sqlite", readers.NewSpatiaLiteReader, nil)

	// 高性能并行版本（千万级数据优化）
	MustRegisterConnector("spatialite_parallel", readers.NewSpatiaLiteParallelReader, nil)
	MustRegisterConnector("postgres_copy", nil, writers.NewPostgresCOPYWriter)

	// 表格数据格式
	MustRegisterConnector("csv", readers.NewCSVReader, writers.NewCSVWriter)

	// 对象存储
	MustRegisterConnector("s3", nil, writers.NewS3Writer)
	MustRegisterConnector("minio", nil, writers.NewS3Writer)

	// 后处理器（PostProcessor）
	// 负责数据导入后的优化任务：主键创建、空间索引创建、统计更新
	postprocessor.MustRegisterPostProcessor("postgresql", postprocessor.NewPostgresPostProcessor)
	postprocessor.MustRegisterPostProcessor("postgres", postprocessor.NewPostgresPostProcessor) // 别名
}
