package main

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/testsupport"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// Called only after main has verified the disposable Hosted profile, current
// migrations, addp_online database identity and absence of any existing User.
func prepareAdministratorGovernanceIdentities(db *gorm.DB, cfg *config.Config, output string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repo := iam.NewRepository(db)
	identity := iam.NewIdentityService(repo, nil)
	cipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		return err
	}
	bootstrap, err := iam.NewBootstrapService(repo, identity, cipher, time.Hour, nil, nil)
	if err != nil {
		return err
	}
	proof, _, err := bootstrap.Prepare(ctx)
	if err != nil {
		return err
	}
	inputs, err := platformBootstrapInputs(time.Now(), "online-governance", "Online governance")
	if err != nil {
		return err
	}
	result, err := bootstrap.Apply(ctx, iam.BootstrapApplyInput{BootstrapSecret: proof, Administrators: inputs})
	if err != nil {
		return err
	}
	values := map[string]string{}
	for i, name := range []string{"SYSTEM", "SECURITY", "AUDIT"} {
		prefix := "ADDP_ONLINE_GOV_" + name
		values[prefix+"_ID"] = fmt.Sprint(result.PrincipalIDs[inputs[i].RoleKey])
		values[prefix+"_USERNAME"], values[prefix+"_PASSWORD"], values[prefix+"_TOTP"] = inputs[i].Username, inputs[i].Password, inputs[i].TOTPSecret
	}
	clock, err := testsupport.FixtureDatabaseClock(ctx, db)
	if err != nil {
		return err
	}
	tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{ResourceTicketOwners: models.BrowserResourceAccessOwners}, nil, nil)
	if err != nil {
		return err
	}
	selection, err := iam.NewContextSelectionService(repo, tokens)
	if err != nil {
		return err
	}
	tenants := iam.NewPlatformTenantService(repo, clock)
	memberships := iam.NewTenantMembershipService(repo, clock)
	roles := iam.NewTenantRoleService(repo, clock)
	var tenantID, ownerID int64
	for _, code := range []string{"reserve", "test"} {
		owner, err := createUser(ctx, identity, "online-governance-owner-"+code)
		if err != nil {
			return err
		}
		tenant, err := tenants.Create(ctx, iam.CreateTenantInput{Code: "online-governance-" + code, Name: "Online governance " + code, InitialAdministratorPrincipalID: owner.PrincipalID, ActorPrincipalID: owner.PrincipalID, Audit: audit("governance-tenant")})
		if err != nil {
			return err
		}
		tenantID, ownerID = tenant.ID, owner.PrincipalID
	}
	if tenantID <= 1 {
		return fmt.Errorf("governance requires nondefault Tenant")
	}
	values["ADDP_ONLINE_TEST_TENANT_ID"] = fmt.Sprint(tenantID)
	negative, _, _, err := createPermissionFixture(ctx, repo, identity, memberships, roles, selection, tenantID, ownerID, "online-governance-reader", []string{"monitor.execution.read"})
	if err != nil {
		return err
	}
	values["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"] = negative.AccessToken
	candidate, assignmentID, password, err := createPermissionFixture(ctx, repo, identity, memberships, roles, selection, tenantID, ownerID, "online-governance-successor", []string{"monitor.execution.read"})
	if err != nil {
		return err
	}
	assignment, err := roles.GetAssignment(ctx, tenantID, assignmentID)
	if err != nil {
		return err
	}
	enrollmentService, err := iam.NewMFASessionService(repo, cipher, tokens, iam.MFAServiceConfig{}, nil, nil)
	if err != nil {
		return err
	}
	enrollment, err := enrollmentService.BeginEnrollment(ctx, iam.BeginMFAEnrollmentInput{AccessToken: candidate.AccessToken, RefreshToken: candidate.RefreshToken, CurrentPassword: password, Audit: audit("governance-successor-enrollment")})
	if err != nil {
		return err
	}
	code, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		return err
	}
	if _, err = enrollmentService.CompleteEnrollment(ctx, iam.CompleteMFAEnrollmentInput{AccessToken: candidate.AccessToken, RefreshToken: candidate.RefreshToken, EnrollmentToken: enrollment.EnrollmentToken, Code: code, Audit: audit("governance-successor-enrolled")}); err != nil {
		return err
	}
	values["ADDP_ONLINE_GOV_SUCCESSOR_ID"] = fmt.Sprint(assignment.PrincipalID)
	values["ADDP_ONLINE_GOV_SUCCESSOR_USERNAME"] = "online-governance-successor"
	values["ADDP_ONLINE_GOV_SUCCESSOR_PASSWORD"], values["ADDP_ONLINE_GOV_SUCCESSOR_TOTP"] = password, enrollment.Secret
	return writeEnvironmentFile(output, values)
}
