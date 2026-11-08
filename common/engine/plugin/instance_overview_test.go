package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInstanceOverviewExactValuesAndMissingness(t *testing.T) {
	metrics := []InstanceOverviewMetric{{Key: "zero", Status: "unavailable"}, {Key: "large", Status: "unavailable"}, {Key: "denied", Status: "permission_denied"}}
	metrics[0].SetValue(0)
	metrics[1].SetValue(9007199254740993)
	b, err := json.Marshal(metrics)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"value":"0"`) || !strings.Contains(s, `"value":"9007199254740993"`) || strings.Count(s, `"value"`) != 2 {
		t.Fatal(s)
	}
}

type unimplementedOverview struct{ MockPlugin }

func (p unimplementedOverview) Capabilities() EngineCapabilities {
	caps := p.MockPlugin.Capabilities()
	caps.InstanceOverview = true
	return caps
}

func TestInstanceOverviewRequiresProvider(t *testing.T) {
	p := &unimplementedOverview{MockPlugin: MockPlugin{TypeValue: "overview_fixture"}}
	if err := ValidatePluginCapabilities(p); err == nil || !strings.Contains(err.Error(), "InstanceOverviewProvider") {
		t.Fatal(err)
	}
}
