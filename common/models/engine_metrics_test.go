package models

import (
	"strings"
	"testing"
	"time"
)

func TestEngineMetricsSourcePrivateShape(t *testing.T) {
	now := time.Now().UTC()
	valid := func() EngineMetricsSource {
		return EngineMetricsSource{State: "available", Endpoint: "https://127.0.0.1:9443/metrics", ContainerID: strings.Repeat("a", 64), StartedAt: &now}
	}
	s := valid()
	if s.Validate() != nil {
		t.Fatal("valid source rejected")
	}
	for _, change := range []func(*EngineMetricsSource){
		func(s *EngineMetricsSource) { s.State = "collecting" },
		func(s *EngineMetricsSource) { s.Endpoint = "http://127.0.0.1:9443/metrics" },
		func(s *EngineMetricsSource) { s.Endpoint += "?token=private" },
		func(s *EngineMetricsSource) { s.ContainerID = "short" },
		func(s *EngineMetricsSource) { s.StartedAt = nil },
		func(s *EngineMetricsSource) { s.State = "not_deployed" },
	} {
		s := valid()
		change(&s)
		if s.Validate() == nil {
			t.Fatal("invalid source accepted")
		}
	}
	for _, state := range []string{"not_deployed", "unavailable"} {
		if (&EngineMetricsSource{State: state}).Validate() != nil {
			t.Fatal("empty state rejected")
		}
	}
}

func TestRedisObservabilityIdentityRequiresItsOwnCanonicalService(t *testing.T) {
	identity := ObservabilityEngineIdentity{DeploymentID: "10000000-0000-4000-8000-000000000001", NodeID: "20000000-0000-4000-8000-000000000001", Version: 1, Scope: "business", EngineType: "redis", SourceKey: ObservabilityEngineSourceKey{DockerEngineID: "daemon", Project: "business", Service: "redis"}}
	if err := identity.Validate(); err != nil {
		t.Fatal(err)
	}
	identity.SourceKey.Service = "postgres"
	if identity.Validate() == nil {
		t.Fatal("Redis accepted PostgreSQL source key")
	}
}
