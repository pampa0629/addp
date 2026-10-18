package models

import (
	"testing"
	"time"

	plugin "github.com/addp/common/engine/plugin"
)

func TestDecodeTargetProductionRejectsUnprovenFacts(t *testing.T) {
	now := time.Now().UTC()
	valid := TargetProduction{SchemaVersion: TargetProductionSchema, TenantID: 1, ExecutionID: "execution", Attempt: 1, TargetLocator: "addp://engine/2/path/outdoor/persons?type=table", Phase: TargetProductionPending, StartedAt: now}
	for name, change := range map[string]func(*TargetProduction){
		"unknown schema":      func(r *TargetProduction) { r.SchemaVersion = "other" },
		"missing tenant":      func(r *TargetProduction) { r.TenantID = 0 },
		"missing attempt":     func(r *TargetProduction) { r.Attempt = 0 },
		"self retry":          func(r *TargetProduction) { r.RetryOfExecutionID = &r.ExecutionID },
		"pending preparation": func(r *TargetProduction) { r.Preparation = plugin.TableTargetCreated },
		"pending commit":      func(r *TargetProduction) { r.CommittedAt = &now },
		"unknown phase":       func(r *TargetProduction) { r.Phase = "success" },
		"missing preparation": func(r *TargetProduction) { r.Phase = TargetProductionPrepared; r.PreparedAt = &now },
		"missing prepare time": func(r *TargetProduction) {
			r.Phase = TargetProductionPrepared
			r.Preparation = plugin.TableTargetCreated
		},
		"prepare before start": func(r *TargetProduction) {
			r.Phase, r.Preparation = TargetProductionPrepared, plugin.TableTargetCreated
			before := now.Add(-time.Second)
			r.PreparedAt = &before
		},
		"unproven commit": func(r *TargetProduction) {
			r.Phase, r.Preparation, r.PreparedAt = TargetProductionCommitted, plugin.TableTargetCreated, &now
		},
		"commit before prepare": func(r *TargetProduction) {
			r.Phase, r.Preparation, r.PreparedAt = TargetProductionCommitted, plugin.TableTargetCreated, &now
			before := now.Add(-time.Second)
			r.CommittedAt = &before
		},
	} {
		t.Run(name, func(t *testing.T) {
			record := valid
			change(&record)
			if _, err := EncodeTargetProduction(record); err == nil {
				t.Fatal("invalid provenance accepted")
			}
		})
	}
	encoded, err := EncodeTargetProduction(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTargetProduction(encoded); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeTargetProduction(map[string]interface{}{"unexpected": true}); err == nil {
		t.Fatal("unknown record fields accepted")
	}
	if _, err := DecodeTargetProduction(encoded + `{}`); err == nil {
		t.Fatal("trailing private record accepted")
	}
	retired := valid
	retired.SchemaVersion = "transfer.target-production/v1"
	if _, err := EncodeTargetProduction(retired); err == nil {
		t.Fatal("retired production evidence upgraded implicitly")
	}
}
