package models

import (
	"strings"
	"testing"
	"time"
)

func TestServiceGroupSnapshotRejectsInventedHealthAndInvalidCounts(t *testing.T) {
	at := time.Now().UTC()
	base := func() ServiceGroupSnapshot {
		return ServiceGroupSnapshot{State: "available", DeploymentID: strings.Repeat("a", 64), Revision: 1, ObservedAt: at, Groups: []ServiceGroupObservation{{ServiceGroupExpectation: ServiceGroupExpectation{ModuleName: "meta", Role: "worker", RuntimeName: "scan", ExpectedReplicas: 2, DesiredState: "running", ChangedAt: at.Add(-time.Minute)}, State: "observed", OnlineReplicas: 0, RegisteredCount: 2}}}
	}
	valid := base()
	if err := valid.Validate(at); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ServiceGroupSnapshot){
		func(s *ServiceGroupSnapshot) { s.ObservedAt = at.Add(time.Second) },
		func(s *ServiceGroupSnapshot) { s.Groups = nil },
		func(s *ServiceGroupSnapshot) { s.Revision = 0 },
		func(s *ServiceGroupSnapshot) { s.DeploymentID = "hostname" },
		func(s *ServiceGroupSnapshot) { s.Groups = append(s.Groups, s.Groups[0]) },
		func(s *ServiceGroupSnapshot) { s.Groups[0].State = "lost" },
		func(s *ServiceGroupSnapshot) { s.Groups[0].OnlineReplicas = -1 },
		func(s *ServiceGroupSnapshot) { s.Groups[0].OnlineReplicas = 3 },
		func(s *ServiceGroupSnapshot) { s.Groups[0].RegisteredCount = 10001 },
		func(s *ServiceGroupSnapshot) { s.Groups[0].ExpectedReplicas = 0 },
		func(s *ServiceGroupSnapshot) { s.Groups[0].ChangedAt = at.Add(time.Second) },
		func(s *ServiceGroupSnapshot) { s.Groups[0].DesiredState = "planned_stopped" },
		func(s *ServiceGroupSnapshot) { s.Groups[0].RuntimeName = "" },
		func(s *ServiceGroupSnapshot) { s.State = "unavailable"; s.Reason = "missing" },
	} {
		s := base()
		mutate(&s)
		if s.Validate(at) == nil {
			t.Fatalf("invalid snapshot accepted: %+v", s)
		}
	}
	for _, tc := range []struct{ state, reason, desired string }{{"unknown", "module_unregistered", "running"}, {"unknown", "runtime_name_unavailable", "running"}, {"paused", "module_disabled", "running"}, {"paused", "planned_stopped", "planned_stopped"}} {
		s := base()
		s.Groups[0].State = tc.state
		s.Groups[0].Reason = tc.reason
		s.Groups[0].DesiredState = tc.desired
		if err := s.Validate(at); err != nil {
			t.Fatal(tc, err)
		}
	}
	for _, reason := range []string{"missing", "unavailable", "invalid_intent", "source_changed", "instance_budget"} {
		s := ServiceGroupSnapshot{State: "unavailable", Reason: reason, ObservedAt: at, Groups: []ServiceGroupObservation{}}
		if err := s.Validate(at); err != nil {
			t.Fatal(reason, err)
		}
	}
}

func TestServiceExpectationOrderingMatchesTupleOrder(t *testing.T) {
	at := time.Now().UTC()
	plan := ServiceExpectationDocument{Schema: "addp.service-expectations/v1", DeploymentID: strings.Repeat("b", 64), Revision: 1, Groups: []ServiceGroupExpectation{}}
	for _, module := range []string{"a", "a-b", "aa"} {
		plan.Groups = append(plan.Groups, ServiceGroupExpectation{ModuleName: module, Role: "backend", ExpectedReplicas: 1, DesiredState: "running", ChangedAt: at})
	}
	if err := plan.Validate(at); err != nil {
		t.Fatal(err)
	}
	plan.Groups[0], plan.Groups[1] = plan.Groups[1], plan.Groups[0]
	if plan.Validate(at) == nil {
		t.Fatal("unsorted intent accepted")
	}
}
