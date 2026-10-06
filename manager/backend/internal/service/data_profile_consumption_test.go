package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/google/uuid"
)

type profileConsumerForTest struct {
	calls   int
	err     error
	empty   bool
	zero    bool
	tenant  uint
	id      string
	request commonClient.ManagerProfileAccessRequest
	onCheck func()
}

func (c *profileConsumerForTest) CheckProfileAccess(_ context.Context, tenant uint, id string, request commonClient.ManagerProfileAccessRequest) (*commonClient.ManagerProfileAccessObservation, error) {
	c.calls++
	c.tenant, c.id, c.request = tenant, id, request
	if c.onCheck != nil {
		c.onCheck()
	}
	if c.err != nil || c.empty {
		return nil, c.err
	}
	if c.zero {
		return &commonClient.ManagerProfileAccessObservation{}, nil
	}
	return &commonClient.ManagerProfileAccessObservation{ObservedAt: time.Now().UTC()}, nil
}

// A deterministic worker fixture, not a substitute for the PostgreSQL claim
// and System current-lease integration gates.
func claimProfileForTest(item *commonExecution.TaskExecution) {
	token, owner, until := uuid.NewString(), "profile-test-worker", time.Now().Add(time.Minute)
	item.Status, item.Attempt = commonExecution.ExecutionStatusRunning, 1
	item.LeaseToken, item.LeaseOwner, item.LeaseExpiresAt = &token, &owner, &until
}

func profileClaimContextForTest(t *testing.T, item *commonExecution.TaskExecution) context.Context {
	t.Helper()
	lease, err := commonExecution.LeaseFromExecution(*item)
	if err != nil {
		t.Fatal(err)
	}
	return commonExecution.ContextWithLease(t.Context(), lease)
}

func TestDataProfileWorkerRejectsUnboundOrMismatchedClaimBeforeResolution(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*commonExecution.TaskExecution)
	}{
		{"pending", func(e *commonExecution.TaskExecution) { e.Status = commonExecution.ExecutionStatusPending }},
		{"wrong owner module", func(e *commonExecution.TaskExecution) { e.Module = "meta" }},
		{"wrong source", func(e *commonExecution.TaskExecution) { e.Source = "orchestrator" }},
		{"wrong task", func(e *commonExecution.TaskExecution) { e.TaskType = "embedding" }},
		{"wrong boundary", func(e *commonExecution.TaskExecution) { e.ExecutionBoundary = "unbounded" }},
		{"scheduled", func(e *commonExecution.TaskExecution) { e.TriggerType = "scheduled" }},
		{"parent execution", func(e *commonExecution.TaskExecution) { id := uuid.NewString(); e.ParentExecutionID = &id }},
		{"missing authorization", func(e *commonExecution.TaskExecution) { e.ExecutionAuthorizationID = nil }},
		{"invalid authorization", func(e *commonExecution.TaskExecution) { id := int64(0); e.ExecutionAuthorizationID = &id }},
		{"missing authorization expiry", func(e *commonExecution.TaskExecution) { e.AuthorizationExpiresAt = nil }},
		{"missing lease expiry", func(e *commonExecution.TaskExecution) { e.LeaseExpiresAt = nil }},
		{"wrong attempt", func(e *commonExecution.TaskExecution) { e.Attempt++ }},
		{"wrong token", func(e *commonExecution.TaskExecution) { token := uuid.NewString(); e.LeaseToken = &token }},
		{"wrong owner", func(e *commonExecution.TaskExecution) { owner := "another-worker"; e.LeaseOwner = &owner }},
		{"wrong execution", func(e *commonExecution.TaskExecution) { e.ExecutionID = uuid.NewString() }},
		{"wrong tenant", func(e *commonExecution.TaskExecution) { e.TenantID++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, sampler, store := queuedProfileForTest(t, DefaultDataProfileBudget)
			ctx := profileClaimContextForTest(t, store.createdExecution)
			tc.change(store.createdExecution)
			if err := svc.runClaimedExecution(ctx, store.createdExecution); !errors.Is(err, ErrDataProfileSourceAuthorizationRequired) {
				t.Fatalf("invalid claim: %v", err)
			}
			if sampler.resolveCalls != 0 || sampler.sampleCalls != 0 || svc.authorizationConsumer.(*profileConsumerForTest).calls != 0 || store.completed {
				t.Fatal("invalid claim reached resolution, consumption or sampling")
			}
		})
	}
	svc, sampler, store := queuedProfileForTest(t, DefaultDataProfileBudget)
	if err := svc.runClaimedExecution(t.Context(), store.createdExecution); !errors.Is(err, ErrDataProfileSourceAuthorizationRequired) || sampler.resolveCalls != 0 {
		t.Fatalf("missing context lease was accepted: %v", err)
	}
}

