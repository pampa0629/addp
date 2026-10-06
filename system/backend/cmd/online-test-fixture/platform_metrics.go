package main

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// This owner-only helper prepares identities; actual Platform login and MFA are
// exercised through the public API by the suite, never by claiming AAL2 here.
func platformMetricsBootstrapInputs(now time.Time) ([]iam.BootstrapAdministratorInput, error) {
	roles := []string{"system", "security", "audit"}
	inputs := make([]iam.BootstrapAdministratorInput, 0, len(roles))
	for _, role := range roles {
		var entropy [48]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return nil, err
		}
		secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(entropy[:20])
		input := iam.BootstrapAdministratorInput{
			RoleKey: "platform." + role + "_administrator", Username: "online-metrics-" + role,
			Password: base64.RawURLEncoding.EncodeToString(entropy[20:]), DisplayName: "Online metrics " + role,
			TOTPSecret: secret,
		}
		// Previously verified consecutive codes leave the current code unused,
		// so public login can prove MFA immediately without replay or a fake clock.
		for _, at := range []time.Time{now.UTC().Truncate(30 * time.Second).Add(-60 * time.Second), now.UTC().Truncate(30 * time.Second).Add(-30 * time.Second)} {
			code, err := totp.GenerateCode(secret, at)
			if err != nil {
				return nil, err
			}
			input.TOTPProofs = append(input.TOTPProofs, iam.BootstrapTOTPProof{Code: code, VerifiedAt: at})
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func preparePlatformMetricsIdentities(db *gorm.DB, cfg *config.Config, output string) error {
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
	secret, _, err := bootstrap.Prepare(ctx)
	if err != nil {
		return err
	}
	inputs, err := platformMetricsBootstrapInputs(time.Now())
	if err != nil {
		return err
	}
	if _, err := bootstrap.Apply(ctx, iam.BootstrapApplyInput{BootstrapSecret: secret, Administrators: inputs}); err != nil {
		return fmt.Errorf("bootstrap disposable metrics identities: %w", err)
	}
	values := map[string]string{}
	for i, prefix := range []string{"ADDP_ONLINE_METRICS_ADMIN", "ADDP_ONLINE_METRICS_SECURITY"} {
		values[prefix+"_USERNAME"], values[prefix+"_PASSWORD"], values[prefix+"_TOTP"] = inputs[i].Username, inputs[i].Password, inputs[i].TOTPSecret
	}
	tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{AccessTokenTTL: 30 * time.Minute, RefreshTokenFamilyTTL: 45 * time.Minute, ResourceTicketOwners: models.BrowserResourceAccessOwners}, nil, nil)
	if err != nil {
		return err
	}
	selection, err := iam.NewContextSelectionService(repo, tokens)
	if err != nil {
		return err
	}
	tenants := iam.NewPlatformTenantService(repo, nil)
	memberships := iam.NewTenantMembershipService(repo, nil)
	roles := iam.NewTenantRoleService(repo, nil)
	// Reserve occupies ID 1; A and B must be distinct, nondefault Tenant scopes.
	for index, code := range []string{"reserve", "a", "b"} {
		admin, err := createUser(ctx, identity, "online-metrics-tenant-"+code)
		if err != nil {
			return err
		}
		tenant, err := tenants.Create(ctx, iam.CreateTenantInput{Code: "online-metrics-" + code, Name: "Online metrics " + code,
			InitialAdministratorPrincipalID: admin.PrincipalID, ActorPrincipalID: admin.PrincipalID, Audit: audit("online-metrics-tenant-" + code)})
		if err != nil {
			return err
		}
		if index == 0 {
			continue
		}
		if tenant.ID <= 1 {
			return fmt.Errorf("metrics isolation requires nondefault Tenant")
		}
		session, _, _, err := createPermissionFixture(ctx, identity, memberships, roles, selection, tenant.ID, admin.PrincipalID,
			"online-metrics-reader-"+code, []string{"monitor.execution.read"})
		if err != nil {
			return err
		}
		if index == 1 {
			values["ADDP_ONLINE_TEST_TENANT_ID"] = fmt.Sprint(tenant.ID)
			values["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"] = session.AccessToken
		} else {
			values["ADDP_ONLINE_METRICS_FOREIGN_TENANT_ID"] = fmt.Sprint(tenant.ID)
			values["ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN"] = session.AccessToken
		}
	}
	return writeEnvironmentFile(output, values)
}
