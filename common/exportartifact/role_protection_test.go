package exportartifact

import (
	"context"
	"errors"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRoleExportSessionBindsUserAuthorityAndRechecksDelivery(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(db, "export_sessions"); err != nil {
		t.Fatal(err)
	}
	store := NewGormStore(db, "export_sessions")
	transfer := &transferStub{}
	svc := NewService(transfer, store, testMinIOClient(t), "manager", "develop", "/api/v1/develop/exports")
	choice := dataprotection.FieldRoleChoice{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", PolicyRevision: 1, AssessmentRevision: 1, OptionID: "hash"}}
	req := CreateRequest{TenantID: 7, UserID: 9, SourceRef: "query", FileName: "selected", ExecutionConfig: commonClient.TransferExecutionConfig{Source: commonClient.TransferExecutionEndpoint{Locator: "source", ProtectionChoices: []dataprotection.FieldRoleChoice{choice}}}}
	if _, err := svc.Create(t.Context(), req); err == nil || transfer.request != nil {
		t.Fatal("role export created without user authority")
	}
	issuedID := ""
	req.AuthorizeExecution = func(_ context.Context, id string, _ commonClient.TransferExecutionConfig) (*commonClient.IssuedExecutionAuthorization, error) {
		issuedID = id
		return &commonClient.IssuedExecutionAuthorization{ID: "81", ExecutionID: id, Audience: "transfer", TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: "13", IssuedAuthorizationVersion: "2", SourceType: "user", ExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	response, err := svc.Create(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !response.RequiresCurrentUser || issuedID != response.TransferExecutionID || transfer.request.ExecutionAuthorization == nil {
		t.Fatal("lost protected session or exact execution authority")
	}
	session, err := store.Get(t.Context(), response.ID, 7, 9)
	if err != nil || session.Protection == nil || len(session.Protection.Choices) != 1 {
		t.Fatal("lost source selection persistence", err)
	}
	digest, _ := commonClient.ExportRequestDigest(transfer.request)
	if session.ExecutionRequestDigest != digest {
		t.Fatal("owner provenance does not freeze authority and choices")
	}
	session.Status = StatusSuccess
	if err := store.UpdateStatus(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	for _, open := range []bool{false, true} {
		if open {
			_, err = svc.Open(t.Context(), session.ID, 7, 9, nil)
		} else {
			_, err = svc.Get(t.Context(), session.ID, 7, 9, nil)
		}
		if !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
			t.Fatal("missing download qualification accepted", err)
		}
	}
	calls := 0
	deny := func(_ context.Context, selection *commonClient.TransferSourceProtectionSelection) error {
		calls++
		if selection.SourceLocator != "source" || selection.Choices[0] != choice {
			t.Error("changed delivery selection")
		}
		return dataprotection.ErrRoleOptionIneligible
	}
	if _, err := svc.Get(t.Context(), session.ID, 7, 9, deny); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		t.Fatal(err)
	}
	if _, err := svc.Open(t.Context(), session.ID, 7, 9, deny); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("qualification not rechecked at both boundaries")
	}
}
