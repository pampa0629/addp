package api

import (
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/addp/catalog/internal/service"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func TestCollectionAccessIntersectsTenantPermissionsWithCurrentMemberships(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name        string
		permissions []string
		memberships bool
		read        []int64
		update      []int64
		options     []service.CollectionProjectGroupAccess
	}{
		{name: "membership alone grants nothing", memberships: true},
		{name: "tenant permissions do not enumerate nonmembers", permissions: []string{"catalog.collection.read", "catalog.collection.update"}},
		{name: "read permission covers only current memberships", permissions: []string{"catalog.collection.read"}, memberships: true,
			read: []int64{9, 9007199254740993}, options: []service.CollectionProjectGroupAccess{
				{ProjectGroupID: 9, RelationRole: "member", CanRead: true},
				{ProjectGroupID: 9007199254740993, RelationRole: "leader", CanRead: true},
			}},
		{name: "update alone cannot produce a writable option", permissions: []string{"catalog.collection.update"}, memberships: true,
			update: []int64{9, 9007199254740993}},
		{name: "read and update cover the same current memberships", permissions: []string{"catalog.collection.read", "catalog.collection.update"}, memberships: true,
			read: []int64{9, 9007199254740993}, update: []int64{9, 9007199254740993}, options: []service.CollectionProjectGroupAccess{
				{ProjectGroupID: 9, RelationRole: "member", CanRead: true, CanUpdate: true},
				{ProjectGroupID: 9007199254740993, RelationRole: "leader", CanRead: true, CanUpdate: true},
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			auth := authtest.NewTenantUserAuthContext("7", "40", test.permissions)
			if len(test.permissions) == 0 {
				auth.Authorization.RoleAssignments = []authorization.RoleAssignment{}
			}
			auth.Organization.Departments = []authorization.DepartmentMembership{
				{MembershipID: "73", DepartmentID: "31", MembershipType: "primary", RelationRole: "member", AncestorIDs: []string{"30"}},
			}
			if test.memberships {
				auth.Organization.ProjectGroups = []authorization.ProjectGroupMembership{
					{MembershipID: "71", ProjectGroupID: "9", RelationRole: "member"},
					{MembershipID: "72", ProjectGroupID: "9007199254740993", RelationRole: "leader"},
				}
			}
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			if err := commonAuth.SetAuthContextForGin(context, auth); err != nil {
				t.Fatal(err)
			}
			access := collectionAccess(context, 40)
			if !slices.Equal(access.ReadGroupIDs, test.read) || !slices.Equal(access.UpdateGroupIDs, test.update) {
				t.Fatalf("collection access = %+v, want read %v / update %v", access, test.read, test.update)
			}
			options := collectionProjectGroupAccesses(context)
			if len(options) != len(test.options) || len(options) > 0 && !reflect.DeepEqual(options, test.options) {
				t.Fatalf("project group options = %+v, want %+v", options, test.options)
			}
			// Collection permissions and group membership cannot grant inventory access.
			if access.EntryAccess.Inventory {
				t.Fatal("collection permission expanded entry visibility")
			}
			if !slices.Equal(access.EntryAccess.DepartmentIDs, []int64{31}) {
				t.Fatalf("department visibility membership was changed: %+v", access.EntryAccess)
			}
		})
	}
}

func TestCollectionAccessRequiresCanonicalAuthContext(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	if groups := permissionProjectGroupIDs(context, "catalog.collection.read"); len(groups) != 0 {
		t.Fatalf("unauthenticated groups = %v", groups)
	}
}
