package api

import (
	"fmt"
	commonapi "github.com/addp/common/api"
	"github.com/addp/service/internal/models"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueryDefinitionRejectsUntrustedPlanAndTrailingJSON(t *testing.T) {
	valid := `{"service_name":"metric","title":"Metric","config_type":"analytical","metric_source":{"implementation_id":3,"revision_id":7}}`
	for _, body := range []string{strings.TrimSuffix(valid, "}") + `,"execution_plan":{}}`, valid + ` {}`, strings.Replace(valid, `"revision_id":7`, `"revision_id":7,"sql":"SELECT 1"`, 1)} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
		var request models.CreateQueryServiceRequest
		if bindQueryDefinition(c, &request) == nil {
			t.Fatal("publication accepted untrusted or trailing fields")
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(valid))
	var request models.CreateQueryServiceRequest
	if err := bindQueryDefinition(c, &request); err != nil {
		t.Fatal(err)
	}
}

func TestMetricRebindRequiresIntegerVersionOnly(t *testing.T) {
	for _, tc := range []struct {
		version string
		valid   bool
	}{
		{`"version":1`, true},
		{`"version":0`, false},
		{`"version":-1`, false},
		{`"version":"1"`, false},
		{`"version":1.2`, false},
		{`"version":1,"definition_version":"current"`, false},
		{`"service_version":"consumer"`, false},
		{`"definition_version":""`, false},
		{`"definition_version":"current","service_version":"consumer"`, false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPut, "/query/35/metric-source", strings.NewReader(`{"metric_source":{"implementation_id":3,"revision_id":4},`+tc.version+`}`))
		var req models.RebindMetricSourceRequest
		err := bindQueryDefinition(c, &req)
		if (err == nil) != tc.valid {
			t.Fatalf("version contract %s: %v", tc.version, err)
		}
	}
}

func TestQueryManagementRequiresVersion(t *testing.T) {
	for _, factory := range []func() interface{}{
		func() interface{} { return &models.UpdateQueryServiceRequest{} },
		func() interface{} { return &models.QueryServiceVersionRequest{} },
	} {
		for _, body := range []string{`{}`, `{"version":0}`, `{"version":-1}`, `{"version":"1"}`, `{"definition_version":"old"}`, `{"version":1,"service_version":"old"}`} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPut, "/query/1", strings.NewReader(body))
			if bindQueryDefinition(c, factory()) == nil {
				t.Fatalf("invalid request accepted: %s", body)
			}
		}
	}
}

func TestQueryManagementConflictResponse(t *testing.T) {
	for _, err := range []error{commonapi.ErrConflict, fmt.Errorf("write query: %w", commonapi.ErrConflict)} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		if !writeQueryVersionConflict(c, err) || recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"error_code":"resource_version_conflict"`) {
			t.Fatalf("conflict response: %d %s", recorder.Code, recorder.Body.String())
		}
	}
}
