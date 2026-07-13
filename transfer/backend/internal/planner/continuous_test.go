package planner

import (
	"testing"

	"github.com/addp/common/engine/plugins/kafka"
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

func validContinuousConfig() map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "continuous"},
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
