package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/gin-gonic/gin"
)

func TestModelPutRequestsRequireCompleteEditableState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		target interface{}
	}{
		{name: "entity", target: &models.UpdateEntityRequest{}},
		{name: "entity attribute", target: &models.UpdateEntityAttributeRequest{}},
		{name: "entity relation", target: &models.UpdateEntityRelationRequest{}},
		{name: "logical table", target: &models.UpdateLogicalTableRequest{}},
		{name: "logical field", target: &models.UpdateLogicalFieldRequest{}},
		{name: "concept mappings", target: &models.ReplaceConceptMappingsRequest{}},
		{name: "dw layer", target: &models.UpdateDWLayerRequest{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodPut, "/", http.NoBody)
			context.Request.Header.Set("Content-Type", "application/json")
			context.Request.Body = io.NopCloser(strings.NewReader(`{}`))

			if err := context.ShouldBindJSON(tt.target); err == nil {
				t.Fatal("ShouldBindJSON error = nil, want incomplete PUT request rejected")
			}
		})
	}
}

func TestModelPutRequestsAcceptCompleteZeroAndNullableValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		body   string
		target interface{}
	}{
		{name: "entity", body: `{"version":1,"name":"Order","domain_id":null,"description":""}`, target: &models.UpdateEntityRequest{}},
		{name: "entity attribute", body: `{"version":1,"name":"ID","column_name":"id","data_type":"bigint","element_id":null,"is_pk":false,"nullable":false,"description":"","sort_order":0}`, target: &models.UpdateEntityAttributeRequest{}},
		{name: "entity relation", body: `{"version":1,"source_entity":1,"target_entity":2,"relation_type":"one_to_many","name":"","description":""}`, target: &models.UpdateEntityRelationRequest{}},
		{name: "logical table", body: `{"version":1,"name":"Order","domain_id":null,"table_type":"entity","layer":"dwd","grain_description":"","scd_type":0,"description":"","materialization":{}}`, target: &models.UpdateLogicalTableRequest{}},
		{name: "logical field", body: `{"version":1,"name":"ID","column_name":"id","data_type":"bigint","element_id":null,"length":null,"nullable":false,"is_pk":false,"default_value":"","description":"","sort_order":0,"field_role":"regular"}`, target: &models.UpdateLogicalFieldRequest{}},
		{name: "concept mappings", body: `{"version":1,"table_mappings":[],"field_mappings":[],"relation_mappings":[]}`, target: &models.ReplaceConceptMappingsRequest{}},
		{name: "dw layer", body: `{"version":1,"layer_name":"DWD","description":"","naming_rule":"","sort_order":0}`, target: &models.UpdateDWLayerRequest{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(tt.body))
			context.Request.Header.Set("Content-Type", "application/json")

			if err := context.ShouldBindJSON(tt.target); err != nil {
				t.Fatalf("ShouldBindJSON: %v", err)
			}
		})
	}
}

func TestMetricDraftRequestAcceptsGroupedDecimalSumWithoutDistinctFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"version":1,"metric_definition_revision_id":1,"contract":{"operation":"sum_decimal_by_group","group":{"field_id":21,"relation_id":0},"measure":{"field_id":22,"relation_id":0}}}`))
	context.Request.Header.Set("Content-Type", "application/json")
	var request models.SaveMetricImplementationRevisionRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		t.Fatalf("ShouldBindJSON: %v", err)
	}
	if request.Contract.Group == nil || request.Contract.Measure == nil || request.Contract.Group.FieldID != 21 || request.Contract.Measure.FieldID != 22 {
		t.Fatalf("unexpected grouped sum contract: %+v", request.Contract)
	}
}
