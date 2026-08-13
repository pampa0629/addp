package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestModelListHandlersRejectInvalidQueryParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		path    string
		handler gin.HandlerFunc
	}{
		{name: "entity domain type", path: "/entities?domain_id=abc", handler: NewEntityHandler(nil).ListEntities},
		{name: "entity domain zero", path: "/entities?domain_id=0", handler: NewEntityHandler(nil).ListEntities},
		{name: "entity page type", path: "/entities?page=abc", handler: NewEntityHandler(nil).ListEntities},
		{name: "entity page zero", path: "/entities?page=0", handler: NewEntityHandler(nil).ListEntities},
		{name: "entity page size too large", path: "/entities?page_size=101", handler: NewEntityHandler(nil).ListEntities},
		{name: "entity invalid status", path: "/entities?status=archived", handler: NewEntityHandler(nil).ListEntities},
		{name: "entity repeated status", path: "/entities?status=draft&status=approved", handler: NewEntityHandler(nil).ListEntities},
		{name: "entity repeated keyword", path: "/entities?keyword=order&keyword=customer", handler: NewEntityHandler(nil).ListEntities},
		{name: "logical table domain negative", path: "/logical-tables?domain_id=-1", handler: NewLogicalTableHandler(nil).ListLogicalTables},
		{name: "logical table page negative", path: "/logical-tables?page=-1", handler: NewLogicalTableHandler(nil).ListLogicalTables},
		{name: "logical table page size type", path: "/logical-tables?page_size=abc", handler: NewLogicalTableHandler(nil).ListLogicalTables},
		{name: "logical table page size zero", path: "/logical-tables?page_size=0", handler: NewLogicalTableHandler(nil).ListLogicalTables},
		{name: "logical table invalid status", path: "/logical-tables?status=materialized", handler: NewLogicalTableHandler(nil).ListLogicalTables},
		{name: "logical table invalid table type", path: "/logical-tables?table_type=aggregate", handler: NewLogicalTableHandler(nil).ListLogicalTables},
		{name: "logical table repeated layer", path: "/logical-tables?layer=dwd&layer=ads", handler: NewLogicalTableHandler(nil).ListLogicalTables},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodGet, tt.path, nil)

			tt.handler(context)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			var body map[string]interface{}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v; body=%s", err, response.Body.String())
			}
			if body["error_code"] != "invalid_request" || body["error"] == "" {
				t.Fatalf("body = %#v, want invalid_request", body)
			}
		})
	}
}

func TestParseOptionalEnum(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		allowed []string
		want    string
		wantErr bool
	}{
		{name: "absent", allowed: []string{"draft", "approved"}},
		{name: "empty", values: []string{""}, allowed: []string{"draft", "approved"}},
		{name: "allowed", values: []string{"draft"}, allowed: []string{"draft", "approved"}, want: "draft"},
		{name: "unknown", values: []string{"archived"}, allowed: []string{"draft", "approved"}, wantErr: true},
		{name: "repeated", values: []string{"draft", "approved"}, allowed: []string{"draft", "approved"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOptionalEnum(tt.values, tt.allowed...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseOptionalEnum() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("parseOptionalEnum() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseModelListQueryAcceptsDefaultsAndValidValues(t *testing.T) {
	tests := []struct {
		name     string
		rawQuery string
		domainID *int64
		page     int
		pageSize int
	}{
		{name: "defaults", page: 1, pageSize: 20},
		{name: "explicit", rawQuery: "domain_id=7&page=3&page_size=100", domainID: int64Pointer(7), page: 3, pageSize: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/?"+tt.rawQuery, nil)
			query, err := parseModelListQuery(request.URL.Query())
			if err != nil {
				t.Fatalf("parseModelListQuery: %v", err)
			}
			if query.Page != tt.page || query.PageSize != tt.pageSize {
				t.Fatalf("query = %+v, want page=%d page_size=%d", query, tt.page, tt.pageSize)
			}
			if tt.domainID == nil && query.DomainID != nil || tt.domainID != nil && (query.DomainID == nil || *query.DomainID != *tt.domainID) {
				t.Fatalf("domain_id = %v, want %v", query.DomainID, tt.domainID)
			}
		})
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}
