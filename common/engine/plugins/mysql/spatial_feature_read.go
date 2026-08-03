package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkb"
	"github.com/twpayne/go-geom/xy"
)

func (p *MySQLPlugin) ReadSpatialFeature(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.SpatialFeatureReadOptions) (*plugin.SpatialFeatureData, error) {
	database, table, err := mysqlTablePathParts(path)
	if err != nil {
		return nil, err
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build mysql spatial feature dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql spatial feature connection: %w", err)
	}
	defer db.Close()

	columns, err := mysqlTableColumns(ctx, db, database, table)
	if err != nil {
		return nil, err
	}
	geometryColumn, identityColumn, err := resolveMySQLSpatialFeatureColumns(columns, opts)
	if err != nil {
		return nil, err
	}
	dialect := mysqlDialect()
	quotedGeometry := dialect.QuoteIdentifier(geometryColumn.Name)
	query := fmt.Sprintf(
		"SELECT ST_AsWKB(%s, 'axis-order=long-lat'), ST_SRID(%s) FROM %s WHERE %s = ? LIMIT 1",
		quotedGeometry,
		quotedGeometry,
		dialect.QualifiedTable(database, table),
		dialect.QuoteIdentifier(identityColumn.Name),
	)
	var geometryWKB []byte
	var srid int
	if err := db.QueryRowContext(ctx, query, opts.IdentityValue).Scan(&geometryWKB, &srid); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("read mysql spatial feature: %w", err)
	}
	definitions := map[int]datatype.CRSDefinition{}
	if srid > 0 {
		definition, err := queryMySQLCRSDefinition(ctx, db, srid)
		if err != nil {
			return nil, err
		}
		if definition != nil {
			definitions[srid] = *definition
		}
	}
	spatialInfo := buildMySQLSpatialInfo([]mysqlSpatialColumnRow{{
		Name: geometryColumn.Name, DataType: geometryColumn.DataType, SRSID: geometryColumn.SRSID, Nullable: geometryColumn.Nullable,
	}}, nil, definitions)
	geometryEWKB, err := mysqlWKBToEWKB(geometryWKB, srid)
	if err != nil {
		return nil, fmt.Errorf("decode mysql spatial feature geometry: %w", err)
	}
	centroidEWKB, err := mysqlGeometryCentroidEWKB(geometryEWKB, srid)
	if err != nil {
		return nil, fmt.Errorf("decode mysql spatial feature centroid: %w", err)
	}
	return &plugin.SpatialFeatureData{
		GeometryEWKB: geometryEWKB,
		CentroidEWKB: centroidEWKB,
		SRID:         srid,
		Spatial:      spatialInfo,
	}, nil
}

func mysqlGeometryCentroidEWKB(encoded []byte, srid int) ([]byte, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	geometry, err := ewkb.Unmarshal(encoded)
	if err != nil {
		return nil, err
	}
	coord, err := mysqlGeometryCentroid(geometry)
	if err != nil {
		return nil, err
	}
	point := geom.NewPointFlat(geom.XY, []float64{coord.X(), coord.Y()}).SetSRID(srid)
	return ewkb.Marshal(point, ewkb.NDR)
}

func mysqlGeometryCentroid(geometry geom.T) (geom.Coord, error) {
	if _, ok := geometry.(*geom.GeometryCollection); !ok {
		return xy.Centroid(geometry)
	}
	collector := &mysqlCentroidCollector{
		area:  xy.NewAreaCentroidCalculator(geom.XY),
		line:  xy.NewLineCentroidCalculator(geom.XY),
		point: xy.NewPointCentroidCalculator(),
	}
	collector.add(geometry)
	var coord geom.Coord
	switch {
	case collector.areaCount > 0:
		coord = collector.area.GetCentroid()
	case collector.lineCount > 0:
		coord = collector.line.GetCentroid()
	case collector.pointCount > 0:
		coord = collector.point.GetCentroid()
	default:
		return nil, fmt.Errorf("geometry is empty")
	}
	if len(coord) < 2 || math.IsNaN(coord[0]) || math.IsNaN(coord[1]) || math.IsInf(coord[0], 0) || math.IsInf(coord[1], 0) {
		bounds := geometry.Bounds()
		if bounds == nil || bounds.IsEmpty() {
			return nil, fmt.Errorf("geometry centroid is unavailable")
		}
		return geom.Coord{(bounds.Min(0) + bounds.Max(0)) / 2, (bounds.Min(1) + bounds.Max(1)) / 2}, nil
	}
	return coord, nil
}

type mysqlCentroidCollector struct {
	area       *xy.AreaCentroidCalculator
	line       *xy.LineCentroidCalculator
	point      xy.PointCentroidCalculator
	areaCount  int
	lineCount  int
	pointCount int
}

func (c *mysqlCentroidCollector) add(geometry geom.T) {
	if geometry == nil || geometry.Empty() {
		return
	}
	switch value := geometry.(type) {
	case *geom.Point:
		c.point.AddPoint(value)
		c.pointCount++
	case *geom.MultiPoint:
		for i := 0; i < value.NumPoints(); i++ {
			c.add(value.Point(i))
		}
	case *geom.LineString:
		c.line.AddLine(value)
		c.lineCount++
	case *geom.LinearRing:
		c.line.AddLinearRing(value)
		c.lineCount++
	case *geom.MultiLineString:
		for i := 0; i < value.NumLineStrings(); i++ {
			c.add(value.LineString(i))
		}
	case *geom.Polygon:
		c.area.AddPolygon(value)
		c.areaCount++
	case *geom.MultiPolygon:
		for i := 0; i < value.NumPolygons(); i++ {
			c.add(value.Polygon(i))
		}
	case *geom.GeometryCollection:
		for _, child := range value.Geoms() {
			c.add(child)
		}
	}
}

func resolveMySQLSpatialFeatureColumns(columns []mysqlColumnInfo, opts plugin.SpatialFeatureReadOptions) (mysqlColumnInfo, mysqlColumnInfo, error) {
	geometryName := strings.TrimSpace(opts.GeometryField)
	identityName := strings.TrimSpace(opts.IdentityField)
	if geometryName == "" || identityName == "" {
		return mysqlColumnInfo{}, mysqlColumnInfo{}, fmt.Errorf("mysql spatial feature requires geometry_field and identity_field")
	}
	var geometryColumn, identityColumn mysqlColumnInfo
	for _, column := range columns {
		if strings.EqualFold(column.Name, geometryName) {
			geometryColumn = column
		}
		if strings.EqualFold(column.Name, identityName) {
			identityColumn = column
		}
	}
	if geometryColumn.Name == "" || !datatype.IsSpatialFieldType(mysqlCommonFieldType(geometryColumn)) {
		return mysqlColumnInfo{}, mysqlColumnInfo{}, fmt.Errorf("mysql spatial feature geometry field %q does not exist or is not spatial", geometryName)
	}
	if identityColumn.Name == "" {
		return mysqlColumnInfo{}, mysqlColumnInfo{}, fmt.Errorf("mysql spatial feature identity field %q does not exist", identityName)
	}
	return geometryColumn, identityColumn, nil
}

func mysqlWKBToEWKB(encoded []byte, srid int) ([]byte, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	geometry, err := wkb.Unmarshal(encoded)
	if err != nil {
		return nil, err
	}
	if srid > 0 {
		geometry, err = geom.SetSRID(geometry, srid)
		if err != nil {
			return nil, err
		}
	}
	return ewkb.Marshal(geometry, ewkb.NDR)
}