func TestDataProfileWorkerRequiresExactRepreparedPlan(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*DataProfileSamplePlan)
	}{
		{"changed offset", func(p *DataProfileSamplePlan) { p.pages.(*profilePreparedPagesForTest).positions[0].Offset++ }},
		{"changed limit", func(p *DataProfileSamplePlan) { p.pages.(*profilePreparedPagesForTest).positions[0].Limit-- }},
		{"missing page", func(p *DataProfileSamplePlan) {
			pages := p.pages.(*profilePreparedPagesForTest)
			pages.positions = pages.positions[1:]
		}},
		{"changed source", func(p *DataProfileSamplePlan) {
			p.pages.(*profilePreparedPagesForTest).set, _ = plugin.NewQueryReadSet(plugin.TabularItemPath(1, "schema", "public", "other"))
		}},
		{"expanded source", func(p *DataProfileSamplePlan) {
			p.pages.(*profilePreparedPagesForTest).set, _ = plugin.NewQueryReadSet(plugin.TabularItemPath(1, "schema", "public", "orders"), plugin.TabularItemPath(1, "schema", "public", "base"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, sampler, store := queuedProfileForTest(t, DefaultDataProfileBudget)
			sampler.changePlan = tc.change
			if err := svc.runClaimedExecution(profileClaimContextForTest(t, store.createdExecution), store.createdExecution); !errors.Is(err, ErrDataProfileSourceChanged) {
				t.Fatalf("changed plan accepted: %v", err)
			}
			if svc.authorizationConsumer.(*profileConsumerForTest).calls != 0 || sampler.sampleCalls != 0 || store.completed {
				t.Fatal("changed plan reached consumption or sampling")
			}
		})
	}
}

func TestDataProfileWorkerConsumptionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		consumer *profileConsumerForTest
		want     error
	}{
		{"missing client", nil, ErrDataProfileUnavailable},
		{"revoked", &profileConsumerForTest{err: &commonClient.SystemAPIError{StatusCode: 403}}, ErrDataProfileSourceAuthorizationRequired},
		{"lease changed", &profileConsumerForTest{err: &commonClient.SystemAPIError{StatusCode: 409}}, ErrDataProfileSourceAuthorizationRequired},
		{"expired credential", &profileConsumerForTest{err: &commonClient.SystemAPIError{StatusCode: 401}}, ErrDataProfileSourceAuthorizationRequired},
		{"invalid scope", &profileConsumerForTest{err: &commonClient.SystemAPIError{StatusCode: 400}}, ErrDataProfileSourceAuthorizationRequired},
		{"unavailable", &profileConsumerForTest{err: &commonClient.SystemAPIError{StatusCode: 503}}, ErrDataProfileUnavailable},
		{"transport error", &profileConsumerForTest{err: errors.New("sensitive upstream detail")}, ErrDataProfileUnavailable},
		{"empty observation", &profileConsumerForTest{empty: true}, ErrDataProfileUnavailable},
		{"zero observation", &profileConsumerForTest{zero: true}, ErrDataProfileUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, sampler, store := queuedProfileForTest(t, DefaultDataProfileBudget)
			if tc.consumer == nil {
				svc.SetExecutionAccessClient(nil)
			} else {
				svc.authorizationConsumer = tc.consumer
			}
			if err := svc.runClaimedExecution(profileClaimContextForTest(t, store.createdExecution), store.createdExecution); err != tc.want {
				t.Fatalf("unsafe error: %v", err)
			}
			if sampler.sampleCalls != 0 || store.completed || (tc.consumer != nil && tc.consumer.calls != 1) {
				t.Fatal("failed consumption read, persisted or retried")
			}
		})
	}
}

func TestDataProfileWorkerCancelledBeforeConsumptionDoesNotRead(t *testing.T) {
	svc, sampler, store := queuedProfileForTest(t, DefaultDataProfileBudget)
	ctx, cancel := context.WithCancel(profileClaimContextForTest(t, store.createdExecution))
	cancel()
	if err := svc.runClaimedExecution(ctx, store.createdExecution); !errors.Is(err, ErrDataProfileUnavailable) {
		t.Fatalf("cancelled attempt accepted: %v", err)
	}
	if svc.authorizationConsumer.(*profileConsumerForTest).calls != 0 || sampler.sampleCalls != 0 || store.completed {
		t.Fatal("cancelled attempt reached authorization, sampling or persistence")
	}
}

