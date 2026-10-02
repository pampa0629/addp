package api

import (
	"errors"
	execution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecutionFailureLocalizationPreservesStoredFacts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Accept-Language", "en")
	commoni18n.I18nMiddleware()(c)
	code := "orchestrator.execution.dispatch_uncertain"
	item := &execution.TaskExecution{ErrorDetails: commonModels.JSONMap{"code": code, "message": "stored message"}, Metadata: commonModels.JSONMap{"step_results": map[string]interface{}{"step": map[string]interface{}{"error_code": code, "error": "stored step", "result": map[string]interface{}{"execution_id": "child"}}}}}
	projected := localizeExecutionFailure(c, item)
	if !strings.Contains(projected.ErrorDetails["message"].(string), "No automatic replay") || item.ErrorDetails["message"] != "stored message" {
		t.Fatalf("failure=%v source=%v", projected.ErrorDetails, item.ErrorDetails)
	}
	step := projected.Metadata["step_results"].(map[string]interface{})["step"].(commonModels.JSONMap)
	if !strings.Contains(step["error"].(string), "No automatic replay") || step["result"].(map[string]interface{})["execution_id"] != "child" {
		t.Fatalf("step=%v", step)
	}
	stored := item.Metadata["step_results"].(map[string]interface{})["step"].(map[string]interface{})
	if stored["error"] != "stored step" {
		t.Fatal("localization modified stored metadata")
	}
}

func TestAdmissionFailureUsesLocalizedSafeError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/", nil)
	c.Request.Header.Set("Accept-Language", "en")
	commoni18n.I18nMiddleware()(c)
	respondExecutionAdmissionError(c, errors.New("password=private SQL=select*"))
	if recorder.Code != 500 || strings.Contains(recorder.Body.String(), "private") || !strings.Contains(recorder.Body.String(), "Could not create this execution") {
		t.Fatalf("admission response=%d %s", recorder.Code, recorder.Body.String())
	}
}
