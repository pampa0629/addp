package service

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
)

type cdcIdentities struct {
	snapshot commonmodels.ObservabilityIdentitySnapshot
}

func (r *cdcIdentities) GetObservabilityIdentities(context.Context) (*commonmodels.ObservabilityIdentitySnapshot, error) {
	raw, _ := json.Marshal(r.snapshot)
	var result commonmodels.ObservabilityIdentitySnapshot
	json.Unmarshal(raw, &result)
	return &result, nil
}

type cdcBackend struct {
	calls int
	after func()
	plan  resourcequery.Plan
}

func (b *cdcBackend) CDCQuery(_ context.Context, p resourcequery.Plan, s resourcequery.CDCScope, _ resourcequery.Budget) (resourcequery.Summary, error) {
	b.calls++
	b.plan = p
	if b.after != nil {
		b.after()
	}
	state := "collecting"
	series := resourcequery.Empty(p, "no_data")
	if s.Component.Kind == "kafka-connect" {
		state = "failed"
	} else {
		for i := range series {
			for j := range series[i].Points {
				point := &series[i].Points[j]
				v := 0.0
				stamp := point.EvaluatedAt
				point.Value = &v
				point.SampledAt = &stamp
				point.DataState = "valid"
			}
		}
	}
	at := p.End
	return resourcequery.Summary{Series: series, Collection: resourcequery.Collection{State: state, SampledAt: &at}}, nil
}
func TestCDCObservationCurrentIdentityBudgetAndPeerIsolation(t *testing.T) {
	for _, change := range []string{"valid", "source", "restart", "policy", "stale", "budget", "unavailable", "empty", "duplicate", "dns"} {
		t.Run(change, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			started := now.Add(-time.Minute)
			reader := &cdcIdentities{snapshot: commonmodels.ObservabilityIdentitySnapshot{ObservedAt: now, InfrastructureComponents: &commonmodels.ObservabilityComponentSnapshot{State: "available", Items: []commonmodels.ObservabilityComponentIdentity{}}}}
			for i, kind := range []string{"redpanda", "kafka-connect"} {
				reader.snapshot.InfrastructureComponents.Items = append(reader.snapshot.InfrastructureComponents.Items, commonmodels.ObservabilityComponentIdentity{DockerEngineID: "fixture", Project: "addp-infra", Kind: kind, ContainerID: strings.Repeat(string(rune('a'+i)), 64), StartedAt: started, Metrics: &commonmodels.EngineMetricsSource{State: "available", Endpoint: []string{"https://127.0.0.1:9100/metrics", "https://127.0.0.2:9100/metrics"}[i], ContainerID: strings.Repeat(string(rune('a'+i)), 64), StartedAt: &started}})
			}
			policies := &resourcePolicies{row: models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget(), Version: 1}}
			backend := &cdcBackend{}
			svc := NewResourceObservationService(policies, nil, nil, nil, true, resourceSourcePolicyWithNetwork(t, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, []uint16{9100}), ProcessObservationDependencies{}).WithCDC(CDCObservationDependencies{Identities: reader, Backend: backend})
			svc.now = func() time.Time { return now }
			switch change {
			case "source":
				backend.after = func() {
					reader.snapshot.InfrastructureComponents.Items[0].Metrics.Endpoint = "https://127.0.0.3:9100/metrics"
				}
			case "restart":
				backend.after = func() {
					item := &reader.snapshot.InfrastructureComponents.Items[0]
					next := started.Add(time.Second)
					item.StartedAt = next
					item.Metrics.StartedAt = &next
				}
			case "policy":
				backend.after = func() { policies.row.Version++ }
			case "stale":
				reader.snapshot.ObservedAt = now.Add(-6 * time.Second)
			case "budget":
				policies.row.MaxSeries = 17
			case "unavailable":
				reader.snapshot.InfrastructureComponents = &commonmodels.ObservabilityComponentSnapshot{State: "unavailable", Items: []commonmodels.ObservabilityComponentIdentity{}}
			case "empty":
				reader.snapshot.InfrastructureComponents.Items = []commonmodels.ObservabilityComponentIdentity{}
			case "duplicate":
				reader.snapshot.InfrastructureComponents.Items[1].Kind = "redpanda"
			case "dns":
				reader.snapshot.InfrastructureComponents.Items[1].Metrics.Endpoint = "https://192.0.2.1:9100/metrics"
			}
			result, err := svc.CDCResources(context.Background(), "user", "", nil, time.Time{}, time.Time{}, false)
			if change == "valid" || change == "dns" {
				if err != nil || len(result.Data) != 2 || result.Data[0].Collection.State != "collecting" || result.Data[0].Series[0].Points[0].Value == nil || *result.Data[0].Series[0].Points[0].Value != 0 {
					t.Fatalf("peer data lost: %+v %v", result, err)
				}
				want := "failed"
				if change == "dns" {
					want = "source_unavailable"
				}
				if result.Data[1].Collection.State != want || result.Data[1].Series[0].Points[0].Value != nil {
					t.Fatalf("bad source was healthy: %+v", result.Data[1])
				}
			} else if change == "empty" || change == "unavailable" {
				if err != nil || result.Data == nil || len(result.Data) != 0 || result.DiscoveryState != reader.snapshot.InfrastructureComponents.State {
					t.Fatalf("empty/unavailable conflated: %+v %v", result, err)
				}
			} else if err == nil || len(result.Data) > 0 {
				t.Fatalf("changed/invalid read disclosed data: %+v %v", result, err)
			}
			if (change == "stale" || change == "budget" || change == "duplicate") && backend.calls != 0 {
				t.Fatal("queried before shared guards")
			}
			if change == "valid" {
				result, err = svc.CDCResources(context.Background(), "user", reader.snapshot.InfrastructureComponents.Items[0].ID(), []string{"cdc.redpanda.disk_total_bytes"}, now.Add(-time.Hour), now, true)
				if err != nil || result.Start.Before(started) || backend.plan.Points > 1000 {
					t.Fatalf("old runtime range: %+v %v", result, err)
				}
				_, err = svc.CDCResources(context.Background(), "user", reader.snapshot.InfrastructureComponents.Items[0].ID(), []string{"cdc.connect.tasks"}, now.Add(-time.Hour), now, true)
				if err != resourcequery.ErrInvalid {
					t.Fatal("cross catalog", err)
				}
			}
		})
	}
}
func TestCDCDisabledBeforePrivateReads(t *testing.T) {
	svc := NewResourceObservationService(&resourcePolicies{row: models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget()}}, nil, nil, nil, false, nil, ProcessObservationDependencies{})
	// Missing configuration is never reported as healthy or an empty inventory.
	_, err := svc.CDCResources(context.Background(), "user", "", nil, time.Time{}, time.Time{}, false)
	if err != metricsdiscovery.ErrUnconfigured {
		t.Fatal(err)
	}
}
