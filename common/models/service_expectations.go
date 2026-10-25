package models

import (
	"errors"
	"regexp"
	"time"
)

const ServiceExpectationLimit = 1000

var serviceExpectationName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,49}$`)
var serviceDeploymentID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ServiceGroupExpectation is deployment intent, never inferred from registrations.
type ServiceGroupExpectation struct {
	ModuleName       string    `json:"module_name"`
	Role             string    `json:"role"`
	RuntimeName      string    `json:"runtime_name"`
	ExpectedReplicas int       `json:"expected_replicas"`
	DesiredState     string    `json:"desired_state"`
	ChangedAt        time.Time `json:"changed_at"`
}

func (g ServiceGroupExpectation) Key() string {
	return g.ModuleName + "\x00" + g.Role + "\x00" + g.RuntimeName
}

func (g ServiceGroupExpectation) Valid(at time.Time) bool {
	return serviceExpectationName.MatchString(g.ModuleName) &&
		((g.Role == "backend" && g.RuntimeName == "") || (g.Role == "worker" && serviceExpectationName.MatchString(g.RuntimeName))) &&
		g.ExpectedReplicas >= 1 && g.ExpectedReplicas <= 1000 &&
		(g.DesiredState == "running" || g.DesiredState == "planned_stopped") &&
		!g.ChangedAt.IsZero() && !at.IsZero() && !g.ChangedAt.After(at)
}

type ServiceExpectationDocument struct {
	Schema       string                    `json:"schema"`
	DeploymentID string                    `json:"deployment_id"`
	Revision     int64                     `json:"revision"`
	Groups       []ServiceGroupExpectation `json:"groups"`
}

func (d ServiceExpectationDocument) Validate(at time.Time) error {
	if d.Schema != "addp.service-expectations/v1" || !serviceDeploymentID.MatchString(d.DeploymentID) ||
		d.Revision < 1 || d.Revision > 9007199254740991 || d.Groups == nil || len(d.Groups) > ServiceExpectationLimit {
		return errors.New("invalid service expectation document")
	}
	previous := ""
	for _, group := range d.Groups {
		if !group.Valid(at) || group.Key() <= previous {
			return errors.New("invalid service expectation group")
		}
		previous = group.Key()
	}
	return nil
}

// ServiceGroupObservation reports only current lease facts; no alert is derived here.
type ServiceGroupObservation struct {
	ServiceGroupExpectation
	State           string `json:"state"`
	Reason          string `json:"reason"`
	OnlineReplicas  int    `json:"online_replicas"`
	RegisteredCount int    `json:"registered_count"`
}

type ServiceGroupSnapshot struct {
	State        string                    `json:"state"`
	Reason       string                    `json:"reason"`
	DeploymentID string                    `json:"deployment_id"`
	Revision     int64                     `json:"revision"`
	ObservedAt   time.Time                 `json:"observed_at"`
	Groups       []ServiceGroupObservation `json:"groups"`
}

// Validate isolates deployment-intent failures from unrelated resource identities.
func (s ServiceGroupSnapshot) Validate(at time.Time) error {
	invalid := errors.New("invalid service group snapshot")
	if s.ObservedAt.IsZero() || !s.ObservedAt.Equal(at) || s.Groups == nil || len(s.Groups) > ServiceExpectationLimit {
		return invalid
	}
	if s.State != "available" {
		if len(s.Groups) != 0 {
			return invalid
		}
		switch {
		case s.State == "unconfigured" && s.Reason == "unconfigured":
		case s.State == "unavailable" && (s.Reason == "missing" || s.Reason == "unavailable" || s.Reason == "invalid_intent" || s.Reason == "source_changed" || s.Reason == "instance_budget"):
		default:
			return invalid
		}
		if (s.DeploymentID != "" || s.Revision != 0) && (!serviceDeploymentID.MatchString(s.DeploymentID) || s.Revision < 1 || s.Revision > 9007199254740991) {
			return invalid
		}
		return nil
	}
	if s.Reason != "" {
		return invalid
	}
	plan := ServiceExpectationDocument{Schema: "addp.service-expectations/v1", DeploymentID: s.DeploymentID, Revision: s.Revision, Groups: []ServiceGroupExpectation{}}
	total := 0
	for _, group := range s.Groups {
		plan.Groups = append(plan.Groups, group.ServiceGroupExpectation)
		if group.OnlineReplicas < 0 || group.RegisteredCount < group.OnlineReplicas || group.RegisteredCount > ObservabilityIdentityInstanceLimit {
			return invalid
		}
		total += group.RegisteredCount
		switch {
		case group.State == "observed" && group.Reason == "" && group.DesiredState == "running":
		case group.State == "paused" && ((group.Reason == "planned_stopped" && group.DesiredState == "planned_stopped") || (group.Reason == "module_disabled" && group.DesiredState == "running")):
		case group.State == "unknown" && group.DesiredState == "running" && (group.Reason == "module_unregistered" || group.Reason == "runtime_name_unavailable"):
		default:
			return invalid
		}
	}
	if total > ObservabilityIdentityInstanceLimit {
		return invalid
	}
	return plan.Validate(at)
}
