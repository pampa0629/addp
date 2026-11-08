package postgresql

import "testing"

func TestInstanceOverviewDoesNotCertifyProtocolAdapters(t *testing.T) {
	base := &PostgreSQLPlugin{}
	adapter := NewProtocolCompatiblePlugin(ProtocolIdentity{EngineType: "uncertified_overview", DisplayName: "Protocol fixture", DefaultPort: 5432})
	if !base.Capabilities().InstanceOverview || adapter.Capabilities().InstanceOverview {
		t.Fatal("overview capability leaked to protocol adapter")
	}
}
