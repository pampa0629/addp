package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/production"
	"github.com/google/uuid"
)

type productionDeliveryFixture struct {
	calls               []string
	lookup              *authorization.InitialProductionLookup
	result              *authorization.InitialProductionResult
	lookupErr, issueErr error
}

func (f *productionDeliveryFixture) ResolveInitialProductionGrant(context.Context, production.Reference) (*authorization.InitialProductionLookup, error) {
	f.calls = append(f.calls, "resolve")
	return f.lookup, f.lookupErr
}
func (f *productionDeliveryFixture) IssueInitialProductionGrant(context.Context, production.Reference) (*authorization.InitialProductionResult, error) {
	f.calls = append(f.calls, "issue")
	return f.result, f.issueErr
}
func (f *productionDeliveryFixture) EnsureProductionBasicScan(_ context.Context, t uint, r production.Reference) (*MetaProductionIdentification, error) {
	f.calls = append(f.calls, "scan")
	return &MetaProductionIdentification{Reference: r, TenantID: int64(t), ScanExecutionID: uuid.NewString(), Status: "pending", EngineVersion: 1}, nil
}

func TestProductionDeliveryResolvesOriginalOutcomeBeforeAnyPreparation(t *testing.T) {
	ref := production.Reference{Owner: "develop", ExecutionID: uuid.NewString(), ResultID: uuid.NewString()}
	terminal := &authorization.InitialProductionResult{SchemaVersion: authorization.InitialProductionResultSchema, TenantID: 7, ProducerOwner: ref.Owner, ExecutionID: uuid.MustParse(ref.ExecutionID), ResultID: uuid.MustParse(ref.ResultID), Outcome: "denied", DenialCode: "source_access_denied", RecordedAt: time.Now().UTC()}
	for _, tc := range []struct {
		name                string
		found               bool
		lookupErr, issueErr error
		wantCalls           string
		terminal            bool
	}{
		{"history", true, nil, nil, "resolve", true},
		{"new terminal", false, nil, nil, "resolve issue", true},
		{"lost lookup", false, errors.New("lost"), nil, "resolve", false},
		{"lost issue", false, nil, errors.New("lost"), "resolve issue", false},
		{"forbidden", false, nil, &TenantAPIError{StatusCode: 403}, "resolve issue", false},
		{"only explicit pending scans", false, nil, &TenantAPIError{StatusCode: 503, ErrorCode: "initial_production_pending"}, "resolve issue scan", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &productionDeliveryFixture{lookup: &authorization.InitialProductionLookup{Found: tc.found}, lookupErr: tc.lookupErr, result: terminal, issueErr: tc.issueErr}
			if tc.found {
				f.lookup.Result = terminal
			}
			result, _, err := ContinueInitialProduction(t.Context(), 7, ref, f, f)
			got := ""
			for _, call := range f.calls {
				if got != "" {
					got += " "
				}
				got += call
			}
			if got != tc.wantCalls || (err == nil) != tc.terminal || (result != nil) != tc.terminal {
				t.Fatal(got, result, err)
			}
		})
	}
	f := &productionDeliveryFixture{lookup: &authorization.InitialProductionLookup{Found: true, Result: terminal}}
	if result, _, err := ContinueInitialProduction(t.Context(), 7, ref, f, nil); err != nil || result == nil {
		t.Fatal("history recovery required Meta configuration", result, err)
	}
	f.calls = nil
	foreign := ref
	foreign.ResultID = uuid.NewString()
	if _, _, err := ContinueInitialProduction(t.Context(), 7, foreign, f, f); err == nil || len(f.calls) != 1 {
		t.Fatal("foreign result history accepted")
	}
}
