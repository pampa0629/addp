package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
)

func TestSourceReadSetPreservesPreciseTargets(t *testing.T) {
	base := testFulfillmentRequest()
	other := engineplugin.TabularItemPath(base.Path.EngineID, "schema", "业务.域", " 表/名")
	request := sourceReadRequest{TenantID: base.TenantID, Source: base.Operator, Targets: []engineplugin.EngineCatalogPath{base.Path, other, base.Path}}
	paths, encoded, err := request.encode()
	if err != nil || len(paths) != 2 || paths[0].Segments[2].Name != " 表/名 " || paths[1].Segments[2].Name != " 表/名" {
		t.Fatalf("precise dedup=%+v %s %v", paths, encoded, err)
	}
	var decoded []engineplugin.EngineCatalogPath
	if err := json.Unmarshal(encoded, &decoded); err != nil || len(decoded) != 2 {
		t.Fatalf("batch=%s %v", encoded, err)
	}
	request.Targets[0].Segments[2].Name = "changed"
	if paths[0].Segments[2].Name != " 表/名 " {
		t.Fatal("observation aliases caller targets")
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
