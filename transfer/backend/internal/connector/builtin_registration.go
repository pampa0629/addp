package connector

func init() {
	// JDBC 系列（兼容 PostgreSQL/MySQL）
	MustRegisterConnector("jdbc", NewJDBCReader, NewJDBCWriter)
	MustRegisterConnector("postgresql", NewJDBCReader, NewJDBCWriter)
	MustRegisterConnector("mysql", NewJDBCReader, NewJDBCWriter)

	// 空间数据格式
	MustRegisterConnector("shapefile", NewShapefileReader, NewShapefileWriter)
	MustRegisterConnector("geopackage", NewGeoPackageReader, NewGeoPackageWriter)
	MustRegisterConnector("geojson", NewGeoJSONReader, NewGeoJSONWriter)

	// 表格数据格式
	MustRegisterConnector("csv", NewCSVReader, NewCSVWriter)

	// 对象存储
	MustRegisterConnector("s3", nil, NewS3Writer)
	MustRegisterConnector("minio", nil, NewS3Writer)
}
