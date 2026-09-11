package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/security/internal/repository"
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

func TestRespondErrorIncludesStableResourceVersionConflictCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	respondError(context, repository.ErrVersionConflict)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["error_code"] != "resource_version_conflict" {
		t.Fatalf("error_code = %q", response["error_code"])
	}
	if response["error"] != "资源已被其他操作更新，请刷新后重试" {
		t.Fatalf("error = %q", response["error"])
	}
}

func TestRespondErrorIncludesStableNoSupportedFindingsReleaseCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	respondError(context, service.ErrNoSupportedFindingsReleaseUnavailable)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["error_code"] != "no_supported_findings_release_unavailable" {
		t.Fatalf("error_code = %q", response["error_code"])
	}
}

func TestRespondErrorIncludesStableDiscoveryExecutionInProgressCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	respondError(context, service.ErrDiscoveryExecutionInProgress)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["error_code"] != "protection_discovery_execution_in_progress" {
		t.Fatalf("error_code = %q", response["error_code"])
	}
}

func TestRespondErrorIncludesStableLiveEnrollmentCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	respondError(context, service.ErrLiveEnrollmentAlreadyExists)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["error_code"] != "protection_enrollment_already_active" {
		t.Fatalf("error_code = %q", response["error_code"])
	}
}
