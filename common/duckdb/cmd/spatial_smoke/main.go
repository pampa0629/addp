package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/addp/common/duckdb"
)

func main() {
	ctx := context.Background()

	db, err := duckdb.OpenDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "open duckdb failed: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get conn failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	if err := duckdb.LoadSpatialExtension(ctx, conn); err != nil {
		fmt.Fprintf(os.Stderr, "load spatial failed: %v\n", err)
		os.Exit(2)
	}

	query := `
		SELECT CAST(
			ST_AsGeoJSON(
				ST_Transform(
					ST_GeomFromGeoJSON('{"type":"Point","coordinates":[12958412.49,4852030.63]}'),
					'EPSG:3857',
					'EPSG:4326',
					true
				)
			) AS VARCHAR
		)
	`

	var result string
	if err := conn.QueryRowContext(ctx, query).Scan(&result); err != nil {
		fmt.Fprintf(os.Stderr, "query failed: %v\n", err)
		os.Exit(3)
	}

	fmt.Println(result)
}

var _ *sql.Conn
