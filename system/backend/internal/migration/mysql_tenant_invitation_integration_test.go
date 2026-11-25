package migration

import (
	"errors"
	"fmt"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"gorm.io/gorm"
)

func checkMySQLTenantInvitation(t *testing.T, db *gorm.DB, repo *iam.Repository, tokens *iam.TokenFamilyService, member iam.TenantMembership) {
	ctx := t.Context()
	now := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	svc, err := iam.NewTenantInvitationService(repo, iam.NewIdentityService(repo, nil), tokens, iam.TenantInvitationServiceConfig{InvitationTTL: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	kind, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
	audit := iam.AuditMetadata{PrincipalID: &member.PrincipalID, PrincipalType: &kind, ContextType: &contextType, TenantID: &member.TenantID}
	input := iam.CreateTenantInvitationInput{TenantID: member.TenantID, CreatedByPrincipalID: member.PrincipalID, Email: "Invitee@example.test", Audit: audit}
	first, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Email = "invitee@example.test"
	if _, err = svc.Create(ctx, input); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate pending email: %v", err)
	}
	if _, err = svc.Revoke(ctx, member.TenantID, first.Invitation.ID, member.PrincipalID, audit); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	for i := 0; i < 2; i++ {
		list, total, err := svc.List(ctx, member.TenantID, 1, 10, "ViTeE", nil, audit)
		if err != nil || total != 2 || len(list) != 2 || list[0].ID != second.Invitation.ID || list[0].Status != iam.TenantInvitationStatusExpired {
			t.Fatalf("expiry list count=%d error=%v", total, err)
		}
	}
	var count int64
	if err := db.Table("system.audit_logs").Where("event_name=? AND tenant_id=? AND entity_id=?", "iam.tenant_invitation.expired", member.TenantID, fmt.Sprint(second.Invitation.ID)).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expiry audit count=%d err=%v", count, err)
	}
	if err := db.Exec("DELETE FROM tenant_invitations WHERE id=?", second.Invitation.ID).Error; err == nil {
		t.Fatal("physically deleted terminal invitation")
	}
	if err := db.Exec("UPDATE tenant_invitations SET status='pending', expired_at=NULL WHERE id=?", second.Invitation.ID).Error; err == nil {
		t.Fatal("reopened terminal invitation")
	}
	third, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.Register(ctx, iam.RegisterTenantInvitationInput{InvitationSecret: third.Secret, Username: "invited-mysql", Password: "invited-mysql-password", DisplayName: "受邀用户"})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Invitation.Status != iam.TenantInvitationStatusAccepted || accepted.Membership.TenantID != member.TenantID || accepted.Membership.SourceType != iam.TenantMembershipSourceInvitation || accepted.Session.AccessToken == "" {
		t.Fatal("registration did not create a tenant membership and login session")
	}
}
