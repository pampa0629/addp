package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestItemEncodingRefreshRejectsInvalidEncodingWithStableLocalizedContract(t *testing.T) {
	tokenSource := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "token", nil })
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 9, "tenant_id": 1, "engine_type": "nfs", "lifecycle_state": "active", "connection_status": "online", "connection_info": map[string]interface{}{}})
	}))
	defer system.Close()
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/items/8/refresh" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"upstream-localized-message","error_code":"scan_encoding_invalid"}`))
	}))
	defer meta.Close()
	svc := service.NewMetadataService(nil, client.NewSystemClient(system.URL, tokenSource), client.NewMetaClient(meta.URL, tokenSource), nil, nil)
	handler := NewMetadataHandler(svc)
	for _, lang := range []string{"zh-cn", "en"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Params = gin.Params{{Key: "id", Value: "9"}}
		setTenantAuthContextForTest(c, 1, 7)
		c.Set("addp_lang", lang)
		c.Request = httptest.NewRequest(http.MethodPost, "/engines/9/items/refresh", strings.NewReader(`{"item_id":8,"encoding":"bad"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		handler.RefreshItem(c)
		var body map[string]string
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != 400 || body["error_code"] != "scan_encoding_invalid" || body["error"] == "" || strings.Contains(body["error"], "upstream-localized-message") {
			t.Fatalf("response=%d %#v", recorder.Code, body)
		}
		if lang == "zh-cn" && !strings.Contains(body["error"], "编码") {
			t.Fatal(body)
		}
		if lang == "en" && !strings.Contains(body["error"], "Encoding") {
			t.Fatal(body)
		}
	}
}
