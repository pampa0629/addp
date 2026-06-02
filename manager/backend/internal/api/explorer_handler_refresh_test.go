package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func TestRefreshNodeResponseOnlyReturnsLocatorAndRun(t *testing.T) {
	t.Parallel()

	treeRequests := 0
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/engines/9":
			_, _ = w.Write([]byte(`{"id":9,"name":"Business MinIO","engine_type":"s3","connection_info":{},"is_active":true}`))
		case "/api/v1/meta/scan/run/manual":
			if got := r.Header.Get("Authorization"); got != "Bearer user-token" {
				http.Error(w, fmt.Sprintf("Authorization = %q", got), http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":11,"tenant_id":1,"execution_id":"run-1","module":"meta","task_type":"scan","status":"pending","trigger_type":"manual"}`))
		case "/api/v1/meta/engines/9/tree":
			treeRequests++
			http.Error(w, "tree should not be loaded during refresh submission", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer metaServer.Close()

	explorerService := service.NewExplorerService(
		client.NewSystemClient(metaServer.URL, "test-token"),
		client.NewMetaClientWithInternalKey(metaServer.URL, "internal-key"),
		nil,
	)
	handler := NewExplorerHandler(explorerService, nil, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/tree/:engine_id/refresh", handler.RefreshNode)

	locator := "addp://engine/9/path/?type=service"
	req := httptest.NewRequest(http.MethodPost, "/tree/9/refresh?locator="+url.QueryEscape(locator), nil)
	req.Header.Set("Authorization", "Bearer user-token")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if treeRequests != 0 {
		t.Fatalf("treeRequests = %d, want 0", treeRequests)
	}

	var body struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data["locator"] != locator {
		t.Fatalf("locator = %#v, want %q", body.Data["locator"], locator)
	}
	if _, ok := body.Data["run"].(map[string]interface{}); !ok {
		t.Fatalf("run = %#v, want object", body.Data["run"])
	}
	if _, ok := body.Data["node"]; ok {
		t.Fatalf("response data must not include node: %#v", body.Data)
	}
}
