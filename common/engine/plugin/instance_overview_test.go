package plugin_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
)

func TestInstanceOverviewExactValuesAndMissingness(t *testing.T) {
	metrics := []plugin.InstanceOverviewMetric{{Key: "zero", Status: "unavailable"}, {Key: "large", Status: "unavailable"}, {Key: "denied", Status: "permission_denied"}}
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

type unimplementedOverview struct{ plugin.EnginePlugin }

func (p unimplementedOverview) Capabilities() plugin.EngineCapabilities {
	caps := p.EnginePlugin.Capabilities()
	caps.InstanceOverview = true
	return caps
}

func TestInstanceOverviewRequiresProviderAndDoesNotCertifyProtocolAdapters(t *testing.T) {
	base := &postgresql.PostgreSQLPlugin{}
	if err := plugin.ValidatePluginCapabilities(unimplementedOverview{EnginePlugin: base}); err == nil || !strings.Contains(err.Error(), "InstanceOverviewProvider") {
		t.Fatal(err)
	}
	adapter := postgresql.NewProtocolCompatiblePlugin(postgresql.ProtocolIdentity{EngineType: "uncertified_overview", DisplayName: "Protocol fixture", DefaultPort: 5432})
	if !base.Capabilities().InstanceOverview || adapter.Capabilities().InstanceOverview {
		t.Fatal("overview capability leaked to protocol adapter")
	}
}
