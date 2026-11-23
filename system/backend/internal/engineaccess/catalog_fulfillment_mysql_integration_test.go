package engineaccess

import (
	"context"
	"encoding/json"
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
		Operator: provenance(actorFor(50003)), Path: base.Path, DecisionID: base.DecisionID, RecipientType: "user", RecipientID: 40002,
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
	first, err := svc.IssueFulfillmentGrant(ctx, caller, id, binding)
	if err != nil || first.RequestID != id || first.GrantedAt.IsZero() {
		t.Fatalf("first issue: %+v, %v", first, err)
	}
	relation := sourceGrant{TenantID: 7, EngineID: int64(binding.Path.EngineID), ItemFingerprint: binding.ItemFingerprint, RecipientType: binding.RecipientType, RecipientID: binding.RecipientID, Action: binding.Action}
	relation.CatalogPath, err = encodeFulfillmentPath(binding.Path)
	if err != nil {
		t.Fatal(err)
	}
	checkActive := func(want uuid.UUID) {
		t.Helper()
		rows, err := repo.activeRelationGrants(ctx, relation)
		if err != nil {
			t.Fatal(err)
		}
		if want == uuid.Nil && len(rows) == 0 {
			return
		}
		if len(rows) != 1 || rows[0].RequestID != want {
			t.Fatalf("expected active request %s, got %+v", want, rows)
		}
	}
	checkVisibility := func(want string) {
		t.Helper()
		result, err := repo.readCurrentSourceRules(ctx, sourceReadRequest{
			Action: shared.SourceAccessMetadataRead, TenantID: 7,
			Source:  userProvenance{PrincipalID: confirmer.PrincipalID, MembershipID: confirmer.MembershipID, AuthorizationVersion: confirmer.AuthorizationVersion},
			Targets: []engineplugin.EngineCatalogPath{base.Path}, Bindings: testReadBindings(base.Path),
		})
		if err != nil || len(result.Targets) != 1 || result.Targets[0].Reason != want || result.Covered != (want == "grant") {
			t.Fatalf("metadata visibility: expected %s, got %+v, %v", want, result, err)
		}
		if result.ExpiresAt != nil {
			t.Fatalf("unexpected deadline: %v", result.ExpiresAt)
		}
	}
	checkActive(id)
	checkVisibility("grant")
	secondID := uuid.New()
	secondBinding := binding
	secondBinding.DecisionID = uuid.New()
	if _, err := svc.AcceptFulfillment(ctx, caller, secondID, secondBinding); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IssueFulfillmentGrant(ctx, caller, secondID, secondBinding); !errors.Is(err, ErrGrantRelationExists) {
		t.Fatalf("duplicate active relation: %v", err)
	}
	withdrawal := RevokeGrantInput{Actor: actorFor(50003), EngineID: 12, RequestID: id, Reason: "withdraw Catalog access"}
	denied := withdrawal
	denied.Actor = actorFor(50002)
	if _, err := svc.RevokeGrant(ctx, denied); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("unauthorized revocation: %v", err)
	}
	checkActive(id)
	revoked, err := svc.RevokeGrant(ctx, withdrawal)
	if err != nil || revoked.RevokedAt.Before(first.GrantedAt) || len(revoked.RevokedRequestIDs) != 1 || revoked.RevokedRequestIDs[0] != id {
		t.Fatalf("withdraw: %+v, %v", revoked, err)
	}
	again, err := svc.RevokeGrant(ctx, withdrawal)
	if err != nil || !again.RevokedAt.Equal(revoked.RevokedAt) {
		t.Fatalf("repeat withdrawal: %+v, %v", again, err)
	}
	checkActive(uuid.Nil)
	checkVisibility("no_grant")
	second, err := svc.IssueFulfillmentGrant(ctx, caller, secondID, secondBinding)
	if err != nil || second.RequestID != secondID {
		t.Fatalf("reissue: %+v, %v", second, err)
	}
	if _, err := svc.IssueFulfillmentGrant(ctx, caller, id, binding); err != nil {
		t.Fatalf("historical retry: %v", err)
	}
	checkActive(secondID)
	checkVisibility("grant")
	t.Run("metadata-only schema grant and natural expiry", func(t *testing.T) {
		child := engineplugin.TabularItemPath(base.Path.EngineID, "schema", "visibility", "orders")
		node := child
		node.Segments = child.Segments[:2]
		raw, err := json.Marshal(node)
		if err != nil {
			t.Fatal(err)
		}
		operator := actorFor(50001)
		now, err := repo.wallClock(ctx)
		if err != nil {
			t.Fatal(err)
		}
		expiry := now.Add(8 * time.Second)
		recursive, reason := false, "metadata-only schema"
		grant := sourceGrant{RequestID: uuid.New(), ApprovalMode: approvalModeIndependent, TenantID: 7, EngineID: int64(node.EngineID),
			CatalogPath: raw, NodeRecursive: &recursive, RecipientType: "user", RecipientID: confirmer.PrincipalID,
			Action: shared.SourceAccessMetadataRead, ExpiryMode: shared.SharingExpiryAtTime, ExpiresAt: &expiry, Reason: &reason,
			OperatorPrincipalID: operator.PrincipalID, OperatorMembershipID: operator.MembershipID, OperatorAuthorizationVersion: operator.AuthorizationVersion}
		if err := repo.transaction(ctx, func(tx *Repository) error { return tx.insertSourceGrant(ctx, &grant) }); err != nil {
			t.Fatal(err)
		}
		sibling := engineplugin.TabularItemPath(node.EngineID, "schema", "sibling", "orders")
		paths := []engineplugin.EngineCatalogPath{child, child, sibling}
		bindings := testReadBindings(child, sibling)
		targets := []sourceRuleTarget{{Binding: bindings[0], Action: shared.SourceAccessMetadataRead}, {Binding: bindings[0], Action: shared.SourceAccessRead}, {Binding: bindings[1], Action: shared.SourceAccessMetadataRead}}
		read := func(want bool) {
			t.Helper()
			result, err := repo.queryCurrentSourceRules(ctx, sourceReadRequest{Action: shared.SourceAccessMetadataRead, TenantID: 7, Source: userProvenance{PrincipalID: confirmer.PrincipalID, MembershipID: confirmer.MembershipID, AuthorizationVersion: confirmer.AuthorizationVersion}}, paths, targets)
			if err != nil || len(result.Targets) != 3 || result.Targets[0].Covered != want || result.Targets[1].Covered || result.Targets[2].Covered || result.Covered {
				t.Fatalf("metadata/content/sibling visibility: %+v, %v", result, err)
			}
			deadline := result.Targets[0].ExpiresAt
			if (want && (deadline == nil || !deadline.Equal(expiry))) || (!want && deadline != nil) {
				t.Fatalf("unexpected deadline: %v", deadline)
			}
		}
		read(true)
		if err := repo.db.Exec("SELECT SLEEP(GREATEST(0,TIMESTAMPDIFF(MICROSECOND,CURRENT_TIMESTAMP(6),?))/1000000+0.02)", expiry).Error; err != nil {
			t.Fatal(err)
		}
		read(false)
		t.Log("metadata visible under active grant; content and sibling denied; no visibility after natural expiry")
	})
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
