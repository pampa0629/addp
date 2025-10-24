package connector

import (
	"github.com/addp/transfer/pkg/pipeline"
)

// RegisterAllConnectors 注册所有连接器到全局注册表
func RegisterAllConnectors(registry *pipeline.ConnectorRegistry) {
	// 注册 JDBC Reader/Writer (PostgreSQL, MySQL, etc.)
	registry.RegisterReader("jdbc", NewJDBCReader)        // 通用 JDBC 类型
	registry.RegisterReader("postgresql", NewJDBCReader)
	registry.RegisterReader("mysql", NewJDBCReader)
	registry.RegisterWriter("jdbc", NewJDBCWriter)        // 通用 JDBC 类型
	registry.RegisterWriter("postgresql", NewJDBCWriter)
	registry.RegisterWriter("mysql", NewJDBCWriter)

	// 注册 Shapefile Reader/Writer
	registry.RegisterReader("shapefile", NewShapefileReader)
	registry.RegisterWriter("shapefile", NewShapefileWriter)

	// 注册 GeoPackage Reader/Writer
	registry.RegisterReader("geopackage", NewGeoPackageReader)
	registry.RegisterWriter("geopackage", NewGeoPackageWriter)

	// 注册 GeoJSON Reader/Writer
	registry.RegisterReader("geojson", NewGeoJSONReader)
	registry.RegisterWriter("geojson", NewGeoJSONWriter)

	// 注册 S3/MinIO Writer
	registry.RegisterWriter("s3", NewS3Writer)
	registry.RegisterWriter("minio", NewS3Writer)

	// TODO: 注册 File Reader/Writer (not implemented yet)
	// registry.RegisterReader("file", NewFileReader)
	// registry.RegisterWriter("file", NewFileWriter)
}
