package service

import (
	"github.com/addp/common/query/plan"
	"testing"
)

func TestMetricNeutralPlanBuildsBothOperations(t *testing.T) {
	for _, operation := range []string{"count_distinct", "directional_overlap"} {
		t.Run(operation, func(t *testing.T) {
			contract, bindings := metricGoldenContract()
			contract.Operation = operation
			if operation == "directional_overlap" {
				contract.Filters = nil
			}
			p, err := buildMetricPlan(contract, bindings)
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.Validate(p); err != nil {
				t.Fatal(err)
			}
			if len(p.Assertions) < 3 {
				t.Fatal("missing independent quality assertions")
			}
		})
	}
}

func TestMetricDetailsRequireExplicitCountContract(t *testing.T) {
	c, b := metricGoldenContract()
	if _, err := buildMetricResultPlan(c, b, true); err == nil {
		t.Fatal("undeclared details accepted")
	}
	c.IncludeDetails = true
	p, err := buildMetricResultPlan(c, b, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Validate(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Output.StableKey) != 3 || p.Output.StableKey[2] != "member" {
		t.Fatal(p.Output)
	}
	if len(p.Assertions) < 3 {
		t.Fatal("details lost source assertions")
	}
	c.Operation = "directional_overlap"
	c.Filters = nil
	if _, err := buildMetricPlan(c, b); err == nil {
		t.Fatal("overlap details accepted")
	}
}
