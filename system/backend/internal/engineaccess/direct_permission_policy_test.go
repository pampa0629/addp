package engineaccess

import (
	"fmt"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
)

func TestDirectPermissionExplicitSettingWins(t *testing.T) {
	for _, deployment := range []*catalogDeploymentDeclaration{nil, {Enabled: true, Version: 8}, {Enabled: false, Version: 9}} {
		for _, allow := range []bool{true, false} {
			got, err := resolveDirectPermission(&directPermissionSetting{Allowed: allow, Version: 4}, deployment)
			want := directPermissionDenied
			if allow {
				want = directPermissionAllowed
			}
			if err != nil || got.State != want || got.Source != directPermissionExplicit || got.Version != 4 {
				t.Fatalf("explicit decision = %+v, %v", got, err)
			}
		}
	}
	// An explicit decision does not read or depend on the deployment fact.
	if got, err := resolveDirectPermission(&directPermissionSetting{Allowed: false, Version: 4}, &catalogDeploymentDeclaration{}); err != nil || got.State != directPermissionDenied {
		t.Fatalf("explicit deny depended on unrelated deployment: %+v %v", got, err)
	}
}

func TestDirectPermissionDeploymentDefaultIsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		declaration   *catalogDeploymentDeclaration
		state, source string
		version       int64
	}{
		{nil, directPermissionUnknown, directPermissionDeploymentUnknown, 0},
		{&catalogDeploymentDeclaration{Enabled: true, Version: 7}, directPermissionDenied, directPermissionDeployment, 7},
		{&catalogDeploymentDeclaration{Enabled: false, Version: 8}, directPermissionAllowed, directPermissionDeployment, 8},
	} {
		got, err := resolveDirectPermission(nil, tc.declaration)
		if err != nil || got.State != tc.state || got.Source != tc.source || got.Version != tc.version {
			t.Fatalf("default = %+v, %v", got, err)
		}
	}
	if _, err := resolveDirectPermission(nil, &catalogDeploymentDeclaration{}); err == nil {
		t.Fatal("invalid declaration allowed")
	}
	if _, err := resolveDirectPermission(&directPermissionSetting{Allowed: true}, nil); err == nil {
		t.Fatal("invalid explicit fact allowed")
	}
}

func TestDirectPermissionFilterIncludesUnconfiguredIdentities(t *testing.T) {
	allowID, denyID, unconfiguredID := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	settings := map[string]directPermissionSetting{allowID: {Allowed: true, Version: 1}, denyID: {Allowed: false, Version: 5}}
	identities := []string{allowID, denyID, unconfiguredID}
	for _, tc := range []struct {
		deployment *catalogDeploymentDeclaration
		wanted     map[string][]string
	}{
		{nil, map[string][]string{"allowed": {allowID}, "denied": {denyID}, "unknown": {unconfiguredID}}},
		{&catalogDeploymentDeclaration{Enabled: true, Version: 2}, map[string][]string{"allowed": {allowID}, "denied": {denyID, unconfiguredID}, "unknown": {}}},
		{&catalogDeploymentDeclaration{Enabled: false, Version: 3}, map[string][]string{"allowed": {allowID, unconfiguredID}, "denied": {denyID}, "unknown": {}}},
	} {
		for state, want := range tc.wanted {
			query, err := directPermissionIdentityQuery(2, "exact_%", state, 3, 20, settings, tc.deployment)
			if err != nil || query.Page != 3 || query.PageSize != 20 || query.Search != "exact_%" {
				t.Fatalf("query: %+v %v", query, err)
			}
			var got []string
			for _, fingerprint := range identities {
				found := false
				for _, selected := range query.Fingerprints {
					if fingerprint == selected {
						found = true
					}
				}
				if (query.FingerprintFilter == "include" && found) || (query.FingerprintFilter == "exclude" && !found) {
					got = append(got, fingerprint)
				}
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("%s: got %v, want %v", state, got, want)
			}
		}
	}
	// Empty include is essential: 'allowed' with Catalog enabled and no explicit
	// settings must not collapse to an unfiltered all-DataItems query.
	query, err := directPermissionIdentityQuery(2, "", "allowed", 1, 20, nil, &catalogDeploymentDeclaration{Enabled: true, Version: 1})
	if err != nil || query.FingerprintFilter != "include" || len(query.Fingerprints) != 0 {
		t.Fatalf("empty include: %+v %v", query, err)
	}
}

func TestDirectPermissionFilterRejectsIncompleteOrInvalidObservations(t *testing.T) {
	oversized := make(map[string]directPermissionSetting)
	for i := 0; i <= authorizationfacts.MaxIdentityQueryFingerprints; i++ {
		oversized[fmt.Sprintf("%064x", i)] = directPermissionSetting{Allowed: true, Version: 1}
	}
	if _, err := directPermissionIdentityQuery(2, "", "allowed", 1, 20, oversized, nil); err == nil {
		t.Fatal("oversized predicate silently truncated")
	}
	for _, settings := range []map[string]directPermissionSetting{
		{"invalid": {Allowed: true, Version: 1}}, {strings.Repeat("a", 64): {Allowed: true, Version: 0}},
	} {
		if _, err := directPermissionIdentityQuery(2, "", "", 1, 20, settings, nil); err == nil {
			t.Fatal("invalid setting silently omitted")
		}
	}
	if _, err := directPermissionIdentityQuery(2, "", "bogus", 1, 20, nil, nil); err == nil {
		t.Fatal("unknown filter accepted")
	}
	if _, err := directPermissionIdentityQuery(2, "", "allowed", 1, 20, nil, &catalogDeploymentDeclaration{}); err == nil {
		t.Fatal("bad deployment interpreted as absent")
	}
}
