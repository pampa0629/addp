package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/xy"
)

func (p *OraclePlugin) ReadSpatialFeature(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.SpatialFeatureReadOptions) (*plugin.SpatialFeatureData, error) {
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) < 2 {
		return nil, fmt.Errorf("Oracle spatial feature requires a schema/table path")
	}
	schema := segments[len(segments)-2].Name
	table := segments[len(segments)-1].Name
	if p.isSystemSchema(schema) {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorUnsupported, fmt.Errorf("Oracle system schema %q is not exposed", schema))
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build Oracle spatial feature dsn: %w", err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return nil, fmt.Errorf("open Oracle spatial feature connection: %w", err)
	}
	defer db.Close()
	fields, err := p.listColumnsWithSQL(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	geometryField, identityField, err := resolveOracleSpatialFeatureFields(fields, opts)
	if err != nil {
		return nil, err
	}
	spatialInfo, err := enrichOracleSpatialInfo(ctx, db, schema, table, oracleSpatialInfoFromFields([]datatype.FieldInfo{geometryField}))
	if err != nil {
		return nil, err
	}
	dialect := commonquery.ForEngine(p.Type())
	quotedGeometry := dialect.QuoteIdentifier(geometryField.Name)
	query := fmt.Sprintf(
		"SELECT SDO_UTIL.TO_WKBGEOMETRY(%s) FROM %s WHERE %s = :1 FETCH FIRST 1 ROWS ONLY",
		quotedGeometry,
		dialect.QualifiedTable(schema, table),
		dialect.QuoteIdentifier(identityField.Name),
	)
	var geometryWKB []byte
	if err := db.QueryRowContext(ctx, query, opts.IdentityValue).Scan(&geometryWKB); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("read Oracle spatial feature: %w", err)
	}
	srid := oracleSpatialSRID(spatialInfo, geometryField.Name)
	geometryEWKB, err := oracleWKBToEWKB(geometryWKB, srid)
	if err != nil {
		return nil, err
	}
	centroidEWKB, err := oracleGeometryCentroidEWKB(geometryEWKB, srid)
	if err != nil {
		return nil, fmt.Errorf("compute Oracle spatial feature centroid: %w", err)
	}
	return &plugin.SpatialFeatureData{GeometryEWKB: geometryEWKB, CentroidEWKB: centroidEWKB, SRID: srid, Spatial: spatialInfo}, nil
}

func resolveOracleSpatialFeatureFields(fields []datatype.FieldInfo, opts plugin.SpatialFeatureReadOptions) (datatype.FieldInfo, datatype.FieldInfo, error) {
	geometryName := strings.TrimSpace(opts.GeometryField)
	identityName := strings.TrimSpace(opts.IdentityField)
	if geometryName == "" || identityName == "" {
		return datatype.FieldInfo{}, datatype.FieldInfo{}, fmt.Errorf("Oracle spatial feature requires geometry_field and identity_field")
	}
	var geometryField, identityField datatype.FieldInfo
	for _, field := range fields {
		if strings.EqualFold(field.Name, geometryName) {
			geometryField = field
		}
		if strings.EqualFold(field.Name, identityName) {
			identityField = field
		}
	}
	if geometryField.Name == "" || !datatype.IsSpatialFieldType(geometryField.Type) {
		return datatype.FieldInfo{}, datatype.FieldInfo{}, fmt.Errorf("Oracle spatial feature geometry field %q does not exist or is not spatial", geometryName)
	}
	if identityField.Name == "" {
		return datatype.FieldInfo{}, datatype.FieldInfo{}, fmt.Errorf("Oracle spatial feature identity field %q does not exist", identityName)
	}
	return geometryField, identityField, nil
}

func oracleGeometryCentroidEWKB(encoded []byte, srid int) ([]byte, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	geometry, err := ewkb.Unmarshal(encoded)
	if err != nil {
		return nil, err
	}
	coord, err := xy.Centroid(geometry)
	if err != nil || len(coord) < 2 {
		bounds := geometry.Bounds()
		if bounds == nil || bounds.IsEmpty() {
			return nil, fmt.Errorf("Oracle geometry centroid is unavailable")
		}
		coord = geom.Coord{(bounds.Min(0) + bounds.Max(0)) / 2, (bounds.Min(1) + bounds.Max(1)) / 2}
	}
	point := geom.NewPointFlat(geom.XY, []float64{coord[0], coord[1]}).SetSRID(srid)
	return ewkb.Marshal(point, ewkb.NDR)
}
