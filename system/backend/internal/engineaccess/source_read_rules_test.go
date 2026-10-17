package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	engineplugin "github.com/addp/common/engine/plugin"
)

func TestSourceReadSetPreservesPreciseTargets(t *testing.T) {
	base := testFulfillmentRequest()
	other := engineplugin.TabularItemPath(base.Path.EngineID, "schema", "业务.域", " 表/名")
	request := sourceReadRequest{TenantID: base.TenantID, Source: base.Operator, Targets: []engineplugin.EngineCatalogPath{base.Path, other, base.Path}, Bindings: testReadBindings(base.Path, other)}
	paths, encoded, err := request.encode()
	if err != nil || len(paths) != 2 || paths[0].Segments[2].Name != " 表/名 " || paths[1].Segments[2].Name != " 表/名" {
		t.Fatalf("precise dedup=%+v %s %v", paths, encoded, err)
	}
	var decoded []authorizationfacts.Binding
	if err := json.Unmarshal(encoded, &decoded); err != nil || len(decoded) != 2 {
		t.Fatalf("batch=%s %v", encoded, err)
	}
	request.Targets[0].Segments[2].Name = "changed"
	if paths[0].Segments[2].Name != " 表/名 " {
		t.Fatal("observation aliases caller targets")
	}
}

func TestSourceReadCredentialRejectsIncompleteInputBeforeDatabase(t *testing.T) {
	path := testFulfillmentRequest().Path
	for _, targets := range [][]engineplugin.EngineCatalogPath{nil, {engineplugin.TabularNamespacePath(path.EngineID, "schema", "public")}} {
		result, err := NewRepository(nil).testReadCurrentUserSourceRules(context.Background(), "addp_at_not_loaded", targets)
		if result != nil || !errors.Is(err, errSourceReadRules) {
			t.Fatalf("incomplete credential target set=%+v %v", result, err)
		}
	}
	if result, err := NewRepository(nil).testReadCurrentUserSourceRules(context.Background(), "", []engineplugin.EngineCatalogPath{path}); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("empty credential=%+v %v", result, err)
	}
	if result, err := (*Repository)(nil).testReadCurrentUserSourceRules(context.Background(), "addp_at_not_loaded", []engineplugin.EngineCatalogPath{path}); result != nil || !errors.Is(err, errSourceReadRules) {
		t.Fatalf("missing repository=%+v %v", result, err)
	}
}

func TestSourceReadSetRejectsIncompleteInputsBeforeDatabase(t *testing.T) {
	for _, mutate := range []func(*sourceReadRequest){
		func(r *sourceReadRequest) { r.TenantID = 0 },
		func(r *sourceReadRequest) { r.Source = userProvenance{} },
		func(r *sourceReadRequest) { r.Source.MembershipID = -1 },
		func(r *sourceReadRequest) { r.Source.AuthorizationVersion = 0 },
		func(r *sourceReadRequest) { r.Targets = nil },
		func(r *sourceReadRequest) { r.Targets[0].EngineID = 0 },
		func(r *sourceReadRequest) { r.Targets[0].Version = "unknown" },
		func(r *sourceReadRequest) {
			r.Targets = append(r.Targets, engineplugin.TabularNamespacePath(r.Targets[0].EngineID, "schema", "public"))
		},
	} {
		base := testFulfillmentRequest()
		request := sourceReadRequest{TenantID: base.TenantID, Source: base.Operator, Targets: []engineplugin.EngineCatalogPath{base.Path}}
		mutate(&request)
		result, err := NewRepository(nil).readCurrentSourceRules(context.Background(), request)
		if result != nil || !errors.Is(err, errSourceReadRules) {
			t.Fatalf("invalid input=%+v result=%+v %v", request, result, err)
		}
	}
}

func TestSourceReadObservationFailsClosedForPartialOrMixedResults(t *testing.T) {
	paths := []engineplugin.EngineCatalogPath{testFulfillmentRequest().Path, testFulfillmentRequest().Path}
	now := time.Now().UTC()
	base := []sourceReadRuleRow{{Position: 1, ObservedAt: now, Reason: "grant"}, {Position: 2, ObservedAt: now, Reason: "grant"}}
	for _, reason := range []string{"explicit_deny", "no_grant", "source_unavailable", "target_unavailable"} {
		rows := append([]sourceReadRuleRow(nil), base...)
		rows[1].Reason = reason
		result, err := sourceReadObservation(paths, rows)
		if err != nil || result.Covered || !result.Targets[0].Covered || result.Targets[1].Covered {
			t.Fatalf("mixed set=%+v %v", result, err)
		}
	}
	for _, mutate := range []func([]sourceReadRuleRow) []sourceReadRuleRow{
		func(rows []sourceReadRuleRow) []sourceReadRuleRow { return rows[:1] },
		func(rows []sourceReadRuleRow) []sourceReadRuleRow { rows[1].Position = 1; return rows },
		func(rows []sourceReadRuleRow) []sourceReadRuleRow {
			rows[1].ObservedAt = now.Add(time.Microsecond)
			return rows
		},
		func(rows []sourceReadRuleRow) []sourceReadRuleRow { rows[0].ObservedAt = time.Time{}; return rows },
		func(rows []sourceReadRuleRow) []sourceReadRuleRow { rows[1].Reason = "unknown"; return rows },
	} {
		result, err := sourceReadObservation(paths, mutate(append([]sourceReadRuleRow(nil), base...)))
		if result != nil || !errors.Is(err, errSourceReadRules) {
			t.Fatalf("malformed result allowed=%+v %v", result, err)
		}
	}
	if result, err := sourceReadObservation(paths, base); err != nil || !result.Covered {
		t.Fatalf("complete set=%+v %v", result, err)
	}
}

