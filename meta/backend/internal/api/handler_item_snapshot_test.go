package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestItemSnapshotReadSeparatesMissingAndDatabaseFailure(t *testing.T) {
	for _, kind := range []string{"found", "missing", "database_failure", "other_tenant"} {
		for _, byID := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/id=%v", kind, byID), func(t *testing.T) {
				db := metatest.OpenMetadataDB(t)
				now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
				var fields []datatype.FieldInfo
				for i := 0; i < 260; i++ {
					fields = append(fields, datatype.FieldInfo{Name: fmt.Sprintf("field_%d", i), Type: datatype.FieldTypeString, NativeType: "text", Nullable: true, DefaultExpression: "private expression"})
				}
				item := models.MetaItem{ID: 19, TenantID: 7, EngineID: 4, NodeID: 1, ItemType: "table", Name: "source", FullName: "synthetic.source", Fingerprint: "synthetic-fingerprint", ScannedAt: &now, ScannedDepth: models.ScannedDepthBasic, Attributes: models.JSONMap{"type_info": map[string]interface{}{"table": datatype.TableInfoPayload(&datatype.TableInfo{Fields: fields})}}}
				if kind == "other_tenant" {
					item.TenantID = 8
				}
				if kind != "missing" {
					if err := db.Create(&item).Error; err != nil {
						t.Fatal(err)
					}
				}
				queries := 0
				if kind == "database_failure" {
					if err := db.Callback().Query().Before("gorm:query").Register("snapshot_database_failure", func(tx *gorm.DB) { queries++; _ = tx.AddError(errors.New("private database credentials")) }); err != nil {
						t.Fatal(err)
					}
				}
				handler := &Handler{metadataQueryService: service.NewMetadataQueryService(db)}
				router := gin.New()
				router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
					if err := commonauth.SetAuthContextForGin(c, authtest.NewTenantUserAuthContext("7", "91", []string{"meta.catalog.read"})); err != nil {
						t.Fatal(err)
					}
				})
				router.GET("/api/v1/meta/items/by-catalog-path", handler.QueryItemByCatalogPath)
				router.GET("/api/v1/meta/items/:item_id", handler.GetItemByID)
				server := httptest.NewServer(router)
				defer server.Close()
				path := "/api/v1/meta/items/by-catalog-path?engine_id=4&catalog_path=synthetic.source"
				if byID {
					path = "/api/v1/meta/items/19"
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				want := http.StatusOK
				if kind == "missing" || kind == "other_tenant" {
					want = http.StatusNotFound
				}
				if kind == "database_failure" {
					want = http.StatusInternalServerError
				}
				if response.Code != want || strings.Contains(response.Body.String(), "private database credentials") {
					t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body)
				}
				if kind == "database_failure" && queries != 1 {
					t.Fatalf("queries=%d; failure must not continue candidates", queries)
				}
				if kind == "found" {
					var decoded commonmodels.MetaItem
					if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
						t.Fatal(err)
					}
					tokenSource := commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "synthetic-service", nil })
					client := commonclient.NewMetaClient(server.URL, tokenSource)
					id := uint(0)
					if byID {
						id = 19
					}
					snapshot, err := client.GetItemSnapshot(context.Background(), 7, 4, id, "synthetic.source")
					if err != nil {
						t.Fatal(err)
					}
					table := snapshot.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})
					if len(table["fields"].([]interface{})) != 260 || snapshot.ScannedAt == nil || snapshot.TenantID != 7 {
						t.Fatalf("incomplete SDK snapshot: %+v", snapshot)
					}
				}
			})
		}
	}
}
