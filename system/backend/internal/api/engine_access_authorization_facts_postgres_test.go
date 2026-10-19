package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type authorizationFactsReaderFunc func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error)

func (f authorizationFactsReaderFunc) ReadAuthorizationFacts(ctx context.Context, tenant uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
	return f(ctx, tenant, fingerprint)
}

func exerciseAuthorizationFactsAPI(t *testing.T, db *gorm.DB, identity *iam.Repository, actor engineaccess.Actor, engine models.Engine) {
	t.Run("qualified authorization facts observation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		roles := iam.NewTenantRoleService(identity, time.Now)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: actor.TenantID, RoleKey: "custom.authorization_fact_reader", Name: "Authorization facts reader", PermissionKeys: []string{"system.engine_access_grant.read", "system.engine_access_delegation.create"}, ActorPrincipalID: actor.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		stepUp := time.Now().Add(time.Minute)
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: actor.TenantID, MembershipID: actor.MembershipID, RoleIDs: []int64{role.ID}, Reason: "Read exact resource facts", ActorPrincipalID: actor.PrincipalID, AssuranceLevel: iam.AssuranceLevelAAL2, StepUpExpiresAt: &stepUp}); err != nil {
			t.Fatal(err)
		}
		refresh := func() {
			p, err := identity.GetPrincipal(ctx, actor.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			actor.AuthorizationVersion = p.AuthorizationVersion
		}
		refresh()
		actor.TokenExpiresAt = time.Now().Add(time.Minute)
		fingerprint := commonmodels.GenerateItemFingerprint(engine.ID, "outdoor.persons")
		facts := &authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, ItemFingerprint: fingerprint, EngineID: engine.ID, ItemType: "table", FullName: "outdoor.persons", BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: plugin.TabularItemPath(engine.ID, "schema", "outdoor", "persons")}}
		hash, err := facts.ComputeBoundaryHash()
		if err != nil {
			t.Fatal(err)
		}
		facts.BoundaryHash = &hash
		calls := 0
		reader := authorizationFactsReaderFunc(func(_ context.Context, tenant uint, requested string) (*authorizationfacts.AuthorizationFacts, error) {
			calls++
			if int64(tenant) != actor.TenantID || requested != fingerprint {
				t.Fatal("wrong resource context")
			}
			return facts, nil
		})
		s := engineaccess.NewService(engineaccess.NewRepository(db), nil).WithAuthorizationFactsReader(reader)
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), fingerprint); err != nil {
			t.Fatal(err)
		}
		other := *facts
		other.EngineID++
		other.ItemFingerprint = commonmodels.GenerateItemFingerprint(other.EngineID, other.FullName)
		otherBoundary := *facts.Boundary
		otherBoundary.NativeTarget = plugin.TabularItemPath(other.EngineID, "schema", "outdoor", "persons")
		other.Boundary = &otherBoundary
		otherHash, err := other.ComputeBoundaryHash()
		if err != nil {
			t.Fatal(err)
		}
		other.BoundaryHash = &otherHash
		s.WithAuthorizationFactsReader(authorizationFactsReaderFunc(func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error) {
			return &other, nil
		}))
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), other.ItemFingerprint); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatal("another engine target was exposed under selected engine", err)
		}
		s.WithAuthorizationFactsReader(reader)
		bad := actor
		bad.AuthorizationVersion--
		before := calls
		if _, err := s.GetAuthorizationFacts(ctx, bad, int64(engine.ID), fingerprint); !errors.Is(err, commonapi.ErrForbidden) || calls != before {
			t.Fatal("stale actor called Meta", err)
		}
		s.WithAuthorizationFactsReader(authorizationFactsReaderFunc(func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error) {
			return nil, &client.TenantAPIError{StatusCode: http.StatusNotFound}
		}))
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), fingerprint); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatal("missing target confused with dependency failure", err)
		}
		s.WithAuthorizationFactsReader(authorizationFactsReaderFunc(func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error) {
			// Must complete while the remote read is in flight: no local IAM lock may remain held.
			if err := db.WithContext(ctx).Exec("UPDATE system.principals SET authorization_version=authorization_version+1 WHERE id=?", actor.PrincipalID).Error; err != nil {
				return nil, err
			}
			return facts, nil
		}))
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), fingerprint); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("remote read outlived changed human authority", err)
		}
		refresh()
		s.WithAuthorizationFactsReader(nil)
		if _, err := s.GetAuthorizationFacts(ctx, actor, int64(engine.ID), fingerprint); !errors.Is(err, engineaccess.ErrAuthorizationFactsUnavailable) {
			t.Fatal("missing service credential silently bypassed", err)
		}
	})
}
