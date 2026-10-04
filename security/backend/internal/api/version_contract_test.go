package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/security/internal/models"
	"github.com/gin-gonic/gin"
)

// Use literal JSON, rather than marshaling the DTO under test: otherwise a
// wrong ,string tag makes both the test client and the server share the bug.
func TestSecurityVersionedCommandsBindJSONIntegers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, command := range []struct {
		name, field, otherFields string
		newRequest               func() any
	}{
		{"update policy", "version", `,"effect":"mask","rationale":"validation"`, func() any { return &models.UpdateProtectionPolicyRequest{} }},
		{"revoke policy", "version", `,"rationale":"validation"`, func() any { return &models.RevokeProtectionPolicyRequest{} }},
		{"revise assessment", "version", `,"sensitive_data_type_id":"1","security_grade_id":"2","rationale":"validation"`, func() any { return &models.AssessmentRevisionRequest{} }},
		{"revoke assessment", "version", `,"rationale":"validation"`, func() any { return &models.RevokeAssessmentRequest{} }},
		{"manual assessment", "enrollment_version", `,"enrollment_id":"target","component_key":"userInfo.phone","sensitive_data_type_id":"1","security_grade_id":"2","rationale":"validation"`, func() any { return &models.CreateManualAssessmentRequest{} }},
	} {
		t.Run(command.name, func(t *testing.T) {
			for _, value := range []struct {
				json   string
				status int
			}{
				{"3", http.StatusOK}, {`"3"`, http.StatusBadRequest}, {"0", http.StatusBadRequest}, {"null", http.StatusBadRequest}, {"3.5", http.StatusBadRequest},
			} {
				router := gin.New()
				router.POST("/command", func(c *gin.Context) {
					request := command.newRequest()
					if c.ShouldBindJSON(request) != nil {
						c.Status(http.StatusBadRequest)
						return
					}
					c.JSON(http.StatusOK, request)
				})
				body := fmt.Sprintf(`{"%s":%s%s}`, command.field, value.json, command.otherFields)
				request := httptest.NewRequest(http.MethodPost, "/command", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if recorder.Code != value.status {
					t.Errorf("%s=%s: HTTP %d, want %d", command.field, value.json, recorder.Code, value.status)
				}
			}
		})
	}
}
