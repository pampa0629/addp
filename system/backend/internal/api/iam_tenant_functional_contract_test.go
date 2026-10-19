package api

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/gin-gonic/gin"
)

func TestIAMTenantFunctionalRequestsRejectRetiredScopeFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, body string
		target     any
	}{
		{"role scopes", `{"role_key":"custom.reader","name":"Reader","scope_types":["tenant"],"permission_keys":["manager.data_item.read"]}`, &IAMTenantRoleRequest{}},
		{"assignment tenant scope", `{"membership_id":"1","role_ids":["2"],"scope_type":"tenant","reason":"test"}`, &IAMCreateTenantRoleAssignmentsRequest{}},
		{"assignment department", `{"membership_id":"1","role_ids":["2"],"department_id":null,"reason":"test"}`, &IAMCreateTenantRoleAssignmentsRequest{}},
		{"assignment project", `{"membership_id":"1","role_ids":["2"],"project_group_id":"3","reason":"test"}`, &IAMCreateTenantRoleAssignmentsRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			if err := commonapi.BindOptionalJSONStrict(c, test.target); err == nil {
				t.Fatal("retired request field accepted")
			}
		})
	}
}

func TestIAMTenantRoleAssignmentFilterRejectsRetiredAndRepeatedQueries(t *testing.T) {
	for _, query := range []string{"scope_type=tenant", "scope_type=department", "department_id=3", "project_group_id=4", "membership_id=1&membership_id=2", "unknown=1"} {
		t.Run(query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/?"+query, nil)
			if _, err := parseIAMTenantRoleAssignmentFilter(c); !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("query accepted: %v", err)
			}
		})
	}
}