func TestDataProfileWorkerCancelledDuringConsumptionDoesNotRead(t *testing.T) {
	svc, sampler, store := queuedProfileForTest(t, DefaultDataProfileBudget)
	ctx, cancel := context.WithCancel(profileClaimContextForTest(t, store.createdExecution))
	defer cancel()
	svc.authorizationConsumer.(*profileConsumerForTest).onCheck = cancel
	if err := svc.runClaimedExecution(ctx, store.createdExecution); !errors.Is(err, ErrDataProfileUnavailable) {
		t.Fatalf("cancelled observation accepted: %v", err)
	}
	if sampler.sampleCalls != 0 || store.completed {
		t.Fatal("cancelled observation reached sampling or persistence")
	}
}

func TestDataProfileWorkerRejectsInvalidFrozenReadSetBeforeResolution(t *testing.T) {
	for _, value := range []any{nil, map[string]any{"paths": []any{}}, map[string]any{"paths": []any{map[string]any{"engine_id": 2}}}} {
		svc, sampler, store := queuedProfileForTest(t, DefaultDataProfileBudget)
		store.createdExecution.ExecutionConfig["read_set"] = value
		if err := svc.runClaimedExecution(profileClaimContextForTest(t, store.createdExecution), store.createdExecution); !errors.Is(err, ErrDataProfileSourceAuthorizationRequired) {
			t.Fatalf("invalid frozen source: %v", err)
		}
		if sampler.resolveCalls != 0 || sampler.sampleCalls != 0 || svc.authorizationConsumer.(*profileConsumerForTest).calls != 0 {
			t.Fatal("invalid frozen source reached domain work")
		}
	}
}

type profileServiceTokenForTest struct{ tenants []uint }

func (s *profileServiceTokenForTest) PlatformToken(context.Context) (string, error) {
	return "", errors.New("profile consumption must not request a platform token")
}

func (s *profileServiceTokenForTest) Token(_ context.Context, tenant uint) (string, error) {
	s.tenants = append(s.tenants, tenant)
	return "addp_at_current_manager_service", nil
}

func TestDataProfileWorkerUsesCurrentTenantServiceAndFullFrozenScope(t *testing.T) {
	svc, sampler, store := queuedProfileForTest(t, DefaultDataProfileBudget)
	item := store.createdExecution
	raw, err := store.GetRawExecutionConfig(t.Context(), 7, item.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	var frozen frozenDataProfileConfig
	if err := json.Unmarshal(raw, &frozen); err != nil {
		t.Fatal(err)
	}
	expected, err := commonExecution.NewManagerProfileReadScope(raw, &frozen.ReadSet)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/v1/system/execution-authorizations/41/manager-profile-accesses" || r.URL.RawQuery != "" ||
			r.Header.Get("Authorization") != "Bearer addp_at_current_manager_service" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("expanded consumption or saved User credential")
		}
		var req commonClient.ManagerProfileAccessRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.ExecutionID != item.ExecutionID || req.Attempt != item.Attempt || req.LeaseToken != *item.LeaseToken || !reflect.DeepEqual(req.SourceReadScope, *expected) {
			t.Error("missing exact claim or full configuration binding")
		}
		if calls > 1 {
			w.WriteHeader(403)
			return
		}
		json.NewEncoder(w).Encode(commonClient.ManagerProfileAccessObservation{ObservedAt: time.Now().UTC()})
	}))
	defer server.Close()
	tokens := &profileServiceTokenForTest{}
	svc.SetExecutionAccessClient(commonClient.NewSystemServiceClient(server.URL, tokens, server.Client()))
	ctx := profileClaimContextForTest(t, item)
	// First observation succeeds, then the first page's current check denies.
	// Fake sampling measures gate ordering, not real source access.
	if err := svc.runClaimedExecution(ctx, item); err != nil || sampler.sampleCalls != 1 {
		t.Fatalf("first observation: %v", err)
	}
	if err := svc.runClaimedExecution(ctx, item); !errors.Is(err, ErrDataProfileSourceAuthorizationRequired) || sampler.sampleCalls != 1 || calls != 3 || store.failedCode != "source_authorization_required" {
		t.Fatalf("previous observation cached: %v, calls=%d samples=%d", err, calls, sampler.sampleCalls)
	}
	if !reflect.DeepEqual(tokens.tenants, []uint{7, 7, 7}) {
		t.Fatalf("wrong service tenant: %v", tokens.tenants)
	}
}
