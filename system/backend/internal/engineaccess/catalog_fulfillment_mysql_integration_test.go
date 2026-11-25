package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
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
		source := userProvenance{PrincipalID: confirmer.PrincipalID, MembershipID: confirmer.MembershipID, AuthorizationVersion: confirmer.AuthorizationVersion}
		ranges, err := repo.metadataRefreshRanges(ctx, 7, source, shared.MetadataRefreshQuery{Kind: "item", Target: base.Path})
		if err != nil || (len(ranges) == 1) != (want == "grant") || len(ranges) > 1 {
			t.Fatalf("item refresh expected %s, ranges=%+v error=%v", want, ranges, err)
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
	t.Run("current grant management list", func(t *testing.T) {
		path := base.Path
		filter := SourceGrantFilter{CatalogPath: &path, RecipientType: binding.RecipientType, RecipientID: binding.RecipientID}
		observed, count, err := repo.listAccountGrantRelations(ctx, 7, 12, binding.RecipientID, 1, 100, filter)
		if err != nil || count != 1 || len(observed) != 1 {
			t.Fatalf("account target count %d: %v", count, err)
		}
		item := observed[0]
		if item.RequestID != nil || item.GrantCount != 1 || item.Inspection == nil || item.Inspection.Reason != "grant" || !item.Inspection.RuleCovered ||
			len(item.Inspection.Sources) != 1 || item.Inspection.Sources[0].RequestID != secondID || item.Inspection.Sources[0].RecipientID != binding.RecipientID {
			t.Fatalf("account source/anchor mismatch: %+v", item.Inspection)
		}
		empty, count, err := repo.listAccountGrantRelations(ctx, 7, 12, binding.RecipientID, 2, 1, filter)
		if err != nil || count != 1 || len(empty) != 0 {
			t.Fatalf("empty account page: %d, %v", count, err)
		}
		if _, _, err := repo.listAccountGrantRelations(ctx, 7, 12, 999999, 1, 1, filter); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatalf("unknown account: %v", err)
		}
		for _, f := range []SourceGrantFilter{filter, {TableSearch: strings.TrimSpace(path.Segments[len(path.Segments)-1].Name), RecipientType: binding.RecipientType, RecipientID: binding.RecipientID}} {
			rows, total, err := repo.directGrantRelations(ctx, 7, 12, 1, 100, f)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.RequestID != nil && *row.RequestID == secondID {
					found = row.GrantCount == 1 && row.GrantedAt != nil && row.Action == binding.Action
				}
				if row.RequestID != nil && *row.RequestID == id {
					t.Fatal("revoked anchor returned")
				}
			}
			if !found || total < int64(len(rows)) {
				t.Fatalf("current relation missing: count=%d rows=%d", total, len(rows))
			}
			empty, otherTotal, err := repo.directGrantRelations(ctx, 7, 12, int(total)+1, 1, f)
			if err != nil || len(empty) != 0 || otherTotal != total {
				t.Fatalf("empty page total %d, want %d: %v", otherTotal, total, err)
			}
		}
	})
	checkVisibility("grant")
	for _, sample := range []struct {
		name           string
		child, sibling engineplugin.EngineCatalogPath
	}{
		{"metadata-only schema grant and natural expiry", engineplugin.TabularItemPath(base.Path.EngineID, "schema", "visibility", "orders"), engineplugin.TabularItemPath(base.Path.EngineID, "schema", "sibling", "orders")},
		{"MongoDB collection", engineplugin.EngineCatalogBranchLeafPath(engineplugin.DynamicSchemaCatalogModel(), 85, "database", "visibility", "collection", "collection", "orders"), engineplugin.EngineCatalogBranchLeafPath(engineplugin.DynamicSchemaCatalogModel(), 85, "database", "sibling", "collection", "collection", "orders")},
		{"MySQL database", engineplugin.TabularItemPath(84, "database", "visibility", "orders"), engineplugin.TabularItemPath(84, "database", "sibling", "orders")},
		{"GaussDB schema", engineplugin.TabularItemPath(81, "schema", "visibility", "orders"), engineplugin.TabularItemPath(81, "schema", "sibling", "orders")},
		{"FTP directory", engineplugin.FileItemPath(82, "visibility/orders.gpkg"), engineplugin.FileItemPath(82, "sibling/orders.gpkg")},
		{"RustFS prefix", engineplugin.ObjectItemPath(83, "reports", "visibility/orders.csv"), engineplugin.ObjectItemPath(83, "reports", "sibling/orders.csv")},
	} {
		t.Run(sample.name, func(t *testing.T) {
			child, sibling := sample.child, sample.sibling
			node := child
			node.Segments = child.Segments[:len(child.Segments)-1]
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
			paths := []engineplugin.EngineCatalogPath{child, child, sibling}
			bindings := make([]authorizationfacts.Binding, 0, 2)
			for _, path := range []engineplugin.EngineCatalogPath{child, sibling} {
				var facts authorizationfacts.AuthorizationFacts
				switch path.Segments[len(path.Segments)-1].Term {
				case "file":
					facts = *developGeoPackageFacts(t, path)
				case "object":
					facts = exactNativeObjectFacts(t, path)
				default:
					facts = testBindingFacts(path)
				}
				binding, err := facts.MetadataBinding()
				if err != nil {
					t.Fatal(err)
				}
				bindings = append(bindings, binding)
			}
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
			checkListed := func(active bool) {
				t.Helper()
				filter := SourceGrantFilter{CatalogPath: &child, RecipientType: "user", RecipientID: confirmer.PrincipalID}
				observed, total, err := repo.listAccountGrantRelations(ctx, 7, int64(node.EngineID), confirmer.PrincipalID, 1, 10, filter)
				if err != nil {
					t.Fatal(err)
				}
				if active {
					if total != 1 || len(observed) != 1 || observed[0].Inspection == nil || !observed[0].Inspection.RuleCovered ||
						len(observed[0].Inspection.Sources) != 1 || observed[0].Inspection.Sources[0].RequestID != grant.RequestID ||
						observed[0].ExpiresAt == nil || !observed[0].ExpiresAt.Equal(expiry) || observed[0].NodeRecursive == nil || *observed[0].NodeRecursive {
						t.Fatalf("active node grant missing or scope changed: %+v", observed)
					}
				} else if total != 0 || len(observed) != 0 {
					t.Fatal("expired account relation still listed")
				}
				history, n, err := repo.listSourceGrants(ctx, 7, int64(node.EngineID), 1, 10, filter)
				if err != nil || n != 1 || len(history) != 1 || history[0].RequestID != grant.RequestID {
					t.Fatalf("node grant history lost: %d, %v", n, err)
				}
			}
			read(true)
			checkListed(true)
			if err := repo.db.Exec("SELECT SLEEP(GREATEST(0,TIMESTAMPDIFF(MICROSECOND,CURRENT_TIMESTAMP(6),?))/1000000+0.02)", expiry).Error; err != nil {
				t.Fatal(err)
			}
			read(false)
			checkListed(false)
			t.Log("metadata visible under active grant; content and sibling denied; no visibility after natural expiry")
		})
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
