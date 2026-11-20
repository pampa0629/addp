package api

import "testing"

func TestModuleEndpointReadTargets(t *testing.T) {
	for _, tc := range []struct {
		caller, module string
		allowed        bool
	}{
		{"addp-monitor", "meta", true}, {"addp-monitor", "catalog", false}, {"addp-transfer", "manager", true},
		{"addp-transfer", "develop", true}, {"addp-transfer", "meta", false}, {"addp-manager", "meta", false},
		{"addp-gateway", "meta", false}, {"addp-monitor", "", false},
	} {
		if got := moduleEndpointTargetAllowed(tc.caller, tc.module); got != tc.allowed {
			t.Errorf("%s -> %s allowed=%v", tc.caller, tc.module, got)
		}
	}
}
