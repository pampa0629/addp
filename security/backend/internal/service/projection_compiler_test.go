package service

import (
	"testing"

	"github.com/addp/common/dataprotection"
)

func requireManagerProjectionEffects(t *testing.T, projection *dataprotection.Projection, previewEffect string) {
	t.Helper()
	if projection == nil || len(projection.Rules) != 2 {
		t.Fatalf("manager projection = %#v, want preview and profile rules", projection)
	}
	wantProfile := dataprotection.EffectSuppress
	if previewEffect == dataprotection.EffectDeny {
		wantProfile = dataprotection.EffectDeny
	}
	for action, want := range map[string]string{
		managerPreviewAction: previewEffect,
		managerProfileAction: wantProfile,
	} {
		matched := false
		for _, rule := range projection.Rules {
			if rule.Action == action {
				matched = true
				if rule.Decision.Effect != want {
					t.Fatalf("manager %s effect = %q, want %q", action, rule.Decision.Effect, want)
				}
			}
		}
		if !matched {
			t.Fatalf("manager projection has no %s rule: %#v", action, projection.Rules)
		}
	}
}

func managerProjectionRule(t *testing.T, projection *dataprotection.Projection, action string) dataprotection.Rule {
	t.Helper()
	if projection != nil {
		for _, rule := range projection.Rules {
			if rule.Action == action {
				return rule
			}
		}
	}
	t.Fatalf("manager projection has no %s rule: %#v", action, projection)
	return dataprotection.Rule{}
}

func requireDevelopQueryProjection(t *testing.T, projection *dataprotection.Projection, effect string) {
	t.Helper()
	if projection == nil || projection.ConsumerOwner != "develop" || projection.State != dataprotection.ProjectionStateActive || len(projection.Rules) != 1 {
		t.Fatalf("develop projection = %#v, want one active query rule", projection)
	}
	rule := projection.Rules[0]
	if rule.Action != developQueryAction || rule.Decision.Effect != effect {
		t.Fatalf("develop query rule = %#v, want effect %q", rule, effect)
	}
}

func requireServiceExecuteProjection(t *testing.T, projection *dataprotection.Projection, effect string) {
	t.Helper()
	if projection == nil || projection.ConsumerOwner != "service" || projection.State != dataprotection.ProjectionStateActive || len(projection.Rules) != 1 {
		t.Fatalf("service projection = %#v, want one active service_execute rule", projection)
	}
	rule := projection.Rules[0]
	if rule.Action != serviceExecuteAction || rule.Decision.Effect != effect {
		t.Fatalf("service execute rule = %#v, want effect %q", rule, effect)
	}
}

func requireTransferExportProjection(t *testing.T, projection *dataprotection.Projection, effect string) {
	t.Helper()
	if projection == nil || projection.ConsumerOwner != "transfer" || projection.State != dataprotection.ProjectionStateActive || len(projection.Rules) != 1 {
		t.Fatalf("transfer projection = %#v, want one active export rule", projection)
	}
	rule := projection.Rules[0]
	if rule.Action != transferExportAction || rule.Decision.Effect != effect {
		t.Fatalf("transfer export rule = %#v, want effect %q", rule, effect)
	}
}

func TestManagerProfileDecisionIsSystemDerivedAndNeverMasks(t *testing.T) {
	for _, test := range []struct {
		preview string
		profile string
	}{
		{preview: dataprotection.EffectMask, profile: dataprotection.EffectSuppress},
		{preview: dataprotection.EffectSuppress, profile: dataprotection.EffectSuppress},
		{preview: dataprotection.EffectDeny, profile: dataprotection.EffectDeny},
	} {
		decision, err := managerProfileDecision(dataprotection.Decision{Effect: test.preview})
		if err != nil || decision.Effect != test.profile || decision.InvalidValueEffect != test.profile {
			t.Fatalf("managerProfileDecision(%q) = %#v, %v", test.preview, decision, err)
		}
	}
	if _, err := managerProfileDecision(dataprotection.Decision{Effect: dataprotection.EffectAllow}); err == nil {
		t.Fatal("allow preview decision must not compile into a profile rule")
	}
}
