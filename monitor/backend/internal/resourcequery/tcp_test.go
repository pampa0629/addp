package resourcequery

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTCPFixedScalarScopeAndIntegerEvidence(t *testing.T) {
	const key = "node.tcp.current_connections"
	p, err := NewPlan([]string{key}, testTime.Add(-time.Minute), testTime, testTime, false, nil, DefaultBudget())
	if err != nil || p.SeriesUpperBound != 1 || len(p.Metrics) != 1 || p.Metrics[0].Unit != "connections" || p.Metrics[0].Grouped() || p.Metrics[0].WindowSeconds != 0 {
		t.Fatal(p, err)
	}
	expression, err := p.Expression(Scope{NodeID: testNode, Instance: "host:9100"})
	if err != nil || !strings.Contains(expression, `node_netstat_Tcp_CurrEstab{job="addp_nodes",addp_node_id="`+testNode+`"`) || !strings.Contains(expression, `instance="host:9100"`) || !strings.Contains(expression, "count(node_netstat_Tcp_CurrEstab") || strings.Contains(expression, "rate(") {
		t.Fatal(expression, err)
	}
	if _, err := NewPlan([]string{key}, testTime, testTime, testTime, false, Dimensions{"device": "eth0"}, DefaultBudget()); !errors.Is(err, ErrInvalid) {
		t.Fatal("TCP was treated as per-interface", err)
	}
	row := func(component, value string) string {
		return fmt.Sprintf(`{"metric":{"addp_metric":%q,"addp_component":%q},"value":[1800000000,%q]}`, key, component, value)
	}
	for _, value := range []string{"0", "19", "9007199254740991", "1.5", "9007199254740992", "-1", "NaN", "+Inf"} {
		rows, err := normalize(wire(t, data(row("value", value)+","+row("sampled_at", "1800000000"), false)), p, DefaultBudget())
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		valid := value == "0" || value == "19" || value == "9007199254740991"
		if (rows[0].Points[0].DataState == "valid") != valid || (rows[0].Points[0].Value != nil) != valid {
			t.Fatalf("%s: %+v", value, rows)
		}
	}
	for _, sample := range []string{"1799999939", "1800000001"} {
		rows, err := normalize(wire(t, data(row("value", "19")+","+row("sampled_at", sample), false)), p, DefaultBudget())
		if err != nil || rows[0].Points[0].DataState == "valid" {
			t.Fatal(rows, err)
		}
	}
	rows, err := normalize(wire(t, data("", false)), p, DefaultBudget())
	if err != nil || rows[0].Points[0].DataState != "no_data" || rows[0].Points[0].Value != nil {
		t.Fatal("absent TCP became zero", rows, err)
	}
}
