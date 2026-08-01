package planner

import (
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/common/engine/plugins/mysql"
	"github.com/addp/common/engine/plugins/postgresql"
)

func TestParseContinuousTaskSpecAcceptsKafkaToPostgresUpsert(t *testing.T) {
	spec, err := ParseContinuousTaskSpec(validContinuousConfig())
	if err != nil {
		t.Fatalf("ParseContinuousTaskSpec() error = %v", err)
	}
	if spec.Runtime.Boundary != RuntimeBoundaryContinuous || spec.Source.ChangeStream.PollBatchSize != 1000 {
		t.Fatalf("continuous spec = %#v", spec)
	}
}

func TestBuildContinuousPlanRequiresKafkaAndAtomicMonotonicPostgres(t *testing.T) {
	spec, err := ParseContinuousTaskSpec(validContinuousConfig())
	if err != nil {
		t.Fatalf("ParseContinuousTaskSpec() error = %v", err)
	}
	sourceCaps := (&kafka.KafkaPlugin{}).Capabilities()
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	plan, err := BuildContinuousPlan(spec, StaticEngineResolver{
		30: {Type: "kafka", Capabilities: &sourceCaps},
		8:  {Type: "postgresql", Capabilities: &targetCaps},
	})
	if err != nil {
		t.Fatalf("BuildContinuousPlan() error = %v", err)
	}
	if plan.Source.Path.StringPath() != "orders.events" || plan.Target.Path.StringPath() != "public/orders" {
		t.Fatalf("continuous paths source=%q target=%q", plan.Source.Path.StringPath(), plan.Target.Path.StringPath())
	}
	if len(plan.Target.Fields) != 2 || plan.Target.Fields[0].Type != "int" || len(plan.Target.Keys) != 1 || plan.Target.Keys[0] != "id" {
		t.Fatalf("continuous target plan = %#v", plan.Target)
	}
}

func TestBuildContinuousPlanAcceptsMySQLAtomicTarget(t *testing.T) {
	spec, err := ParseContinuousTaskSpec(validContinuousConfig())
	if err != nil {
		t.Fatal(err)
	}
	spec.Target.ParentLocator = "addp://engine/8/path/business?type=database"
	sourceCaps := (&kafka.KafkaPlugin{}).Capabilities()
	targetCaps := (&mysql.MySQLPlugin{}).Capabilities()
	plan, err := BuildContinuousPlan(spec, StaticEngineResolver{
		30: {Type: "kafka", Capabilities: &sourceCaps},
		8:  {Type: "mysql", Capabilities: &targetCaps},
	})
	if err != nil {
		t.Fatalf("BuildContinuousPlan() error = %v", err)
	}
	if plan.TargetType != "mysql" || plan.Target.Path.StringPath() != "business/orders" {
		t.Fatalf("MySQL continuous target plan = %#v", plan)
	}
}

func TestParseContinuousTaskSpecRejectsImplicitFieldSchema(t *testing.T) {
	config := validContinuousConfig()
	field := config["transforms"].([]interface{})[0].(map[string]interface{})["fields"].([]interface{})[0].(map[string]interface{})
	delete(field, "target_type")
	if _, err := ParseContinuousTaskSpec(config); err == nil {
		t.Fatal("ParseContinuousTaskSpec() error = nil, want missing target_type rejection")
	}
}

func TestParseContinuousTaskSpecRejectsUnusedFormatConversion(t *testing.T) {
	config := validContinuousConfig()
	field := config["transforms"].([]interface{})[0].(map[string]interface{})["fields"].([]interface{})[1].(map[string]interface{})
	field["format"] = "2006-01-02"
	if _, err := ParseContinuousTaskSpec(config); err == nil {
		t.Fatal("ParseContinuousTaskSpec() error = nil, want unsupported format conversion rejection")
	}
}

func TestParseContinuousTaskSpecRejectsKeyMappingDrift(t *testing.T) {
	config := validContinuousConfig()
	config["target"].(map[string]interface{})["policy"].(map[string]interface{})["keys"] = []interface{}{"order_id"}
	if _, err := ParseContinuousTaskSpec(config); err == nil {
		t.Fatal("ParseContinuousTaskSpec() error = nil, want key mapping rejection")
	}
}

