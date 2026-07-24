package planner

import (
	"testing"

	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/common/engine/plugins/postgresql"
)

func TestBuildReplayContinuousPlanOnlyReplacesTargetPlacement(t *testing.T) {
	spec, err := ParseContinuousTaskSpec(validContinuousConfig())
	if err != nil {
		t.Fatal(err)
	}
	resolver := replayTestResolver()

	plan, err := BuildReplayContinuousPlan(spec, ReplayTargetSpec{
		ParentLocator: "addp://engine/2/path/replay?type=schema&node_id=12",
		Name:          "orders_replay",
	}, resolver)
	if err != nil {
		t.Fatalf("BuildReplayContinuousPlan failed: %v", err)
	}
	if got := plan.Source.SourceIdentity; got != spec.Source.Locator {
		t.Fatalf("source identity = %q, want %q", got, spec.Source.Locator)
	}
	if got := plan.Target.Path.StringPath(); got != "replay/orders_replay" {
		t.Fatalf("target path = %q", got)
	}
	if len(plan.Mappings) != len(spec.Transforms[0].Fields) || len(plan.SourceKeys) != len(spec.Source.ChangeStream.Key.Fields) {
		t.Fatalf("replay plan did not inherit mappings and keys: %#v", plan)
	}
	if plan.RecordFailureMode != RecordFailureModeBlock {
		t.Fatalf("record failure mode = %q", plan.RecordFailureMode)
	}
}

func TestBuildReplayContinuousPlanRejectsOwnerTarget(t *testing.T) {
	spec, err := ParseContinuousTaskSpec(validContinuousConfig())
	if err != nil {
		t.Fatal(err)
	}
	_, err = BuildReplayContinuousPlan(spec, ReplayTargetSpec{
		ParentLocator: spec.Target.ParentLocator,
		Name:          spec.Target.Name,
	}, replayTestResolver())
	if err == nil {
		t.Fatal("owner task target was accepted as replay target")
	}
}

func TestBuildReplayContinuousPlanRejectsDeadLetterOwner(t *testing.T) {
	spec, err := ParseContinuousTaskSpec(validContinuousConfig())
	if err != nil {
		t.Fatal(err)
	}
	spec.Runtime.RecordFailure.Mode = RecordFailureModeDeadLetter
	_, err = BuildReplayContinuousPlan(spec, ReplayTargetSpec{
		ParentLocator: "addp://engine/2/path/replay?type=schema&node_id=12",
		Name:          "orders_replay",
	}, replayTestResolver())
	if err == nil {
		t.Fatal("dead-letter owner task was accepted for bounded replay")
	}
}

func replayTestResolver() StaticEngineResolver {
	sourceCapabilities := (&kafka.KafkaPlugin{}).Capabilities()
	targetCapabilities := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	return StaticEngineResolver{
		30: {Type: "kafka", Capabilities: &sourceCapabilities},
		2:  {Type: "postgresql", Capabilities: &targetCapabilities},
		8:  {Type: "postgresql", Capabilities: &targetCapabilities},
	}
}
