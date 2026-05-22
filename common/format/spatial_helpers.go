package format

import "github.com/addp/common/datatype"

func NewSingleGeometrySpatialInfo(columnName, geometryType string, srid int, dimension int) *datatype.SpatialInfo {
	column := datatype.GeometryColumnInfo{
		Name:         columnName,
		GeometryType: geometryType,
	}
	if srid > 0 {
		column.SRID = &srid
	}
	if dimension > 0 {
		column.Dimension = &dimension
	}
	return &datatype.SpatialInfo{
		GeometryColumns:       []datatype.GeometryColumnInfo{column},
		PrimaryGeometryColumn: columnName,
	}
}

func PrimaryGeometryColumn(info *datatype.SpatialInfo) string {
	column := info.PrimaryGeometry()
	if column == nil {
		return ""
	}
	return column.Name
}

func PrimaryGeometryType(info *datatype.SpatialInfo) string {
	column := info.PrimaryGeometry()
	if column == nil {
		return ""
	}
	return column.GeometryType
}

func PrimaryGeometrySRID(info *datatype.SpatialInfo) int {
	column := info.PrimaryGeometry()
	if column == nil || column.SRID == nil {
		return 0
	}
	return *column.SRID
}

func PrimaryGeometryDimension(info *datatype.SpatialInfo) int {
	column := info.PrimaryGeometry()
	if column == nil || column.Dimension == nil {
		return 0
	}
	return *column.Dimension
}