func TestSourceReadRulesConsumeOnlyCanonicalGrantFacts(t *testing.T) {
	if strings.Contains(currentSourceReadRulesSQL, "fulfillment_outcomes") || strings.Contains(currentSourceReadRulesSQL, "binding->") {
		t.Fatal("source read rules must not reconstruct authorization from Catalog receipt history")
	}
	for _, fact := range []string{"g.item_fingerprint = target.item_fingerprint", "g.boundary_hash = target.boundary_hash", "g.recipient_type", "g.recipient_id", "g.action = 'read'", "g.expires_at", "engine_access_grant_revocations"} {
		if !strings.Contains(currentSourceReadRulesSQL, fact) {
			t.Fatalf("canonical Grant observation lost %s", fact)
		}
	}
}

func TestSourceReadObservationCombinesEffectiveDeadlines(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	early, late := now.Add(time.Hour), now.Add(2*time.Hour)
	paths := []engineplugin.EngineCatalogPath{testFulfillmentRequest().Path, testFulfillmentRequest().Path}
	for _, tc := range []struct {
		name         string
		first, next  *time.Time
		nextReason   string
		covered      bool
		wantDeadline *time.Time
	}{
		{"all unlimited", nil, nil, "grant", true, nil},
		{"finite and unlimited", &late, nil, "grant", true, &late},
		{"unlimited and finite", nil, &early, "grant", true, &early},
		{"earliest required source", &late, &early, "grant", true, &early},
		{"same deadline", &early, &early, "grant", true, &early},
		{"missing source", &late, nil, "no_grant", false, nil},
		{"denied source", &late, nil, "explicit_deny", false, nil},
		{"unavailable source", &late, nil, "target_unavailable", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := []sourceReadRuleRow{
				{Position: 1, ObservedAt: now, Reason: "grant", ExpiresAt: tc.first},
				{Position: 2, ObservedAt: now, Reason: tc.nextReason, ExpiresAt: tc.next},
			}
			result, err := sourceReadObservation(paths, rows)
			if err != nil || result == nil || result.Covered != tc.covered {
				t.Fatalf("observation=%+v error=%v", result, err)
			}
			if (result.ExpiresAt == nil) != (tc.wantDeadline == nil) ||
				(result.ExpiresAt != nil && !result.ExpiresAt.Equal(*tc.wantDeadline)) {
				t.Fatalf("deadline=%v want=%v", result.ExpiresAt, tc.wantDeadline)
			}
		})
	}
	result, err := sourceReadObservation(paths[:1], []sourceReadRuleRow{{Position: 1, ObservedAt: now, Reason: "grant", ExpiresAt: &early}})
	if err != nil {
		t.Fatal(err)
	}
	*result.Targets[0].ExpiresAt = late
	if !early.Equal(now.Add(time.Hour)) || !result.ExpiresAt.Equal(early) {
		t.Fatal("source deadline aliases input or the complete-set deadline")
	}
}

func TestSourceReadObservationRejectsInvalidDeadlines(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Hour)
	path := []engineplugin.EngineCatalogPath{testFulfillmentRequest().Path}
	for _, row := range []sourceReadRuleRow{
		{Position: 1, ObservedAt: now, Reason: "grant", ExpiresAt: &past},
		{Position: 1, ObservedAt: now, Reason: "grant", ExpiresAt: &now},
		{Position: 1, ObservedAt: now, Reason: "explicit_deny", ExpiresAt: &future},
		{Position: 1, ObservedAt: now, Reason: "no_grant", ExpiresAt: &future},
	} {
		if result, err := sourceReadObservation(path, []sourceReadRuleRow{row}); result != nil || !errors.Is(err, errSourceReadRules) {
			t.Fatalf("invalid source deadline accepted: %+v %v", result, err)
		}
	}
}
