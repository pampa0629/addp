package semantic

import (
	"fmt"
	"regexp"
	"slices"
	"time"
	_ "time/tzdata"
)

const (
	ServiceString         = "string"
	ServiceDateOnOrBefore = "date_on_or_before_observation"
)

// ServiceBinding is part of a published rule's immutable revision.
type ServiceBinding struct {
	ServiceName    string         `json:"service_name"`
	ServiceID      uint64         `json:"service_id"`
	ServiceVersion string         `json:"service_version"`
	IDParameter    string         `json:"id_parameter"`
	IDField        string         `json:"id_field"`
	TimeZone       string         `json:"time_zone,omitempty"`
	Inputs         []ServiceInput `json:"inputs"`
}

type ServiceInput struct {
	Variable   string `json:"variable"`
	Field      string `json:"field"`
	Conversion string `json:"conversion" enums:"string,date_on_or_before_observation"`
}

var serviceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)
var serviceVersion = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validateServiceBinding(rule Rule, properties map[string]Property) error {
	b := rule.ServiceBinding
	if b == nil {
		return nil
	}
	invalid := fmt.Errorf("invalid_service_binding: %s", rule.ID)
	if b.ServiceID == 0 || !serviceName.MatchString(b.ServiceName) || !serviceVersion.MatchString(b.ServiceVersion) ||
		!identifier.MatchString(b.IDParameter) || !identifier.MatchString(b.IDField) || len(b.Inputs) != len(rule.Inputs) || len(b.Inputs) == 0 {
		return invalid
	}
	variables := make(map[string]Kind, len(rule.Inputs))
	for _, input := range rule.Inputs {
		variables[input.Variable] = properties[input.PropertyID].Kind
	}
	fields := map[string]bool{b.IDField: true}
	hasDate := false
	for _, input := range b.Inputs {
		if !identifier.MatchString(input.Field) || fields[input.Field] {
			return invalid
		}
		fields[input.Field] = true
		kind := variables[input.Variable]
		delete(variables, input.Variable)
		switch {
		case input.Conversion == ServiceString && kind == String:
		case input.Conversion == ServiceDateOnOrBefore && kind == Boolean:
			hasDate = true
		default:
			return invalid
		}
	}
	if !hasDate && b.TimeZone != "" {
		return invalid
	}
	if hasDate {
		if _, err := time.LoadLocation(b.TimeZone); err != nil || b.TimeZone == "" || b.TimeZone == "Local" {
			return invalid
		}
	}
	return nil
}

func (s *Snapshot) RuleServiceBinding(id string) (ServiceBinding, bool) {
	r, ok := s.rules[id]
	if !ok || r.definition.ServiceBinding == nil {
		return ServiceBinding{}, false
	}
	b := *r.definition.ServiceBinding
	b.Inputs = slices.Clone(b.Inputs)
	return b, true
}
