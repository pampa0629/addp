package iam

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Called by the existing real PostgreSQL EA gate, using its isolated schema.
func testExecutionSubjectRoles(t *testing.T, ctx context.Context, db *gorm.DB, issued *IssuedExecutionAuthorization, audit AuditMetadata, userToken string) {
	t.Helper()
	testCurrentSubjectRoles(t, ctx, db, userToken)
	t.Run("current_subject_roles", func(t *testing.T) {
		lease := uuid.New()
		base := ResolveExecutionSubjectRolesInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, Attempt: 1, LeaseToken: lease, ServicePrincipalID: *audit.PrincipalID, ServiceClientID: "addp-develop", TenantID: issued.TenantID, Audit: audit}
		withClaim := func(t *testing.T, body func(*gorm.DB, *ExecutionAuthorizationService, ResolveExecutionSubjectRolesInput)) {
			t.Helper()
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			err := tx.Exec(`INSERT INTO common.task_executions (execution_id,execution_authorization_id,tenant_id,module,status,attempt,lease_token,lease_expires_at,actor_principal_id,actor_tenant_membership_id,issued_authorization_version) VALUES (?,?,?,'develop','running',1,?,statement_timestamp()+interval '1 minute',?,?,?)`, issued.ExecutionID, issued.ID, issued.TenantID, lease, issued.ActorPrincipalID, issued.TenantMembershipID, issued.IssuedAuthorizationVersion).Error
			if err != nil {
				t.Fatal(err)
			}
			svc, _ := NewExecutionAuthorizationService(NewRepository(tx))
			body(tx, svc, base)
		}
		t.Run("current_union_without_permissions", func(t *testing.T) {
			withClaim(t, func(tx *gorm.DB, s *ExecutionAuthorizationService, in ResolveExecutionSubjectRolesInput) {
				if err := tx.Exec(`INSERT INTO system.roles (tenant_id,role_key,name,role_type,allowed_scope_types,allowed_principal_types,immutable,created_by_principal_id) VALUES (?,'tenant.role_option_test','Role option test','tenant_custom',ARRAY['tenant'],ARRAY['user'],false,?)`, issued.TenantID, issued.ActorPrincipalID).Error; err != nil {
					t.Fatal(err)
				}
				roleExpires := time.Now().Add(2 * time.Second)
				insertRoleAssignment(t, tx, issued.ActorPrincipalID, "tenant.role_option_test", "tenant", &issued.TenantID, nil, nil, time.Now().Add(-time.Minute), &roleExpires, "manual")
				// Adding a role advances the identity version: old EA must fail.
				if result, err := s.ResolveSubjectRoles(ctx, in); err == nil || result != nil {
					t.Fatal("role change reused old authorization")
				}
				refresh := func() {
					tokenService, err := NewTokenFamilyService(NewRepository(tx), BrowserSessionConfig{ResourceTicketOwners: []string{"manager"}}, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					selectionService, err := NewContextSelectionService(NewRepository(tx), tokenService)
					if err != nil {
						t.Fatal(err)
					}
					selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{PrincipalID: issued.ActorPrincipalID, Authentication: SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: time.Now()}, Audit: audit})
					if err != nil || selection.Session == nil {
						t.Fatalf("refresh fixture session: %v", err)
					}
					next, err := s.Issue(ctx, IssueExecutionAuthorizationInput{SourceAccessToken: selection.Session.AccessToken, Audience: "develop", ExecutionID: uuid.New(), Accesses: issued.Accesses, ExpiresIn: time.Minute, Audit: audit})
					if err != nil {
						t.Fatal(err)
					}
					if err := tx.Exec(`UPDATE common.task_executions SET execution_id=?,execution_authorization_id=?,issued_authorization_version=? WHERE execution_id=?`, next.ExecutionID, next.ID, next.IssuedAuthorizationVersion, in.ExecutionID).Error; err != nil {
						t.Fatal(err)
					}
					in.ExecutionID = next.ExecutionID
					in.AuthorizationID = next.ID
				}
				refresh()
				result, err := s.ResolveSubjectRoles(ctx, in)
				if err != nil || !reflect.DeepEqual(result.RoleKeys, []string{"tenant.data_engineer", "tenant.role_option_test"}) || result.Attempt != 1 || !result.ExpiresAt.After(result.ObservedAt) || result.ExpiresAt.After(time.Now().Add(time.Minute)) {
					t.Fatalf("role observation=%+v err=%v", result, err)
				}
				// Current assignments, not cached EA issuance roles. Natural validity is
				// checked even when no authorization-version change was needed.
				time.Sleep(time.Until(roleExpires) + 50*time.Millisecond)
				result, err = s.ResolveSubjectRoles(ctx, in)
				if err != nil || !reflect.DeepEqual(result.RoleKeys, []string{"tenant.data_engineer"}) {
					t.Fatalf("expired roles=%+v err=%v", result, err)
				}
			})
		})
		for _, tc := range []struct{ name, sql string }{
			{"finished", "UPDATE common.task_executions SET status='success'"},
			{"wrong_owner", "UPDATE common.task_executions SET module='transfer'"},
			{"old_attempt", "UPDATE common.task_executions SET attempt=2"},
			{"old_token", "UPDATE common.task_executions SET lease_token='a12e3dde-a523-4fe7-9062-dc1a128f1028'"},
			{"expired_lease", "UPDATE common.task_executions SET lease_expires_at=statement_timestamp()-interval '1 second'"},
			{"wrong_authorization", "UPDATE common.task_executions SET execution_authorization_id=NULL"},
			{"wrong_actor", "UPDATE common.task_executions SET actor_principal_id=NULL"},
			{"wrong_membership", "UPDATE common.task_executions SET actor_tenant_membership_id=NULL"},
			{"wrong_version", "UPDATE common.task_executions SET issued_authorization_version=issued_authorization_version+1"},
			{"missing_parent", "UPDATE common.task_executions SET parent_execution_id='a12e3dde-a523-4fe7-9062-dc1a128f1028'"},
			{"identity_version", "UPDATE system.principals SET authorization_version=authorization_version+1 WHERE principal_type='user'"},
			{"membership_expiry", "UPDATE system.tenant_memberships SET joined_at=statement_timestamp()-interval '1 minute', expires_at=statement_timestamp()-interval '1 second'"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				withClaim(t, func(tx *gorm.DB, s *ExecutionAuthorizationService, in ResolveExecutionSubjectRolesInput) {
					if err := tx.Exec(tc.sql).Error; err != nil {
						t.Fatal(err)
					}
					result, err := s.ResolveSubjectRoles(ctx, in)
					if !errors.Is(err, ErrExecutionAuthorizationUnavailable) || result != nil {
						t.Fatalf("result=%+v err=%v", result, err)
					}
				})
			})
		}
		for _, tc := range []struct {
			name   string
			change func(*ResolveExecutionSubjectRolesInput)
		}{
			{"runtime", func(in *ResolveExecutionSubjectRolesInput) { in.ServiceClientID = "addp-duckdb" }},
			{"other_owner", func(in *ResolveExecutionSubjectRolesInput) { in.ServiceClientID = "addp-transfer" }},
			{"tenant", func(in *ResolveExecutionSubjectRolesInput) { in.TenantID++; in.Audit.TenantID = &in.TenantID }},
			{"execution", func(in *ResolveExecutionSubjectRolesInput) { in.ExecutionID = uuid.New() }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				withClaim(t, func(_ *gorm.DB, s *ExecutionAuthorizationService, in ResolveExecutionSubjectRolesInput) {
					tc.change(&in)
					result, err := s.ResolveSubjectRoles(ctx, in)
					if err == nil || result != nil {
						t.Fatalf("unexpected roles=%+v", result)
					}
				})
			})
		}
		for _, audience := range []string{"transfer", "duckdb"} {
			t.Run(audience+"_owner", func(t *testing.T) {
				withClaim(t, func(tx *gorm.DB, s *ExecutionAuthorizationService, in ResolveExecutionSubjectRolesInput) {
					next, err := s.Issue(ctx, IssueExecutionAuthorizationInput{SourceAccessToken: userToken, Audience: audience, ExecutionID: uuid.New(), Accesses: issued.Accesses, ExpiresIn: time.Minute, Audit: audit})
					if err != nil {
						t.Fatal(err)
					}
					owner := "develop"
					if audience == "transfer" {
						owner = "transfer"
					}
					if err := tx.Exec(`UPDATE common.task_executions SET execution_id=?,execution_authorization_id=?,module=? WHERE execution_id=?`, next.ExecutionID, next.ID, owner, in.ExecutionID).Error; err != nil {
						t.Fatal(err)
					}
					in.ExecutionID = next.ExecutionID
					in.AuthorizationID = next.ID
					in.ServiceClientID = "addp-" + owner
					if err := tx.Raw(`SELECT id FROM system.service_principals WHERE name=?`, in.ServiceClientID).Scan(&in.ServicePrincipalID).Error; err != nil {
						t.Fatal(err)
					}
					in.Audit.PrincipalID = &in.ServicePrincipalID
					roles, err := s.ResolveSubjectRoles(ctx, in)
					if err != nil || roles.Owner != owner {
						t.Fatalf("owner result=%+v err=%v", roles, err)
					}
				})
			})
		}
		t.Run("parent_provenance", func(t *testing.T) {
			withClaim(t, func(tx *gorm.DB, s *ExecutionAuthorizationService, in ResolveExecutionSubjectRolesInput) {
				parent := uuid.New()
				if err := tx.Exec(`INSERT INTO common.task_executions (execution_id,tenant_id,module,status,actor_principal_id,actor_tenant_membership_id,issued_authorization_version) VALUES (?,?,'orchestrator','running',?,?,?)`, parent, issued.TenantID, issued.ActorPrincipalID, issued.TenantMembershipID, issued.IssuedAuthorizationVersion).Error; err != nil {
					t.Fatal(err)
				}
				if err := tx.Exec(`UPDATE common.task_executions SET parent_execution_id=? WHERE execution_id=?`, parent, issued.ExecutionID).Error; err != nil {
					t.Fatal(err)
				}
				if _, err := s.ResolveSubjectRoles(ctx, in); err != nil {
					t.Fatal(err)
				}
				if err := tx.Exec(`UPDATE common.task_executions SET status='failed' WHERE execution_id=?`, parent).Error; err != nil {
					t.Fatal(err)
				}
				if result, err := s.ResolveSubjectRoles(ctx, in); err == nil || result != nil {
					t.Fatal("finished parent accepted")
				}
			})
		})
	})
}
