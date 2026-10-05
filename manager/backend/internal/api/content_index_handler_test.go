package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection/projectionstore"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type contentIndexerTestService struct{ err error }

func (s contentIndexerTestService) Enabled() bool { return true }
func (s contentIndexerTestService) UpsertContentDocument(context.Context, uint, commonClient.ManagerContentDocument) error {
	return s.err
}
func (s contentIndexerTestService) DeleteContentDocuments(context.Context, uint, service.ContentDocumentDeleteScope) error {
	return s.err
}

func TestContentIndexHandlerReportsDistinctProtectionAndDeliveryOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"completed", nil, http.StatusNoContent, ""},
		{"version_changed", projectionstore.ErrVersionChanged, http.StatusConflict, "protection_version_changed"},
		{"rule_required", managerprotection.ErrRequired, http.StatusConflict, "manager_content_protection_required"},
		{"unresolved", errors.New("private external task details"), http.StatusServiceUnavailable, "manager_content_index_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.PUT("/content/:document_id", NewContentIndexHandler(contentIndexerTestService{tc.err}).UpsertDocument)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/content/item", strings.NewReader(`{"document_id":"item","payload_kind":"technical_metadata","engine_id":9,"data_item_type":"table","name":"persons"}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if tc.code == "" {
				if response.Body.Len() != 0 {
					t.Fatal("204 response had a body")
				}
				return
			}
			var body struct {
				Code string `json:"error_code"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Code != tc.code {
				t.Fatalf("error contract: %s %v", response.Body.String(), err)
			}
			if strings.Contains(response.Body.String(), "private external task details") {
				t.Fatal("external error details escaped")
			}
		})
	}
}
