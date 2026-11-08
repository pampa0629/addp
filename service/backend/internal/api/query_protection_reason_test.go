package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/service/internal/protection"
	"github.com/gin-gonic/gin"
)

func TestQueryProtectionTypedReasonsUseSafeRequestLanguage(t *testing.T) {
	cases := []struct {
		cause  error
		zh, en string
	}{
		{dataprotection.ErrNotReady, "尚未就绪", "not ready"},
		{dataprotection.ErrExpired, "已到期", "have expired"},
		{dataprotection.ErrNotYetValid, "尚未生效", "not yet effective"},
		{dataprotection.ErrSchemaMismatch, "数据结构与保护规则不匹配", "structure does not match"},
		{dataprotection.ErrLineageUnverified, "无法确认查询输出", "could not be verified"},
		{plugin.ErrQueryOutputLineageUnresolved, "无法确认查询输出", "could not be verified"},
		{dataprotection.ErrComputedOutputUnsupported, "计算或组合输出", "computed or combined"},
		{dataprotection.ErrOperationUnsupported, "当前操作暂不支持", "operation does not yet support"},
		{plugin.ErrQueryOutputTransformUnsupported, "当前操作暂不支持", "operation does not yet support"},
	}
	for _, tc := range cases {
		for _, lang := range []string{"zh-cn", "en"} {
			t.Run(lang+"/"+tc.en, func(t *testing.T) {
				cause := fmt.Errorf("private-sentinel sql=credential %w: %w", protection.ErrRequired, tc.cause)
				router := gin.New()
				router.Use(commoni18n.I18nMiddleware())
				router.GET("/", func(c *gin.Context) { writeQueryExecutionError(c, cause) })
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Accept-Language", lang)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				var body map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				want := tc.zh
				if lang == "en" {
					want = tc.en
				}
				if w.Code != 403 || body["error_code"] != "data_protection_required" || len(body) != 2 || !strings.Contains(body["error"].(string), want) || strings.Contains(w.Body.String(), "private-sentinel") || strings.Contains(w.Body.String(), "credential") {
					t.Fatalf("unsafe or imprecise refusal: %d %s", w.Code, w.Body)
				}
				if queryExecutionErrorCode(cause) != "data_protection_required" {
					t.Fatal("audit category changed")
				}
			})
		}
	}
}

func TestQueryProtectionMessageDoesNotInferTypedReasonFromText(t *testing.T) {
	for _, cause := range []error{protection.ErrRequired, dataprotection.ErrDenied, fmt.Errorf("%w: %s", protection.ErrRequired, dataprotection.ErrExpired.Error()), errors.Join(protection.ErrRequired, errors.New("protection_not_ready"))} {
		if got := queryProtectionMessageID(cause); got != "service.err.data_protection_required" {
			t.Fatalf("untyped detail became a reason: %s", got)
		}
	}
}
