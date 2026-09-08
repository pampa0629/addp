package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

func TestRespondErrorMapsExpiredAccessRequestToConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	respondError(context, service.ErrProtectionAccessRequestExpired)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["error_code"] != "protection_access_request_expired" {
		t.Fatalf("error_code = %q", response["error_code"])
	}
	if response["error"] != "原值访问申请已超过截止时间，请申请人重新提交" {
		t.Fatalf("error = %q", response["error"])
	}
}
