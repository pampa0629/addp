package metaattr

import (
	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
)

func SpatialInfoAttributes(info *datatype.SpatialInfo) map[string]interface{} {
	if info == nil {
		return nil
	}
	srid := info.SRID
	crs := info.CRS
	if len(info.GeometryColumns) == 1 && info.GeometryColumns[0].Name == "" {
		if srid == nil {
			srid = info.GeometryColumns[0].SRID
		}
		if crs == "" {
			crs = info.GeometryColumns[0].CRS
		}
	}
	if srid != nil {
		crs = ""
	}
	attrs := commonJSON.MapFromStruct(spatialInfoAttributes{
		SRID:                  srid,
		CRS:                   crs,
		PrimaryGeometryColumn: info.PrimaryGeometryColumn,
		HasSpatialIndex:       info.HasSpatialIndex,
		IndexName:             info.IndexName,
	})
	if attrs == nil {
		attrs = map[string]interface{}{}
	}
	if len(info.GeometryColumns) > 0 {
		geometryColumns := geometryColumnAttributes(info.GeometryColumns)
		if len(geometryColumns) > 0 {
			attrs["geometry_columns"] = geometryColumns
		}
	}
	if info.Extent != nil {
		bbox := *info.Extent
		attrs["extent"] = []float64{bbox[0], bbox[1], bbox[2], bbox[3]}
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

type spatialInfoAttributes struct {
	SRID                  *int   `json:"srid,omitempty"`
	CRS                   string `json:"crs,omitempty"`
	PrimaryGeometryColumn string `json:"primary_geometry_column,omitempty"`
	HasSpatialIndex       *bool  `json:"has_spatial_index,omitempty"`
	IndexName             string `json:"index_name,omitempty"`
}

type geometryColumnAttributesData struct {
	Name         string `json:"name,omitempty"`
	GeometryType string `json:"geometry_type,omitempty"`
	SRID         *int   `json:"srid,omitempty"`
	CRS          string `json:"crs,omitempty"`
	Dimension    *int   `json:"dimension,omitempty"`
	Nullable     *bool  `json:"nullable,omitempty"`
}

func geometryColumnAttributes(columns []datatype.GeometryColumnInfo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(columns))
	for _, column := range columns {
		if column.Name == "" {
			continue
		}
		crs := column.CRS
		if column.SRID != nil {
			crs = ""
		}
		attrs := commonJSON.MapFromStruct(geometryColumnAttributesData{
			Name:         column.Name,
			GeometryType: column.GeometryType,
			SRID:         column.SRID,
			CRS:          crs,
			Dimension:    column.Dimension,
			Nullable:     column.Nullable,
		})
		if len(attrs) == 0 {
			continue
		}
		result = append(result, attrs)
	}
	return result
}
