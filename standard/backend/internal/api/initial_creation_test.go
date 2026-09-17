package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/standard/internal/models"
	"github.com/gin-gonic/gin"
)

func TestInitialCreationRequestContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body string
		request    func() interface{}
	}{
		{"glossary", `{"code":"customer","scope_type":"tenant_common","name":"Customer","definition":"Customer"}`, func() interface{} { return &models.CreateGlossaryRequest{} }},
		{"element", `{"code":"phone","scope_type":"tenant_common","name":"Phone","definition":"Phone","data_type":"string","value_domain_kind":"unrestricted"}`, func() interface{} { return &models.CreateElementRequest{} }},
		{"code_set", `{"code":"status","scope_type":"tenant_common","name":"Status","description":"Status","value_type":"string"}`, func() interface{} { return &models.CreateCodeSetRequest{} }},
		{"metric", `{"code":"count","scope_type":"tenant_common","name":"Count","definition":"Count","statistical_caliber":"Count","metric_type":"atomic"}`, func() interface{} { return &models.CreateMetricRequest{} }},
		{"document", `{"code":"reference","scope_type":"tenant_common","name":"Reference","doc_type":"reference"}`, func() interface{} { return &models.CreateDocumentRequest{} }},
		{"linked_document", `{"version":1,"code":"reference","scope_type":"tenant_common","name":"Reference","doc_type":"reference"}`, func() interface{} { return &models.CreateLinkedDocumentRequest{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/create", func(c *gin.Context) {
				if bindStandardDefinition(c, tc.request()) {
					c.Status(http.StatusNoContent)
				}
			})
			for _, retiredField := range []string{"", `,"change_summary":"user supplied"`, `,"steward_id":1`} {
				body := tc.body
				expected := http.StatusNoContent
				if retiredField != "" {
					body = strings.TrimSuffix(body, "}") + retiredField + "}"
					expected = http.StatusBadRequest
				}
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/create", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(response, request)
				if response.Code != expected {
					t.Fatalf("client summary=%v: status=%d body=%s", retiredField, response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestIdentityUpdatesRejectRetiredStewardField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, handler := range map[string]gin.HandlerFunc{
		"element":  (&ElementHandler{}).UpdateElement,
		"code_set": (&CodeSetHandler{}).UpdateCodeSet,
		"glossary": (&GlossaryHandler{}).UpdateGlossary,
		"metric":   (&MetricHandler{}).UpdateMetric,
		"document": (&DocumentHandler{}).UpdateDocument,
	} {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			router.PUT("/resource/:id", handler)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/resource/1", strings.NewReader(`{"version":1,"scope_type":"tenant_common","steward_id":1}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
