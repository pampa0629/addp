package models

import (
	"strings"
	"testing"
	"time"
)

func TestComponentIdentityBindsCurrentRuntime(t *testing.T) {
	start := time.Now().UTC()
	item := ObservabilityComponentIdentity{DockerEngineID: "docker-test", Project: "addp-infra", Kind: "redpanda", ContainerID: strings.Repeat("a", 64), StartedAt: start, Metrics: &EngineMetricsSource{State: "not_deployed"}}
	if item.Validate() != nil {
		t.Fatal("valid component rejected")
	}
	id := item.ID()
	item.ContainerID = strings.Repeat("b", 64)
	if item.ID() != id {
		t.Fatal("runtime replacement changed stable deployment identity")
	}
	item.Metrics = &EngineMetricsSource{State: "available", Endpoint: "https://127.0.0.1:9443/metrics", ContainerID: strings.Repeat("a", 64), StartedAt: &start}
	if item.Validate() == nil {
		t.Fatal("old runtime source admitted")
	}
	item.Metrics.ContainerID = item.ContainerID
	if item.Validate() != nil {
		t.Fatal("current runtime rejected")
	}
	item.Project = "business"
	if item.Validate() == nil {
		t.Fatal("non-Infra component admitted")
	}
}
