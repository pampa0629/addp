package api

import (
	"encoding/json"
	"errors"
	"fmt"
	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/service/internal/models"
	svc "github.com/addp/service/internal/service"
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

func TestQueryServiceMetricReferenceFilter(t *testing.T) {
	for _, tc := range []struct {
		query string
		valid bool
	}{
		{"", true}, {"metric_implementation_id=1&metric_revision_id=6", true},
		{"metric_implementation_id=1", false}, {"metric_revision_id=6", false},
		{"metric_implementation_id=&metric_revision_id=6", false},
		{"metric_implementation_id=0&metric_revision_id=6", false},
		{"metric_implementation_id=1&metric_revision_id=-1", false},
		{"metric_implementation_id=1&metric_revision_id=1.5", false},
		{"metric_implementation_id=1&metric_revision_id=99999999999999999999", false},
		{"metric_implementation_id=1&metric_revision_id=6&metric_revision_id=7", false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/query?"+tc.query, nil)
		filter, err := queryServiceMetricFilter(c)
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.query, err)
		}
		if tc.valid && tc.query != "" && (filter.ImplementationID != 1 || filter.RevisionID != 6) {
			t.Fatalf("wrong reference: %#v", filter)
		}
	}
}

func TestQueryDefaultUpdateRequestDoesNotOpenParameterSchemaEditing(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"version":1,"named_parameter_defaults":{"minimum":2}}`, true},
		{`{"version":1,"named_parameters":[{"name":"new","type":"string"}]}`, false},
		{`{"version":1,"sql_query":"SELECT 1"}`, false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("PUT", "/query/1", strings.NewReader(tc.body))
		var req models.UpdateQueryServiceRequest
		err := bindQueryDefinition(c, &req)
		if (err == nil) != tc.valid {
			t.Fatalf("default update request: %s %v", tc.body, err)
		}
	}
}

func TestQueryDefaultErrorsAreLocalizedAndSafe(t *testing.T) {
	for _, language := range []string{"zh-cn", "en"} {
		router := gin.New()
		router.Use(commoni18n.I18nMiddleware())
		router.GET("/error", func(c *gin.Context) {
			writeQueryParameterDefaultsError(c, errors.Join(svc.ErrInvalidNamedParameterDefaults, errors.New("private-sql-and-credential")))
		})
		request := httptest.NewRequest("GET", "/error", nil)
		request.Header.Set("Accept-Language", language)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var body map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		expected := "可选参数"
		if language == "en" {
			expected = "optional SQL parameters"
		}
		if response.Code != 400 || body["error_code"] != "invalid_named_parameter_defaults" || len(body) != 2 || !strings.Contains(body["error"], expected) || strings.Contains(response.Body.String(), "private-sql") {
			t.Fatalf("unsafe or wrong default error: %d %s", response.Code, response.Body.String())
		}
	}
}
