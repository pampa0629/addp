package engineaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
)

func TestCatalogFulfillmentServiceAgainstMySQL(t *testing.T) {
	repo := mysqlSourceGrantRepository(t)
	ctx := t.Context()
	actorFor := func(memberID int64) Actor {
		t.Helper()
		actor := Actor{TenantID: 7, MembershipID: memberID, TokenExpiresAt: time.Now().Add(time.Minute)}
		err := repo.db.Raw(`SELECT p.id,p.authorization_version FROM system.tenant_memberships m
            JOIN system.principals p ON p.id=m.principal_id WHERE m.tenant_id=? AND m.id=?`, actor.TenantID, memberID).
			Row().Scan(&actor.PrincipalID, &actor.AuthorizationVersion)
		if err != nil {
			t.Fatal(err)
		}
		return actor
	}
	base := testFulfillmentRequest()
	svc := NewService(repo, func(e *models.Engine) bool { return e.EngineType == "postgresql" }).
		WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{base.Path}})
	manager := actorFor(50001)
	delegation, err := svc.Create(ctx, CreateInput{Actor: manager, EngineID: 12, TenantMembershipID: 50003,
		ExpiresAt: time.Now().Add(time.Hour), Reason: "Catalog fulfillment"})
	if err != nil {
		t.Fatal(err)
	}
	provenance := func(actor Actor) shared.SharingFulfillmentOperator {
		return shared.SharingFulfillmentOperator{PrincipalID: actor.PrincipalID, MembershipID: actor.MembershipID, AuthorizationVersion: actor.AuthorizationVersion}
	}
	caller := FulfillmentRuntimeActor{Actor: actorFor(50004), ClientID: "addp-catalog"}
	binding := shared.SharingFulfillmentBinding{ItemFingerprint: base.ItemFingerprint, BoundaryHash: base.BoundaryHash, CallerPrincipalID: caller.PrincipalID,
		Operator: provenance(actorFor(50003)), Path: base.Path, DecisionID: base.DecisionID, RecipientType: "user", RecipientID: manager.PrincipalID,
		Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked}
	confirmer := provenance(actorFor(50002))
	calls := 0
	svc.WithFulfillmentBasisReader(fulfillmentBasisFunc(func(_ context.Context, tenant uint, id uuid.UUID, actual shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentBasis, error) {
		calls++
		return &shared.SharingFulfillmentBasis{RequestID: id, TenantID: int64(tenant), Binding: actual, Confirmation: confirmer}, nil
	}))
	id := uuid.New()
	accepted, err := svc.AcceptFulfillment(ctx, caller, id, binding)
	if err != nil || accepted == nil || accepted.Outcome != "accepted" || accepted.Deadline == nil || accepted.Deadline.Sub(accepted.RecordedAt) != 5*time.Minute {
		t.Fatalf("first acceptance: %+v, %v", accepted, err)
	}
	recovered, err := svc.ResolveFulfillment(ctx, caller, id, binding)
	if err != nil || !recovered.Found || recovered.Resolution.RequestID != id {
		t.Fatalf("receipt recovery: %+v, %v", recovered, err)
	}
	if _, err := svc.AcceptFulfillment(ctx, caller, id, binding); err != nil || calls != 1 {
		t.Fatalf("historical acceptance: basis calls=%d, %v", calls, err)
	}
	if _, err := svc.Revoke(ctx, RevokeInput{Actor: manager, EngineID: 12, ID: delegation.ID, Version: 1, Reason: "Catalog handling complete"}); err != nil {
		t.Fatal(err)
	}
	binding.Operator = provenance(actorFor(50003))
	next := uuid.New()
	if _, err := svc.AcceptFulfillment(ctx, caller, next, binding); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("revoked delegated operator: %v", err)
	}
	missing, err := svc.ResolveFulfillment(ctx, caller, next, binding)
	if err != nil || missing.Found {
		t.Fatalf("refused acceptance left receipt: %+v, %v", missing, err)
	}
}