func TestParseContinuousTaskSpecRequiresExplicitRecordFailureMode(t *testing.T) {
	config := validContinuousConfig()
	delete(config["runtime"].(map[string]interface{}), "record_failure")
	if _, err := ParseContinuousTaskSpec(config); err == nil {
		t.Fatal("missing runtime.record_failure was accepted")
	}
	config = validContinuousConfig()
	config["runtime"].(map[string]interface{})["record_failure"] = map[string]interface{}{"mode": "ignore"}
	if _, err := ParseContinuousTaskSpec(config); err == nil {
		t.Fatal("unsupported runtime.record_failure.mode was accepted")
	}
}

func TestBuildContinuousPlanRequiresSkipCapabilityForDeadLetterMode(t *testing.T) {
	config := validContinuousConfig()
	config["runtime"].(map[string]interface{})["record_failure"] = map[string]interface{}{"mode": "dead_letter"}
	spec, err := ParseContinuousTaskSpec(config)
	if err != nil {
		t.Fatal(err)
	}
	sourceCaps := (&kafka.KafkaPlugin{}).Capabilities()
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	for index := range targetCaps.Storage.Store.PartitionedTableChangeApply.Operations {
		if targetCaps.Storage.Store.PartitionedTableChangeApply.Operations[index] == engineplugin.TableChangeOperationSkip {
			targetCaps.Storage.Store.PartitionedTableChangeApply.Operations = append(
				targetCaps.Storage.Store.PartitionedTableChangeApply.Operations[:index],
				targetCaps.Storage.Store.PartitionedTableChangeApply.Operations[index+1:]...,
			)
			break
		}
	}
	if _, err := BuildContinuousPlan(spec, StaticEngineResolver{
		30: {Type: "kafka", Capabilities: &sourceCaps},
		8:  {Type: "postgresql", Capabilities: &targetCaps},
	}); err == nil {
		t.Fatal("dead-letter plan accepted a target without skip capability")
	}
}

func TestBuildDatabaseCDCContinuousPlanMapsFrozenPostgreSQLSpatialFactsToTarget(t *testing.T) {
	config := validPostgreSQLCDCConfig()
	fields := config["transforms"].([]interface{})[0].(map[string]interface{})["fields"].([]interface{})
	config["transforms"].([]interface{})[0].(map[string]interface{})["fields"] = append(fields, map[string]interface{}{
		"source": "shape", "target": "geometry", "target_type": "geometry", "nullable": true,
	})
	spec, err := ParseDatabaseCDCTaskSpec(config)
	if err != nil {
		t.Fatal(err)
	}
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	plan, err := BuildDatabaseCDCContinuousPlan(spec, StaticEngineResolver{
		12: {Type: "postgresql", ConnInfo: engineplugin.ConnectionInfo{"database": "business"}},
		20: {Type: "postgresql", Capabilities: &targetCaps},
	}, DatabaseCDCStreamBinding{
		Provider:      "postgresql",
		ConnInfo:      engineplugin.ConnectionInfo{"bootstrap_servers": "infra"},
		ConsumerGroup: "group", SourceIdentity: "addp://engine/12/path/public/orders?type=table",
		Database: "business", Schema: "public", Table: "orders",
		SpatialInfo: datatype.NewSingleGeometrySpatialInfo("shape", "MultiPolygon", 4549, 2),
	}, 100)
	if err != nil {
		t.Fatalf("BuildDatabaseCDCContinuousPlan() error = %v", err)
	}
	if plan.CDC.SpatialInfo.PrimaryGeometryName() != "shape" || plan.Target.SpatialInfo.PrimaryGeometryName() != "geometry" ||
		plan.Target.SpatialInfo.PrimaryGeometryType() != "MultiPolygon" || plan.Target.SpatialInfo.PrimarySRIDValue() != 4549 {
		t.Fatalf("CDC spatial plan source=%#v target=%#v", plan.CDC.SpatialInfo, plan.Target.SpatialInfo)
	}
}

