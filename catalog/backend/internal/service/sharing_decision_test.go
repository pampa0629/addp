package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeSharingTargetResolver struct {
	calls  int
	before func()
	err    error
}

func (r *fakeSharingTargetResolver) ResolveSharingTarget(_ context.Context, tenantID, itemID int64, fingerprint string) (plugin.EngineCatalogPath, error) {
	r.calls++
	if r.before != nil {
		r.before()
	}
	if r.err != nil {
		return plugin.EngineCatalogPath{}, r.err
	}
	return plugin.EngineCatalogBranchLeafPath(plugin.TabularCatalogModel("schema"), 12, "schema", "public", "table", "table", "orders"), nil
}

func sharingAuth() authorization.AuthContext {
	return authtest.NewTenantUserAuthContext("7", "40", []string{"catalog.entry.read", "catalog.sharing_decision.create"})
}

func seedSharingEntry(t *testing.T, db *gorm.DB) (models.Entry, SharingDecisionInput) {
	t.Helper()
	entry, _ := createEditableCatalogEntry(t, db, 7)
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", entry.ID).Update("observed_snapshot", commonModels.JSONMap{"item_id": 21, "name": "orders"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Responsibility{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID,
		Role: models.ResponsibilityRoleBusinessOwner, SubjectType: "user", SubjectID: 40,
		Status: models.ResponsibilityStatusActive, ObservedSnapshot: commonModels.JSONMap{}, VerifiedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().UTC().AddDate(8, 0, 0)
	return entry, SharingDecisionInput{DecisionID: uuid.New(), Version: 1, RecipientType: "user", RecipientID: 40,
		ExpiryMode: authorization.SharingExpiryAtTime, ExpiresAt: &expiresAt, Reason: "Explicit self read"}
}

func TestSharingDecisionSelfReadExpiryRetryAndHistoricalOwnership(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, input := seedSharingEntry(t, db)
	resolver := &fakeSharingTargetResolver{}
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(resolver)
	auth := sharingAuth()
	result, created, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, auth)
	if err != nil || !created || result.EntryVersion != 2 || !result.SelfBeneficiary || result.Action != "read" || result.ConfirmerInProjectGroup {
		t.Fatalf("create=%+v created=%v err=%v", result, created, err)
	}
	if result.ConfirmedBy != 40 || result.ConfirmerMembershipID == 0 || result.AuthorizationVersion == 0 || result.Target.EngineID != "12" || len(result.Target.Segments) != 3 {
		t.Fatalf("incomplete provenance: %+v", result)
	}
	if result.ExpiresAt.Nanosecond()%1000 != 0 {
		t.Fatal("expiry precision not canonical")
	}
	var audit models.AuditEvent
	if err := db.Where("event_type = ?", "catalog.sharing_decision.created").Take(&audit).Error; err != nil || audit.ActorID != "40" {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
	// Owner transfer does not rewrite history or make an identical retry new.
	if err := db.Where("catalog_entry_id = ?", entry.ID).Delete(&models.Responsibility{}).Error; err != nil {
		t.Fatal(err)
	}
	resolver.err = ErrReferenceValidationUnavailable
	recovered, created, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, auth)
	if err != nil || created || recovered.ID != result.ID || !recovered.CreatedAt.Equal(result.CreatedAt) || resolver.calls != 1 {
		t.Fatalf("retry=%+v created=%v calls=%d err=%v", recovered, created, resolver.calls, err)
	}
	if _, err := s.GetSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, result.ID, auth); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SharingDecisionInput){func(i *SharingDecisionInput) { i.Reason = "different" }, func(i *SharingDecisionInput) { i.RecipientID++ }, func(i *SharingDecisionInput) { changed := i.ExpiresAt.Add(time.Second); i.ExpiresAt = &changed }, func(i *SharingDecisionInput) {
		i.ExpiryMode, i.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil
	}, func(i *SharingDecisionInput) { i.Version++ }} {
		changed := input
		mutate(&changed)
		if _, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, changed, auth); !errors.Is(err, ErrSharingDecisionConflict) {
			t.Fatalf("changed retry accepted: %v", err)
		}
	}
	other := auth
	other.Principal.ID = "41"
	if _, err := s.GetSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, result.ID, other); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("other confirmer history: %v", err)
	}
	if _, err := s.GetSharingDecision(context.Background(), 7, EntryAccess{}, entry.ID, result.ID, auth); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("invisible history: %v", err)
	}
	var count int64
	db.Model(&models.SharingDecision{}).Count(&count)
	if count != 1 {
		t.Fatalf("decisions=%d", count)
	}
	db.Model(&models.AuditEvent{}).Where("event_type = ?", "catalog.sharing_decision.created").Count(&count)
	if count != 1 {
		t.Fatalf("audits=%d", count)
	}
	if err := db.First(&entry, "id = ?", entry.ID).Error; err != nil || entry.Version != 2 {
		t.Fatalf("retry changed aggregate=%+v err=%v", entry, err)
	}
}

