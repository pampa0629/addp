package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

func TestQueryExportErrorsSeparateCapabilityPermissionProtectionAndOutage(t *testing.T) {
	for _, item := range []struct {
		err    error
		status int
		code   string
	}{
		{service.ErrQueryExportUnsupported, 422, "query_export_unsupported"},
		{service.ErrQueryExportUnavailable, 503, "query_export_unavailable"},
		{commonAPI.ErrForbidden, 403, ""},
		{commonAPI.ErrUnauthorized, 401, ""},
		{dataprotection.ErrRoleOptionUnavailable, 409, ""},
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		queryExportCreateError(ctx, item.err)
		var body map[string]any
		if json.Unmarshal(recorder.Body.Bytes(), &body) != nil || recorder.Code != item.status {
			t.Fatal("export refusal collapsed", item.err, recorder.Code)
		}
		if item.code != "" && body["error_code"] != item.code {
			t.Fatal("wrong reason", body)
		}
	}
}
