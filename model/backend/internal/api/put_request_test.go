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
		{name: "entity", body: `{"name":"Order","domain_id":null,"description":""}`, target: &models.UpdateEntityRequest{}},
		{name: "entity attribute", body: `{"name":"ID","column_name":"id","data_type":"bigint","element_id":null,"is_pk":false,"nullable":false,"description":"","sort_order":0}`, target: &models.UpdateEntityAttributeRequest{}},
		{name: "entity relation", body: `{"relation_type":"one_to_many","name":"","description":""}`, target: &models.UpdateEntityRelationRequest{}},
		{name: "logical table", body: `{"name":"Order","domain_id":null,"entity_id":null,"table_type":"entity","layer":"dwd","grain_description":"","scd_type":0,"description":"","materialization":{}}`, target: &models.UpdateLogicalTableRequest{}},
		{name: "logical field", body: `{"name":"ID","column_name":"id","data_type":"bigint","element_id":null,"length":null,"nullable":false,"is_pk":false,"is_partition":false,"default_value":"","description":"","sort_order":0,"field_role":"regular","hierarchy_id":null,"hierarchy_level":null}`, target: &models.UpdateLogicalFieldRequest{}},
		{name: "dw layer", body: `{"layer_name":"DWD","description":"","naming_rule":"","quality_sla":null,"sort_order":0}`, target: &models.UpdateDWLayerRequest{}},
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
