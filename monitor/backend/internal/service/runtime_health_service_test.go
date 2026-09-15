package service

import (
	"testing"
	"time"

	commonRuntimeHealth "github.com/addp/common/runtimehealth"
)

func TestAggregateRuntimeHealthSeparatesRolesAndExpiresInstances(t *testing.T) {
	now := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	stoppedAt := now.Add(-time.Minute)
	items := []commonRuntimeHealth.Heartbeat{
		{InstanceID: "bounded-up", Module: "transfer", Role: commonRuntimeHealth.RoleExecutionWorker, RuntimeName: "sync", Capacity: 4, ActiveCount: 2, HeartbeatAt: now.Add(-5 * time.Second), ExpiresAt: now.Add(25 * time.Second)},
		{InstanceID: "bounded-old", Module: "transfer", Role: commonRuntimeHealth.RoleExecutionWorker, RuntimeName: "sync", Capacity: 4, ActiveCount: 1, HeartbeatAt: now.Add(-time.Minute), ExpiresAt: now.Add(-30 * time.Second)},
		{InstanceID: "develop-query", Module: "develop", Role: commonRuntimeHealth.RoleExecutionSupervisor, RuntimeName: "query", Capacity: 1, ActiveCount: 1, HeartbeatAt: now.Add(-3 * time.Second), ExpiresAt: now.Add(27 * time.Second)},
		{InstanceID: "continuous-stopped", Module: "transfer", Role: commonRuntimeHealth.RoleContinuousWorker, RuntimeName: "continuous_sync", Capacity: 8, HeartbeatAt: stoppedAt, ExpiresAt: stoppedAt, StoppedAt: &stoppedAt},
	}
	result := aggregateRuntimeHealth(items, now)
	if len(result) != 3 {
		t.Fatalf("group count = %d, want 3", len(result))
	}
	var bounded, supervisor, continuous *RuntimeHealthSummary
	for i := range result {
		switch result[i].Role {
		case commonRuntimeHealth.RoleExecutionWorker:
			bounded = &result[i]
		case commonRuntimeHealth.RoleExecutionSupervisor:
			supervisor = &result[i]
		case commonRuntimeHealth.RoleContinuousWorker:
			continuous = &result[i]
		}
	}
	if bounded == nil || bounded.Status != "up" || bounded.HealthyInstances != 1 || bounded.Capacity != 4 || bounded.ActiveCount != 2 {
		t.Fatalf("bounded summary = %#v", bounded)
	}
	if supervisor == nil || supervisor.Status != "up" || supervisor.Capacity != 1 || supervisor.ActiveCount != 1 {
		t.Fatalf("supervisor summary = %#v", supervisor)
	}
	if continuous == nil || continuous.Status != "stopped" || continuous.HealthyInstances != 0 {
		t.Fatalf("continuous summary = %#v", continuous)
	}
}