func TestBuildDatabaseCDCContinuousPlanRoutesMySQLEnvelope(t *testing.T) {
	config := validPostgreSQLCDCConfig()
	config["source"].(map[string]interface{})["locator"] = "addp://engine/12/path/business/orders?type=table"
	spec, err := ParseDatabaseCDCTaskSpec(config)
	if err != nil {
		t.Fatal(err)
	}
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	plan, err := BuildDatabaseCDCContinuousPlan(spec, StaticEngineResolver{
		12: {Type: "mysql"}, 20: {Type: "postgresql", Capabilities: &targetCaps},
	}, DatabaseCDCStreamBinding{
		Provider: "mysql", ConnInfo: engineplugin.ConnectionInfo{"bootstrap_servers": "infra"},
		ConsumerGroup: "group", SourceIdentity: "addp://engine/12/path/business/orders?type=table",
		Database: "business", Table: "orders",
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Envelope != ContinuousEnvelopeMySQLDebezium || plan.CDC.Provider != "mysql" || plan.CDC.Schema != "" {
		t.Fatalf("MySQL CDC plan = %#v", plan)
	}
}

func TestBuildDatabaseCDCContinuousPlanAcceptsMySQLTarget(t *testing.T) {
	config := validPostgreSQLCDCConfig()
	config["target"].(map[string]interface{})["parent_locator"] = "addp://engine/20/path/business?type=database"
	spec, err := ParseDatabaseCDCTaskSpec(config)
	if err != nil {
		t.Fatal(err)
	}
	targetCaps := (&mysql.MySQLPlugin{}).Capabilities()
	plan, err := BuildDatabaseCDCContinuousPlan(spec, StaticEngineResolver{
		12: {Type: "postgresql", ConnInfo: engineplugin.ConnectionInfo{"database": "business"}},
		20: {Type: "mysql", Capabilities: &targetCaps},
	}, DatabaseCDCStreamBinding{
		Provider: "postgresql", ConnInfo: engineplugin.ConnectionInfo{"bootstrap_servers": "infra"},
		ConsumerGroup: "group", SourceIdentity: "addp://engine/12/path/public/orders?type=table",
		Database: "business", Schema: "public", Table: "orders",
	}, 100)
	if err != nil {
		t.Fatalf("BuildDatabaseCDCContinuousPlan() error = %v", err)
	}
	if plan.TargetType != "mysql" || plan.Target.Path.StringPath() != "business/orders_cdc" {
		t.Fatalf("MySQL database CDC target plan = %#v", plan)
	}
}

func TestBuildContinuousFieldsPreservesDecimalPrecisionAndScale(t *testing.T) {
	precision, scale := 20, 10
	mappings, fields, err := buildDatabaseCDCFields([]FieldMappingSpec{{
		Source: "amount", Target: "amount", TargetType: "decimal", Precision: &precision, Scale: &scale, Nullable: boolPtr(false),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 1 || mappings[0].Precision != 20 || mappings[0].Scale != 10 ||
		len(fields) != 1 || fields[0].Precision != 20 || fields[0].Scale != 10 {
		t.Fatalf("decimal mappings=%#v fields=%#v", mappings, fields)
	}
}

func validContinuousConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load":    map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "kafka"}},
		"source": map[string]interface{}{
			"locator": "addp://engine/30/path/orders.events?type=topic", "representation": "native",
			"change_stream": map[string]interface{}{
				"envelope": "record", "encoding": "json", "key": map[string]interface{}{"source": "value", "fields": []interface{}{"id"}},
				"start": map[string]interface{}{"mode": "committed", "initial": "earliest"}, "poll_batch_size": 1000,
			},
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/8/path/public?type=schema", "name": "orders", "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{
				map[string]interface{}{"source": "id", "target": "id", "target_type": "int", "nullable": false},
				map[string]interface{}{"source": "name", "target": "name", "target_type": "string", "nullable": true},
			},
		}},
	}
}
