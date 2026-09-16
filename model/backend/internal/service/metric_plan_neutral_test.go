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
