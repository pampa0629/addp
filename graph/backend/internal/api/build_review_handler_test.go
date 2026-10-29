package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/graph/internal/config"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReviewHTTPScopesAndBatchFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err = db.AutoMigrate(&models.TenantAdmissionLock{}, &models.GraphOperation{}, &models.BuildExecutionControl{}, &models.KnowledgeGraph{}, &models.BuildTask{}, &models.ReviewItem{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.TenantAdmissionLock{TenantID: 7}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.KnowledgeGraph{ID: 11, TenantID: 7, EngineID: 1, Name: "review", IndexManagement: models.GraphIndexesExternal}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.BuildTask{ID: 1, TenantID: 7, GraphID: 11, Name: "review", Status: models.BuildStatusSuccess}).Error; err != nil {
		t.Fatal(err)
	}

	for _, item := range []models.ReviewItem{
		{ID: 1, TaskID: 1, TenantID: 7, GraphID: 11, Content: []byte(`{}`), Status: models.ReviewStatusPending},
		{ID: 2, TenantID: 7, GraphID: 12, Content: []byte(`{}`), Status: models.ReviewStatusPending},
		{ID: 3, TenantID: 8, GraphID: 11, Content: []byte(`{}`), Status: models.ReviewStatusPending},
		{ID: 4, TaskID: 1, TenantID: 7, GraphID: 11, Content: []byte(`{}`), Status: models.ReviewStatusApproved},
	} {
		if err = db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := service.NewBuildService(repository.NewBuildRepository(db), nil, nil, nil, nil, nil, nil, nil, "", nil)
	auth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer reviewer": {"graph.review.read", "graph.review.approve", "graph.review.reject", "graph.review.update"},
		"Bearer reader":   {"graph.review.read"},
	})
	defer auth.Close()
	router := SetupRouter(&config.Config{SystemServiceURL: auth.URL}, nil, nil, nil, NewBuildHandler(svc), nil, nil, nil, modulelifecycle.NewStandalone("graph"))
	tests := []struct {
		name, method, path, body, token string
		status                          int
	}{
		{"cross graph approve", "POST", "/graphs/11/review/2/approve", "", "reviewer", 404},
		{"cross graph reject", "POST", "/graphs/11/review/2/reject", "", "reviewer", 404},
		{"cross graph modify", "PUT", "/graphs/11/review/2", `{"final_content":{"name":"foreign"}}`, "reviewer", 404},
		{"cross tenant", "POST", "/graphs/11/review/3/reject", "", "reviewer", 404},
		{"missing", "POST", "/graphs/11/review/999/reject", "", "reviewer", 404},
		{"malformed graph", "POST", "/graphs/no/review/1/reject", "", "reviewer", 400},
		{"zero item", "POST", "/graphs/11/review/0/reject", "", "reviewer", 400},
		{"negative item", "POST", "/graphs/11/review/-1/reject", "", "reviewer", 400},
		{"overflow item", "POST", "/graphs/11/review/18446744073709551616/reject", "", "reviewer", 400},
		{"already processed", "POST", "/graphs/11/review/4/reject", "", "reviewer", 409},
		{"batch foreign graph", "POST", "/graphs/11/review/batch/reject", `{"ids":[1,2]}`, "reviewer", 404},
		{"batch foreign tenant", "POST", "/graphs/11/review/batch/approve", `{"ids":[1,3]}`, "reviewer", 404},
		{"empty batch", "POST", "/graphs/11/review/batch/reject", `{"ids":[]}`, "reviewer", 400},
		{"duplicate batch", "POST", "/graphs/11/review/batch/reject", `{"ids":[1,1]}`, "reviewer", 400},
		{"zero batch member", "POST", "/graphs/11/review/batch/reject", `{"ids":[1,0]}`, "reviewer", 400},
		{"reader cannot approve", "POST", "/graphs/11/review/1/approve", "", "reader", 403},
		{"reader cannot batch reject", "POST", "/graphs/11/review/batch/reject", `{"ids":[1]}`, "reader", 403},
		{"batch continues after processed item", "POST", "/graphs/11/review/batch/reject", `{"ids":[4,1]}`, "reviewer", 500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1/graph"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept-Language", "en")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var stored models.ReviewItem
			if err := db.First(&stored, 1).Error; err != nil {
				t.Fatal(err)
			}
			want := models.ReviewStatusPending
			if tc.name == "batch continues after processed item" {
				want = models.ReviewStatusRejected
			}
			if stored.Status != want {
				t.Fatalf("unexpected own item mutation: %+v", stored)
			}
		})
	}
	for _, id := range []uint{2, 3} {
		var stored models.ReviewItem
		if err := db.First(&stored, id).Error; err != nil || stored.Status != models.ReviewStatusPending {
			t.Fatalf("foreign item changed: %+v %v", stored, err)
		}
	}
}

func TestReviewErrorsAreLocalizedAndDoNotExposeNativeDetails(t *testing.T) {
	for _, lang := range []string{"zh-cn", "en"} {
		for _, tc := range []struct {
			cause  error
			status int
		}{
			{commonAPI.ErrBadRequest, 400}, {commonAPI.ErrConflict, 409}, {gorm.ErrRecordNotFound, 404},
			{errors.Join(service.ErrReviewBatchFailed, commonAPI.ErrConflict, gorm.ErrRecordNotFound), 500},
			{errors.New("native database failure"), 500},
		} {
			t.Run(fmt.Sprintf("%s/%d/%v", lang, tc.status, tc.cause), func(t *testing.T) {
				router := gin.New()
				router.Use(commoni18n.I18nMiddleware())
				router.GET("/", func(c *gin.Context) { reviewOperationError(c, fmt.Errorf("password=secret: %w", tc.cause)) })
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Accept-Language", lang)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				body := w.Body.String()
				if w.Code != tc.status || strings.Contains(body, "secret") || strings.Contains(body, "native") || strings.Contains(body, "graph.review.") || strings.Contains(body, "common.") {
					t.Fatalf("unsafe or untranslated: %d %s", w.Code, body)
				}
				if errors.Is(tc.cause, service.ErrReviewBatchFailed) {
					want := "保留"
					if lang == "en" {
						want = "preserved"
					}
					if !strings.Contains(body, want) {
						t.Fatalf("lost partial outcome: %s", body)
					}
				}
			})
		}
	}
}
