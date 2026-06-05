package scanruntime

import (
	"context"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func engineSupportsSpatialMetadata(engineType string) bool {
	p, err := plugin.Get(engineType)
	if err != nil {
		return false
	}
	capabilities := p.Capabilities()
	return capabilities.Storage != nil &&
		capabilities.Storage.Facts != nil &&
		capabilities.Storage.Facts.SpatialFacts
}

// scanSpatialMetadata 扫描PostGIS空间元数据。
func (s *DatabaseRuntime) scanSpatialMetadata(ctx context.Context, db *gorm.DB, schemaName, tableName string) *models.SpatialMetadata {
	sqlDB, err := db.DB()
	if err != nil {
		s.log.Warn("获取 sql.DB 失败",
			"schema", schemaName,
			"table", tableName,
			"error", err,
		)
		return nil
	}

	spatialMeta, err := s.spatialService.ScanTableSpatialMetadata(ctx, sqlDB, schemaName, tableName)
	if err != nil {
		s.log.Warn("空间元数据扫描失败",
			"schema", schemaName,
			"table", tableName,
			"error", err,
		)
		return nil
	}

	return spatialMeta
}

func spatialInfoFromMetadata(spatialMeta *models.SpatialMetadata) *datatype.SpatialInfo {
	if spatialMeta == nil || spatialMeta.GeometryColumn == "" {
		return nil
	}
	info := &datatype.SpatialInfo{
		GeometryColumns: []datatype.GeometryColumnInfo{{
			Name:         spatialMeta.GeometryColumn,
			GeometryType: metaattr.NormalizeGeometryType(firstString(spatialMeta.GeometryTypes)),
		}},
		PrimaryGeometryColumn: spatialMeta.GeometryColumn,
	}
	if spatialMeta.SRID > 0 {
		srid := spatialMeta.SRID
		info.GeometryColumns[0].SRID = &srid
	}
	if len(spatialMeta.Extent) == 4 {
		extent := datatype.NewBoundingBox(spatialMeta.Extent[0], spatialMeta.Extent[1], spatialMeta.Extent[2], spatialMeta.Extent[3])
		info.Extent = &extent
	}
	hasSpatialIndex := spatialMeta.HasSpatialIndex
	info.HasSpatialIndex = &hasSpatialIndex
	info.IndexName = spatialMeta.IndexName
	return info
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
