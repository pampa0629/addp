package models

import (
	"strings"
	"testing"
	"time"
)

func TestPostgreSQLMetricsSourcePrivateShape(t *testing.T) {
	now := time.Now().UTC()
	valid := func() PostgreSQLMetricsSource {
		return PostgreSQLMetricsSource{State: "available", Endpoint: "https://127.0.0.1:9443/metrics", ContainerID: strings.Repeat("a", 64), StartedAt: &now}
	}
	s := valid()
	if s.Validate() != nil {
		t.Fatal("valid source rejected")
	}
	for _, change := range []func(*PostgreSQLMetricsSource){
		func(s *PostgreSQLMetricsSource) { s.State = "collecting" },
		func(s *PostgreSQLMetricsSource) { s.Endpoint = "http://127.0.0.1:9443/metrics" },
		func(s *PostgreSQLMetricsSource) { s.Endpoint += "?token=private" },
		func(s *PostgreSQLMetricsSource) { s.ContainerID = "short" },
		func(s *PostgreSQLMetricsSource) { s.StartedAt = nil },
		func(s *PostgreSQLMetricsSource) { s.State = "not_deployed" },
	} {
		s := valid()
		change(&s)
		if s.Validate() == nil {
			t.Fatal("invalid source accepted")
		}
	}
	for _, state := range []string{"not_deployed", "unavailable"} {
		if (&PostgreSQLMetricsSource{State: state}).Validate() != nil {
			t.Fatal("empty state rejected")
		}
	}
}
