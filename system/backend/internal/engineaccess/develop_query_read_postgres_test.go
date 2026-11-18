package engineaccess

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func exerciseDevelopQueryReadGrants(t *testing.T, db *gorm.DB, path plugin.EngineCatalogPath, tenant, admin int64, roles *iam.TenantRoleService,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time), grant func(*testing.T, string, int64, *time.Time) uuid.UUID, revoke func(*testing.T, uuid.UUID), credential func(*testing.T, userProvenance) string) {
	t.Run("Develop query current Grants and final Meta recheck", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		user, _ := newUser(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenant, RoleKey: "custom.query_source_read", Name: "Query source read fixture", PermissionKeys: []string{"develop.task.execute", "develop.data_read.execute"}, ActorPrincipalID: admin})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenant, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: admin, Reason: "Query source fixture"}); err != nil {
			t.Fatal(err)
		}
		token := credential(t, user)
		executions, err := iam.NewExecutionAuthorizationService(iam.NewRepository(db))
		if err != nil {
			t.Fatal(err)
		}
		requestID := "develop-query-source"
		issued, err := executions.Issue(ctx, iam.IssueExecutionAuthorizationInput{SourceAccessToken: token, Audience: "develop", ExecutionID: uuid.New(), Accesses: []iam.ExecutionEngineAccessScope{{EngineID: int64(path.EngineID), Effects: []string{"read"}}}, ExpiresIn: time.Minute, Audit: iam.AuditMetadata{RequestID: &requestID}})
		if err != nil {
			t.Fatal(err)
		}
		var version int64
		if err := db.Table("system.engines").Select("version").Where("id=?", path.EngineID).Scan(&version).Error; err != nil {
			t.Fatal(err)
		}
		set, _ := plugin.NewQueryReadSet(path)
		request := client.DevelopQueryReadRequest{AuthorizationID: strconv.FormatInt(issued.ID, 10), ExecutionID: issued.ExecutionID.String(), EngineID: strconv.FormatUint(uint64(path.EngineID), 10), EngineVersion: strconv.FormatInt(version, 10), ReadSet: set, SampleCredential: token}
		metaCalls := 0
		var afterMeta func()
		service := NewService(NewRepository(db), nil).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, actualTenant uint, _ string) (*authorizationfacts.AuthorizationFacts, error) {
			metaCalls++
			if actualTenant != uint(tenant) {
				t.Fatal("wrong Meta tenant")
			}
			if afterMeta != nil {
				afterMeta()
			}
			facts := testBindingFacts(path)
			return &facts, nil
		}))
		check := func(allowed bool) {
			t.Helper()
			result, err := service.CheckDevelopQueryRead(ctx, tenant, request)
			if allowed {
				if err != nil || result == nil || !result.ExpiresAt.After(result.ObservedAt) {
					t.Fatalf("source observation=%+v err=%v", result, err)
				}
			} else if result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("uncovered source admitted: %+v %v", result, err)
			}
		}
		check(false)
		id := grant(t, "user", user.PrincipalID, nil)
		check(true)
		revoke(t, id)
		check(false)
		id = grant(t, "user", user.PrincipalID, nil)
		afterMeta = func() { revoke(t, id) }
		check(false) // Meta preparation must not preserve the earlier Grant.
		afterMeta = nil
		before := metaCalls
		request.ReadSet, _ = plugin.NewQueryReadSet()
		check(true)
		if metaCalls != before {
			t.Fatal("proven empty source enumerated Meta")
		}
	})
}