func TestSharingDecisionOwnProjectGroupIsExplicitAndAllowed(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, input := seedSharingEntry(t, db)
	input.RecipientType, input.RecipientID = "project_group", 50
	auth := sharingAuth()
	auth.Organization.ProjectGroups = []authorization.ProjectGroupMembership{{ProjectGroupID: "50"}}
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	result, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, auth)
	if err != nil || !result.ConfirmerInProjectGroup || result.SelfBeneficiary {
		t.Fatalf("group decision=%+v err=%v", result, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var view map[string]any
	if err := json.Unmarshal(encoded, &view); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"engine_id", "recipient_id", "confirmed_by", "entry_version", "confirmer_membership_id", "authorization_version"} {
		if _, ok := view[key].(string); !ok {
			t.Fatalf("%s must be decimal string: %s", key, encoded)
		}
	}
}

func TestSharingDecisionLongTermIsExplicitImmutableAndStillRequiresPermissions(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, input := seedSharingEntry(t, db)
	input.ExpiryMode, input.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil
	s := NewEntryService(db, nil, &fakeSystemReferenceResolver{}).WithSharingTargetResolver(&fakeSharingTargetResolver{})
	result, created, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth())
	if err != nil || !created || result.ExpiresAt != nil || result.ExpiryMode != authorization.SharingExpiryUntilRevoked {
		t.Fatalf("long-term decision=%+v created=%v err=%v", result, created, err)
	}
	if _, created, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, input, sharingAuth()); err != nil || created {
		t.Fatalf("long-term retry created=%v err=%v", created, err)
	}
	expiry := time.Now().Add(time.Hour)
	changed := input
	changed.ExpiryMode, changed.ExpiresAt = authorization.SharingExpiryAtTime, &expiry
	if _, _, err := s.CreateSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, changed, sharingAuth()); !errors.Is(err, ErrSharingDecisionConflict) {
		t.Fatalf("changed long-term binding accepted: %v", err)
	}
	auth := sharingAuth()
	auth.Authorization.RoleAssignments = nil
	if _, err := s.GetSharingDecision(context.Background(), 7, EntryAccess{Inventory: true}, entry.ID, result.ID, auth); !errors.Is(err, ErrSharingConfirmationForbidden) {
		t.Fatalf("long-term mode bypassed current permissions: %v", err)
	}
}

