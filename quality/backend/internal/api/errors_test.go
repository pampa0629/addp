package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	commonAPI "github.com/addp/common/api"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestRespondQualityServiceErrorContract(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "bad request", err: commonAPI.ErrBadRequest, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "not found", err: gorm.ErrRecordNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "conflict", err: commonAPI.ErrConflict, wantStatus: http.StatusConflict, wantCode: "resource_conflict"},
		{name: "internal", err: errors.New("password=secret database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			respondQualityServiceError(c, tt.err, qualityi18n.MsgCheckTaskNotFound, qualityi18n.MsgInternal)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			var body qualityErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.ErrorCode != tt.wantCode || body.Error == "" {
				t.Fatalf("response = %#v, want code %q and non-empty message", body, tt.wantCode)
			}
			if body.Error == tt.err.Error() {
				t.Fatalf("response leaked internal error: %q", body.Error)
			}
		})
	}
}
