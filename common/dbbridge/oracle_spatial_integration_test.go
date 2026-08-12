package dbbridge

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func TestIntegrationOracleReadOnlySpatialQueries(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_DB_BRIDGE_INTEGRATION") != "1" {
		t.Skip("set ADDP_ORACLE_DB_BRIDGE_INTEGRATION=1 to run Oracle dbbridge integration test")
	}
	host := os.Getenv("ADDP_TEST_ORACLE_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	engine := &models.Engine{ID: 99001, EngineType: "oracle", ConnectionInfo: models.ConnectionInfo{
		"host":         host,
		"port":         15210,
		"service_name": "FREEPDB1",
		"user":         "business",
		"password":     "business_oracle_password",
	}}
	queries := []struct {
		name string
		sql  string
	}{
		{name: "orders", sql: `SELECT * FROM "BUSINESS"."ORDERS" FETCH FIRST 10 ROWS ONLY`},
		{name: "spatial_object", sql: `SELECT * FROM "BUSINESS"."CUSTOMER_LOCATIONS" FETCH FIRST 10 ROWS ONLY`},
		{name: "spatial_object_bounded", sql: `SELECT * FROM "BUSINESS"."CUSTOMER_LOCATIONS"`},
		{name: "spatial_matrix", sql: `SELECT * FROM "BUSINESS"."SPATIAL_FEATURES" FETCH FIRST 10 ROWS ONLY`},
		{name: "geojson", sql: `SELECT ID, SDO_UTIL.TO_GEOJSON(SHAPE) AS SHAPE_GEOJSON FROM "BUSINESS"."CUSTOMER_LOCATIONS" FETCH FIRST 10 ROWS ONLY`},
	}
	if only := os.Getenv("ADDP_ORACLE_DB_BRIDGE_QUERY"); only != "" {
		filtered := queries[:0]
		for _, query := range queries {
			if query.name == only {
				filtered = append(filtered, query)
			}
		}
		queries = filtered
	}
	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			started := time.Now()
			limit := 0
			if query.name == "spatial_object_bounded" {
				limit = 501
			}
			result, err := ExecuteReadOnlyQuery(ctx, engine, query.sql, nil, limit)
			t.Logf("duration=%s result=%#v err=%v", time.Since(started), result, err)
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || len(result.Rows) == 0 {
				t.Fatal("expected rows")
			}
		})
	}
}
