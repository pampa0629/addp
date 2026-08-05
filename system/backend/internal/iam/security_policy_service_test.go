package iam

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateSecurityPolicy(t *testing.T) {
	valid := DefaultSecurityPolicy()
	if err := ValidateSecurityPolicy(valid); err != nil {
		t.Fatalf("ValidateSecurityPolicy(default) error = %v", err)
	}

	invalid := valid
	invalid.DelegatedAccessTokenTTLMinutes = 3
	if err := ValidateSecurityPolicy(invalid); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("delegated token TTL error = %v", err)
	}

	invalid = valid
	invalid.AccessTokenTTLMinutes = 10
	invalid.ResourceAccessTicketTTLMinutes = 11
	if err := ValidateSecurityPolicy(invalid); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("resource ticket TTL error = %v", err)
	}
}

func TestSecurityPolicyServiceUpdateAuditsAndRequiresRestart(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:iam-security-policy?mode=memory&cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&SecurityPolicy{}, &AuditLog{}); err != nil {
		t.Fatal(err)
	}
	policy := DefaultSecurityPolicy()
	policy.CreatedAt = time.Now().UTC()
	policy.UpdatedAt = policy.CreatedAt
	if err := db.Create(&policy).Error; err != nil {
		t.Fatal(err)
	}

	service := NewSecurityPolicyService(NewRepository(db))
	input := UpdateSecurityPolicyInput{
		ExpectedVersion:                  1,
		AccessTokenTTLMinutes:            20,
		DelegatedAccessTokenTTLMinutes:   2,
		ResourceAccessTicketTTLMinutes:   15,
		RefreshTokenTTLDays:              30,
		OAuthAuthorizationCodeTTLMinutes: 5,
		OAuthDeviceCodeTTLMinutes:        10,
		OAuthDevicePollIntervalSeconds:   5,
		TenantInvitationTTLHours:         168,
		EnrollmentTicketTTLMinutes:       5,
		OAuthPublicRateLimitPerMinute:    60,
		OAuthUserRateLimitPerMinute:      30,
		UpdatedByPrincipalID:             42,
		Audit:                            AuditMetadata{},
	}
	updated, err := service.Update(context.Background(), input)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 2 || updated.AppliedVersion != 1 || updated.UpdatedByPrincipalID == nil || *updated.UpdatedByPrincipalID != 42 {
		t.Fatalf("updated policy = %#v", updated)
	}

	var audit AuditLog
	if err := db.Where("event_name = ?", "iam.security_policy.updated").Take(&audit).Error; err != nil {
		t.Fatalf("query audit: %v", err)
	}
	var details map[string]any
	if err := json.Unmarshal(audit.Details, &details); err != nil {
		t.Fatalf("decode audit details: %v", err)
	}
	if details["pending_restart"] != true || details["previous_version"] != float64(1) || details["version"] != float64(2) {
		t.Fatalf("audit details = %#v", details)
	}

	if _, err := service.Update(context.Background(), input); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}

	applied, err := service.LoadAndMarkApplied(context.Background())
	if err != nil {
		t.Fatalf("LoadAndMarkApplied() error = %v", err)
	}
	if applied.Version != 2 || applied.AppliedVersion != 2 {
		t.Fatalf("applied policy = %#v", applied)
	}
}
