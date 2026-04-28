package spatial

import (
	"context"
	"database/sql"
	"encoding/json"

	commonDuckDB "github.com/addp/common/duckdb"
)

type duckDBExecutor struct{}

func (duckDBExecutor) Name() string {
	return "duckdb"
}

func (duckDBExecutor) CanTransform(sourceCRS, targetCRS CRS) bool {
	return sourceCRS.Text != "" && targetCRS.Text != ""
}

func (duckDBExecutor) TransformGeoJSON(ctx context.Context, payload interface{}, sourceCRS, targetCRS CRS) (interface{}, error) {
	db, err := commonDuckDB.OpenDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := commonDuckDB.LoadSpatialExtension(ctx, conn); err != nil {
		return nil, err
	}

	return transformGeoJSONNode(payload, func(geometry map[string]interface{}) (map[string]interface{}, error) {
		return transformGeometryWithDuckDB(ctx, conn, geometry, sourceCRS.Text, targetCRS.Text)
	})
}

func transformGeometryWithDuckDB(ctx context.Context, conn *sql.Conn, geometryMap map[string]interface{}, sourceCRS, targetCRS string) (map[string]interface{}, error) {
	raw, err := json.Marshal(geometryMap)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT CAST(
			ST_AsGeoJSON(
				ST_Transform(
					ST_GeomFromGeoJSON(?),
					?,
					?,
					true
				)
			) AS VARCHAR
		)
	`

	var transformed string
	if err := conn.QueryRowContext(ctx, query, string(raw), sourceCRS, targetCRS).Scan(&transformed); err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(transformed), &result); err != nil {
		return nil, err
	}
	return result, nil
}
