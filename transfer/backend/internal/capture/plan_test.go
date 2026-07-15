package capture

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

func TestBuildConnectorConfigUsesOwnedNamesAndRoutesSingleTopic(t *testing.T) {
	plan := &CapturePlan{
		SourceConnInfo: engineplugin.ConnectionInfo{
			"host": "localhost", "port": 5433, "user": "postgres", "password": "secret", "database": "business", "sslmode": "disable",
		},
		SourceSchema: "public", SourceTable: "orders",
	}
	resource := &models.CaptureResource{
		ConnectorName: "addp-cdc-t1-task2-g1", TopicName: "__addp_cdc.1.2.1",
		SlotName: "addp_cdc_t1_task2_g1", PublicationName: "addp_cdc_t1_task2_g1_pub",
	}
	config := buildConnectorConfig(plan, resource, "host.docker.internal")
	if config["database.hostname"] != "host.docker.internal" {
		t.Fatalf("database.hostname = %q", config["database.hostname"])
	}
	if config["transforms.route.replacement"] != resource.TopicName || config["slot.drop.on.stop"] != "false" {
		t.Fatalf("connector config = %#v", config)
	}
	if config["table.include.list"] != `public\.orders` {
		t.Fatalf("table.include.list = %q", config["table.include.list"])
	}
	if config["decimal.handling.mode"] != "string" || config["time.precision.mode"] != "connect" {
		t.Fatalf("Debezium schemaless type encoding is not frozen: %#v", config)
	}
}

func TestValidatePostgreSQLCDCSourceFieldRejectsSubMillisecondTemporalPrecision(t *testing.T) {
	err := validatePostgreSQLCDCSourceFieldType(
		"changed_at", "timestamp(6) without time zone", sql.NullInt64{Int64: 6, Valid: true}, datatype.FieldTypeTimestamp,
	)
	if err == nil || !strings.Contains(err.Error(), "precision 6") {
		t.Fatalf("error = %v, want temporal precision rejection", err)
	}
	if err := validatePostgreSQLCDCSourceFieldType(
		"changed_at", "timestamp(3) without time zone", sql.NullInt64{Int64: 3, Valid: true}, datatype.FieldTypeTimestamp,
	); err != nil {
		t.Fatalf("millisecond precision should be accepted: %v", err)
	}
}

func TestPostgreSQLCDCSourceTypeMatrix(t *testing.T) {
	tests := map[string]datatype.FieldType{
		"bigint":                         datatype.FieldTypeBigInt,
		"numeric(30,4)":                  datatype.FieldTypeDecimal,
		"date":                           datatype.FieldTypeDate,
		"time(3) without time zone":      datatype.FieldTypeTime,
		"timestamp(3) without time zone": datatype.FieldTypeTimestamp,
		"timestamp(3) with time zone":    datatype.FieldTypeTimestamp,
		"jsonb":                          datatype.FieldTypeJSON,
		"uuid":                           datatype.FieldTypeUUID,
	}
	for nativeType, expected := range tests {
		if actual := postgresqlCDCCommonFieldType(nativeType); actual != expected {
			t.Fatalf("postgresqlCDCCommonFieldType(%q) = %q, want %q", nativeType, actual, expected)
		}
	}
	for _, nativeType := range []string{"bytea", "text[]", "geometry"} {
		actual := postgresqlCDCCommonFieldType(nativeType)
		if actual != datatype.FieldTypeUnknown && planner.ContinuousFieldTypeSupported(actual) {
			t.Fatalf("PostgreSQL CDC v1 unexpectedly supports %q as %q", nativeType, actual)
		}
	}
}

func TestParsePostgreSQLCDCGeometryTypeFreezesConcreteTypmod(t *testing.T) {
	tests := []struct {
		nativeType   string
		geometryType string
		srid         int
		dimension    int
	}{
		{nativeType: "geometry(MultiPolygon,4549)", geometryType: "MultiPolygon", srid: 4549, dimension: 2},
		{nativeType: "geometry(PointZ,4326)", geometryType: "Point", srid: 4326, dimension: 3},
	}
	for _, test := range tests {
		geometryType, srid, dimension, err := parsePostgreSQLCDCGeometryType(test.nativeType)
		if err != nil || geometryType != test.geometryType || srid != test.srid || dimension != test.dimension {
			t.Fatalf("parsePostgreSQLCDCGeometryType(%q) = %q/%d/%d err=%v", test.nativeType, geometryType, srid, dimension, err)
		}
	}
	for _, nativeType := range []string{
		"geometry", "geometry(Geometry,4326)", "geometry(Point,0)", "geometry(PointM,4326)", "geometry(PointZM,4326)",
	} {
		if _, _, _, err := parsePostgreSQLCDCGeometryType(nativeType); err == nil {
			t.Fatalf("parsePostgreSQLCDCGeometryType(%q) error = nil", nativeType)
		}
	}
}