func TestSharingDecisionFailsClosedWithoutMutatingAggregate(t *testing.T) {
	for _, name := range []string{"no_permission", "no_read", "not_owner", "steward", "service", "delegated", "foreign_context", "bad_membership", "expired_role", "future_role", "expired_token", "role_expired_during_validation", "nul_reason", "zero_expiry", "past_expiry", "no_mode", "unknown_mode", "indefinite_with_date", "inactive_recipient", "unavailable_meta", "source_changed", "deprecated", "invisible", "no_system"} {
		t.Run(name, func(t *testing.T) {
			db := openCatalogServiceTestDB(t)
			entry, input := seedSharingEntry(t, db)
			auth := sharingAuth()
			resolver := &fakeSharingTargetResolver{}
			system := &fakeSystemReferenceResolver{}
			s := NewEntryService(db, nil, system).WithSharingTargetResolver(resolver)
			access := EntryAccess{Inventory: true}
			want := ErrSharingConfirmationForbidden
			switch name {
			case "no_permission":
				auth.Authorization.RoleAssignments[0].Permissions = []string{"catalog.entry.read", "catalog.entry.update"}
			case "no_read":
				auth.Authorization.RoleAssignments[0].Permissions = []string{"catalog.sharing_decision.create"}
			case "not_owner":
				auth.Principal.ID = "41"
			case "steward":
				db.Model(&models.Responsibility{}).Where("catalog_entry_id = ?", entry.ID).Update("role", models.ResponsibilityRoleDataSteward)
			case "service":
				auth.Principal.Type = "service"
			case "delegated":
				auth.Delegation = &authorization.DelegationFacts{}
			case "foreign_context":
				foreign := "8"
				auth.Context.TenantID = &foreign
			case "bad_membership":
				bad := "01"
				auth.Context.TenantMembershipID = &bad
			case "expired_role":
				expired := time.Now().Add(-time.Second)
				auth.Authorization.RoleAssignments[0].ValidUntil = &expired
			case "future_role":
				auth.Authorization.RoleAssignments[0].ValidFrom = time.Now().Add(time.Hour)
			case "expired_token":
				auth.Token.ExpiresAt = time.Now().Add(-time.Second)
			case "role_expired_during_validation":
				until := time.Now().Add(time.Hour)
				auth.Authorization.RoleAssignments[0].ValidUntil = &until
				resolver.before = func() { until = time.Now().Add(-time.Second) }
			case "nul_reason":
				input.Reason = "invalid\x00reason"
				want = ErrInvalidEntryUpdate
			case "zero_expiry":
				input.ExpiresAt = nil
				want = ErrInvalidEntryUpdate
			case "past_expiry":
				past := time.Now().Add(-time.Hour)
				input.ExpiresAt = &past
				want = ErrInvalidEntryUpdate
			case "no_mode":
				input.ExpiryMode = ""
				want = ErrInvalidEntryUpdate
			case "unknown_mode":
				input.ExpiryMode = "forever"
				want = ErrInvalidEntryUpdate
			case "indefinite_with_date":
				input.ExpiryMode = authorization.SharingExpiryUntilRevoked
				want = ErrInvalidEntryUpdate
			case "inactive_recipient":
				input.RecipientID = 41
				system.rejectedUserID = 41
				want = ErrReferenceNotReferenceable
			case "unavailable_meta":
				resolver.err = ErrReferenceValidationUnavailable
				want = ErrReferenceValidationUnavailable
			case "source_changed":
				resolver.before = func() {
					if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", entry.ID).Update("source_version", "00000000000000000002").Error; err != nil {
						t.Fatal(err)
					}
				}
				want = ErrEntryVersionConflict
			case "deprecated":
				db.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("governance_status", models.GovernanceStatusDeprecated)
				want = ErrSharingTargetUnsupported
			case "invisible":
				access.Inventory = false
				want = ErrEntryNotFound
			case "no_system":
				s.system = nil
				want = ErrReferenceValidationUnavailable
			}
			if _, _, err := s.CreateSharingDecision(context.Background(), 7, access, entry.ID, input, auth); !errors.Is(err, want) {
				t.Fatalf("err=%v want=%v", err, want)
			}
			var count int64
			if err := db.Model(&models.SharingDecision{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("partial history=%d err=%v", count, err)
			}
			if err := db.First(&entry, "id = ?", entry.ID).Error; err != nil || entry.Version != 1 {
				t.Fatalf("partial version=%d err=%v", entry.Version, err)
			}
		})
	}
}

func TestDatabaseSharingPathUsesTypedAncestryNotSplitNames(t *testing.T) {
	model := plugin.TabularCatalogModel("schema")
	for _, names := range [][2]string{{"public", "orders"}, {" team.space/一 ", " orders.v2/a "}} {
		rootID := uint(10)
		facts := commonModels.MetaItemAncestors{Item: commonModels.MetaItem{ID: 21, TenantID: 7, EngineID: 12, NodeID: 11, ItemType: "table", Name: names[1], FullName: names[0] + "." + names[1]}, Ancestors: []commonModels.MetaNode{{ID: rootID, TenantID: 7, EngineID: 12, NodeType: "server"}, {ID: 11, ParentNodeID: &rootID, TenantID: 7, EngineID: 12, NodeType: "schema", Name: names[0]}}}
		facts.Item.Fingerprint = commonModels.GenerateItemFingerprint(12, facts.Item.FullName)
		path, err := databaseSharingPath(7, facts.Item.Fingerprint, &facts, model)
		if err != nil || len(path.Segments) != 3 || path.Segments[1].Name != names[0] || path.Segments[2].Name != names[1] {
			t.Fatalf("path=%+v err=%v", path, err)
		}
		for _, name := range []string{"tenant", "engine", "parent", "duplicate", "namespace_type", "leaf", "fingerprint", "full_name", "root", "missing"} {
			copy := facts
			copy.Ancestors = append([]commonModels.MetaNode(nil), facts.Ancestors...)
			switch name {
			case "tenant":
				copy.Item.TenantID++
			case "engine":
				copy.Ancestors[1].EngineID++
			case "parent":
				wrong := uint(90)
				copy.Ancestors[1].ParentNodeID = &wrong
			case "duplicate":
				copy.Ancestors[1].ID = rootID
			case "namespace_type":
				copy.Ancestors[1].NodeType = "folder"
			case "leaf":
				copy.Item.ItemType = "file"
			case "fingerprint":
				copy.Item.Fingerprint = "wrong"
			case "full_name":
				copy.Item.FullName = "unverified"
			case "root":
				copy.Ancestors[0].NodeType = "engine"
			case "missing":
				copy.Ancestors = copy.Ancestors[:1]
			}
			if _, err := databaseSharingPath(7, facts.Item.Fingerprint, &copy, model); !errors.Is(err, ErrSharingTargetUnsupported) {
				t.Fatalf("%s accepted: %v", name, err)
			}
		}
	}
}
