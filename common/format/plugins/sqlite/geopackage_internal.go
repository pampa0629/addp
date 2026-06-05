package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/addp/common/datatype"
)

type geoPackageLayer struct {
	TableName      string
	Identifier     string
	GeometryColumn string
	GeometryType   string
	SRID           int
	MinX           sql.NullFloat64
	MinY           sql.NullFloat64
	MaxX           sql.NullFloat64
	MaxY           sql.NullFloat64
}

func readGeoPackageLayers(ctx context.Context, db *sql.DB) map[string]geoPackageLayer {
	query := `
		SELECT c.table_name, COALESCE(c.identifier, c.table_name), g.column_name, g.geometry_type_name, g.srs_id,
		       c.min_x, c.min_y, c.max_x, c.max_y
		FROM gpkg_contents c
		JOIN gpkg_geometry_columns g ON g.table_name = c.table_name
		WHERE c.data_type = 'features'
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return map[string]geoPackageLayer{}
	}
	defer rows.Close()

	result := map[string]geoPackageLayer{}
	for rows.Next() {
		var layer geoPackageLayer
		if err := rows.Scan(&layer.TableName, &layer.Identifier, &layer.GeometryColumn, &layer.GeometryType, &layer.SRID, &layer.MinX, &layer.MinY, &layer.MaxX, &layer.MaxY); err != nil {
			continue
		}
		result[layer.TableName] = layer
	}
	return result
}

func applyGeoPackageSpatialInfo(ctx context.Context, db *sql.DB, info *datatype.TableInfo) *datatype.SpatialInfo {
	if info == nil {
		return nil
	}
	layer, ok := readGeoPackageLayers(ctx, db)[info.Name]
	if !ok || strings.TrimSpace(layer.GeometryColumn) == "" {
		return nil
	}
	for i := range info.Fields {
		if strings.EqualFold(info.Fields[i].Name, layer.GeometryColumn) {
			info.Fields[i].Type = datatype.FieldTypeGeometry
			break
		}
	}
	spatial := datatype.NewSingleGeometrySpatialInfo(layer.GeometryColumn, layer.GeometryType, layer.SRID, 0)
	if definition := geoPackageCRSDefinition(ctx, db, layer.SRID); definition != nil {
		spatial.GeometryColumns[0].CRSRef = definition.ID
		spatial.CRSDefinitions = []datatype.CRSDefinition{*definition}
	}
	hasSpatialIndex := geoPackageLayerHasSpatialIndex(ctx, db, layer)
	spatial.HasSpatialIndex = &hasSpatialIndex
	if bbox, ok := geoPackageLayerBoundingBox(layer); ok {
		extent := datatype.BoundingBox(bbox)
		spatial.Extent = &extent
	}
	return spatial
}

func geoPackageCRSDefinition(ctx context.Context, db *sql.DB, srsID int) *datatype.CRSDefinition {
	if srsID <= 0 {
		return nil
	}
	var organization sql.NullString
	var organizationCoordsysID sql.NullInt64
	var definition sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT organization, organization_coordsys_id, definition
		FROM gpkg_spatial_ref_sys
		WHERE srs_id = ?
		LIMIT 1
	`, srsID).Scan(&organization, &organizationCoordsysID, &definition)
	if err != nil || !definition.Valid || strings.TrimSpace(definition.String) == "" {
		return nil
	}
	code := 0
	if organizationCoordsysID.Valid {
		code = int(organizationCoordsysID.Int64)
	}
	id := datatype.CRSRefFromAuthority(organization.String, code, definition.String)
	if id == "" {
		return nil
	}
	return &datatype.CRSDefinition{
		ID:                 id,
		DefinitionEncoding: datatype.CRSDefinitionEncodingWKT,
		Definition:         definition.String,
		Source:             datatype.CRSDefinitionSourceGeoPackageSRS,
	}
}

func geoPackageLayerBoundingBox(layer geoPackageLayer) ([4]float64, bool) {
	if !layer.MinX.Valid || !layer.MinY.Valid || !layer.MaxX.Valid || !layer.MaxY.Valid {
		return [4]float64{}, false
	}
	if layer.MinX.Float64 > layer.MaxX.Float64 || layer.MinY.Float64 > layer.MaxY.Float64 {
		return [4]float64{}, false
	}
	return [4]float64{layer.MinX.Float64, layer.MinY.Float64, layer.MaxX.Float64, layer.MaxY.Float64}, true
}

func geoPackageLayerHasSpatialIndex(ctx context.Context, db *sql.DB, layer geoPackageLayer) bool {
	indexTable := "rtree_" + layer.TableName + "_" + layer.GeometryColumn
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type IN ('table', 'virtual table') AND name = ? LIMIT 1`, indexTable).Scan(&exists)
	return err == nil && exists == 1
}

func isGeoPackageSystemTable(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(normalized, "gpkg_") || strings.HasPrefix(normalized, "rtree_")
}
