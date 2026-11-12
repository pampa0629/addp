package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/production"
	"github.com/google/uuid"
)

type fileProductionRuntime struct {
	t     *testing.T
	calls int
}

func (f *fileProductionRuntime) ResolveInitialProductionGrant(_ context.Context, ref production.Reference) (*authorization.InitialProductionLookup, error) {
	f.calls++
	now := time.Now().UTC()
	return &authorization.InitialProductionLookup{Found: true, Result: &authorization.InitialProductionResult{SchemaVersion: authorization.InitialProductionResultSchema, TenantID: 3, ProducerOwner: "develop", ExecutionID: uuid.MustParse(ref.ExecutionID), ResultID: uuid.MustParse(ref.ResultID), Outcome: "issued", RecordedAt: now, Grant: &authorization.InitialProductionGrant{RequestID: uuid.New(), GrantedAt: now, ExpiryMode: authorization.SharingExpiryUntilRevoked}}}, nil
}
func (f *fileProductionRuntime) IssueInitialProductionGrant(context.Context, production.Reference) (*authorization.InitialProductionResult, error) {
	f.t.Fatal("resolved history was issued again")
	return nil, nil
}
func (f *fileProductionRuntime) EnsureProductionBasicScan(context.Context, uint, production.Reference) (*client.MetaProductionIdentification, error) {
	f.t.Fatal("resolved history was scanned again")
	return nil, nil
}
