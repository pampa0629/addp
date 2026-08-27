package iam

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCatalogReferenceCandidatesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset catalog reference candidate schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	identityService := NewIdentityService(repository, func() time.Time { return now })
	membershipService := NewTenantMembershipService(repository, func() time.Time { return now })
	organizationService := NewOrganizationService(repository, func() time.Time { return now })
	audit := AuditMetadata{RequestID: stringPointer("catalog-reference-candidates")}
	user := createContextSelectionUser(t, ctx, identityService, "catalog-candidate-user", audit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, "catalog-candidates", audit)
	contextType := ContextTypeTenant
	principalType := PrincipalTypeUser
	tenantAudit := AuditMetadata{
		PrincipalID: &user.PrincipalID, PrincipalType: &principalType,
		ContextType: &contextType, TenantID: &tenant.ID, RequestID: audit.RequestID,
	}
	establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, tenantAudit)
	department, err := organizationService.CreateDepartment(ctx, CreateDepartmentInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID,
		Code: "sales", Name: "Sales", Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("create candidate department: %v", err)
	}
	projectGroup, err := organizationService.CreateProjectGroup(ctx, CreateProjectGroupInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID,
		Code: "delivery", Name: "Delivery", Status: ProjectGroupStatusActive, Audit: tenantAudit,
	})
	if err != nil {
		t.Fatalf("create project group reference: %v", err)
	}

	departments, total, err := repository.ListCatalogDepartmentCandidates(ctx, tenant.ID, "sales", 1, 20)
	if err != nil || total != 1 || len(departments) != 1 || departments[0].ID != department.ID || departments[0].SubjectType != CatalogSubjectTypeDepartment {
		t.Fatalf("departments=%#v total=%d err=%v", departments, total, err)
	}
	users, total, err := repository.ListCatalogUserCandidates(ctx, tenant.ID, "catalog-candidate", 1, 20)
	if err != nil || total != 1 || len(users) != 1 || users[0].ID != user.PrincipalID || users[0].SubjectType != CatalogSubjectTypeUser {
		t.Fatalf("users=%#v total=%d err=%v", users, total, err)
	}
	projectGroups, err := repository.ResolveCatalogProjectGroups(ctx, tenant.ID, []int64{projectGroup.ID})
	if err != nil || len(projectGroups) != 1 || projectGroups[0].ID != projectGroup.ID || projectGroups[0].Name != "Delivery" || projectGroups[0].Status != string(ProjectGroupStatusActive) {
		t.Fatalf("projectGroups=%#v err=%v", projectGroups, err)
	}
}
