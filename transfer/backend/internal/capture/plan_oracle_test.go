package capture

import (
	"database/sql"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
)

func TestOracleCDCNativeTypeAndStrictTypeMatrix(t *testing.T) {
	if got := oracleCDCNativeType("NUMBER", sql.NullInt64{Int64: 10, Valid: true}, sql.NullInt64{Int64: 0, Valid: true}); got != "NUMBER(10,0)" {
		t.Fatalf("native type = %q", got)
	}
	if err := validateOracleCDCSourceFieldType("ID", "NUMBER(10,0)", "NUMBER", sql.NullInt64{}, datatype.FieldTypeBigInt); err != nil {
		t.Fatal(err)
	}
	if err := validateOracleCDCSourceFieldType("CREATED_AT", "DATE", "DATE", sql.NullInt64{}, datatype.FieldTypeTimestamp); err != nil {
		t.Fatal(err)
	}
	if err := validateOracleCDCSourceFieldType("SHAPE", "MDSYS.SDO_GEOMETRY", "MDSYS.SDO_GEOMETRY", sql.NullInt64{}, datatype.FieldTypeGeometry); err == nil {
		t.Fatal("Oracle Spatial CDC must be rejected in v1")
	}
	if err := validateOracleCDCSourceFieldType("PAYLOAD", "CLOB", "CLOB", sql.NullInt64{}, datatype.FieldTypeString); err == nil {
		t.Fatal("Oracle LOB CDC must be rejected in v1")
	}
	if err := validateOracleCDCSourceFieldType("UPDATED_AT", "TIMESTAMP(6)", "TIMESTAMP(6)", sql.NullInt64{Int64: 6, Valid: true}, datatype.FieldTypeTimestamp); err == nil {
		t.Fatal("Oracle sub-millisecond timestamp precision must be rejected")
	}
}

func TestOracleCDCTemporalPrecisionUsesTimestampDataScale(t *testing.T) {
	precision := oracleCDCTemporalPrecision("TIMESTAMP(3)", sql.NullInt64{Int64: 3, Valid: true})
	if !precision.Valid || precision.Int64 != 3 {
		t.Fatalf("TIMESTAMP temporal precision = %#v", precision)
	}
	if precision := oracleCDCTemporalPrecision("DATE", sql.NullInt64{}); precision.Valid {
		t.Fatalf("DATE temporal precision = %#v, want invalid", precision)
	}
}

func TestBuildOracleConnectorConfigUsesDedicatedCDCConnection(t *testing.T) {
	plan := &CapturePlan{
		SourceType: models.CaptureSourceOracle,
		SourceConnInfo: engineplugin.ConnectionInfo{
			"host": "localhost", "port": 15210, "service_name": "FREEPDB1", "user": "business", "password": "business-secret",
		},
		CDCConnInfo: engineplugin.ConnectionInfo{
			"host": "localhost", "port": 15210, "service_name": "FREEPDB1", "user": "C##ADDP_CDC", "password": "cdc-secret",
		},
		SourceCDBName: "FREE", SourceDatabase: "FREEPDB1", SourceSchema: "BUSINESS", SourceTable: "CUSTOMERS",
	}
	resource := &models.CaptureResource{
		ConnectorName: "addp-cdc-t1-task2-g1", TopicName: "__addp_cdc.1.2.1", SourceType: models.CaptureSourceOracle,
		Oracle: &models.OracleCaptureResource{SchemaHistoryTopicName: "__addp_cdc_schema.1.2.1", SchemaHistoryTopicOwned: true},
	}
	config, err := buildOracleConnectorConfig(plan, resource, SupervisorConfig{
		ConnectLoopbackHost: "host.docker.internal", ConnectBootstrapServers: "redpanda:29092",
		ConnectKafkaUsername: "connect", ConnectKafkaPassword: "kafka-secret",
		ConnectKafkaSecurityProtocol: "sasl_plaintext", ConnectKafkaSASLMechanism: "scram-sha-256",
	})
	if err != nil {
		t.Fatal(err)
	}
	if config["connector.class"] != "io.debezium.connector.oracle.OracleConnector" ||
		config["database.hostname"] != "host.docker.internal" || config["database.user"] != "C##ADDP_CDC" ||
		config["database.password"] != "cdc-secret" || config["database.dbname"] != "FREE" ||
		config["database.pdb.name"] != "FREEPDB1" || config["table.include.list"] != `BUSINESS\.CUSTOMERS` ||
		config["schema.history.internal.kafka.topic"] != "__addp_cdc_schema.1.2.1" ||
		config["transforms.route.replacement"] != "__addp_cdc.1.2.1" {
		t.Fatalf("Oracle connector config = %#v", config)
	}
	if config["database.password"] == engineplugin.GetString(plan.SourceConnInfo, "password") {
		t.Fatal("Oracle connector must not reuse the ordinary business account")
	}
}
