package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/gorm"
)

func TestReviewCommandsRequireGraphAndTenant(t *testing.T) {
	for _, action := range []string{"approve", "reject", "modify"} {
		for _, scope := range []string{"graph", "tenant"} {
			t.Run(action+"/"+scope, func(t *testing.T) {
				db := setupGraphCleanupTestDB(t)
				sqlDB, _ := db.DB()
				defer sqlDB.Close()
				item := models.ReviewItem{TenantID: 7, GraphID: 11, ItemType: "entity", Content: []byte(`{`), Status: models.ReviewStatusPending}
				if err := db.Create(&item).Error; err != nil {
					t.Fatal(err)
				}
				svc := &BuildService{buildRepo: repository.NewBuildRepository(db)}
				graphID, tenantID := uint(11), uint(7)
				if scope == "graph" {
					graphID = 12
				} else {
					tenantID = 8
				}
				var err error
				switch action {
				case "approve":
					err = svc.ApproveReviewItem(t.Context(), graphID, item.ID, tenantID, 9)
				case "reject":
					err = svc.RejectReviewItem(t.Context(), graphID, item.ID, tenantID, 9)
				case "modify":
					err = svc.ModifyAndApproveReviewItem(t.Context(), graphID, item.ID, tenantID, 9, map[string]interface{}{})
				}
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("cross-scope error=%v", err)
				}
				var stored models.ReviewItem
				if err = db.First(&stored, item.ID).Error; err != nil || stored.Status != models.ReviewStatusPending || stored.ReviewedBy != nil || len(stored.FinalContent) != 0 {
					t.Fatalf("changed foreign item: %+v %v", stored, err)
				}
			})
		}
	}
}

func TestBatchReviewPreflightsEntireRequest(t *testing.T) {
	for _, action := range []string{"approve", "reject"} {
		for _, invalid := range []string{"foreign graph", "foreign tenant", "missing", "empty", "duplicate", "zero", "oversized", "action"} {
			t.Run(action+"/"+invalid, func(t *testing.T) {
				db := setupGraphCleanupTestDB(t)
				sqlDB, _ := db.DB()
				defer sqlDB.Close()
				own := models.ReviewItem{TenantID: 7, GraphID: 11, Content: []byte(`{`), Status: models.ReviewStatusPending}
				foreign := models.ReviewItem{TenantID: 7, GraphID: 12, Content: []byte(`{`), Status: models.ReviewStatusPending}
				if invalid == "foreign tenant" {
					foreign.TenantID = 8
					foreign.GraphID = 11
				}
				for _, item := range []*models.ReviewItem{&own, &foreign} {
					if err := db.Create(item).Error; err != nil {
						t.Fatal(err)
					}
				}
				ids := []uint{own.ID, foreign.ID}
				requestAction := action
				want := gorm.ErrRecordNotFound
				switch invalid {
				case "missing":
					ids[1] = 999
				case "empty":
					ids = nil
					want = commonAPI.ErrBadRequest
				case "duplicate":
					ids[1] = own.ID
					want = commonAPI.ErrBadRequest
				case "zero":
					ids[1] = 0
					want = commonAPI.ErrBadRequest
				case "oversized":
					ids = make([]uint, 101)
					want = commonAPI.ErrBadRequest
				case "action":
					requestAction = "ignore"
					want = commonAPI.ErrBadRequest
				}
				svc := &BuildService{buildRepo: repository.NewBuildRepository(db)}
				err := svc.BatchReview(t.Context(), 11, 7, 9, ids, requestAction)
				if !errors.Is(err, want) {
					t.Fatalf("error=%v want=%v", err, want)
				}
				var changed int64
				if err = db.Model(&models.ReviewItem{}).Where("status <> ?", models.ReviewStatusPending).Count(&changed).Error; err != nil || changed != 0 {
					t.Fatalf("preflight had side effects: %d %v", changed, err)
				}
			})
		}
	}
}

func TestBatchReviewReportsFailuresAndContinues(t *testing.T) {
	for _, failure := range []string{"processed", "save"} {
		t.Run(failure, func(t *testing.T) {
			db := setupGraphCleanupTestDB(t)
			sqlDB, _ := db.DB()
			defer sqlDB.Close()
			items := make([]models.ReviewItem, 3)
			for i := range items {
				items[i] = models.ReviewItem{TenantID: 7, GraphID: 11, Content: []byte(`{}`), Status: models.ReviewStatusPending}
				if i == 1 && failure == "processed" {
					items[i].Status = models.ReviewStatusApproved
				}
				if err := db.Create(&items[i]).Error; err != nil {
					t.Fatal(err)
				}
			}
			sentinel := errors.New("fixture review persistence failure")
			if failure == "save" {
				if err := db.Callback().Update().Before("gorm:update").Register("fixture:review-failure", func(tx *gorm.DB) {
					if item, ok := tx.Statement.Dest.(*models.ReviewItem); ok && item.ID == items[1].ID {
						tx.AddError(sentinel)
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			svc := &BuildService{buildRepo: repository.NewBuildRepository(db)}
			err := svc.BatchReview(context.Background(), 11, 7, 9, []uint{items[0].ID, items[1].ID, items[2].ID}, "reject")
			if !errors.Is(err, ErrReviewBatchFailed) || !strings.Contains(err.Error(), fmt.Sprint(items[1].ID)) {
				t.Fatalf("lost batch failure: %v", err)
			}
			if failure == "save" && !errors.Is(err, sentinel) {
				t.Fatalf("lost persistence cause: %v", err)
			}
			if failure == "processed" && !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("lost conflict cause: %v", err)
			}
			for i := range items {
				var stored models.ReviewItem
				if e := db.First(&stored, items[i].ID).Error; e != nil {
					t.Fatal(e)
				}
				want := models.ReviewStatusRejected
				if i == 1 {
					want = items[i].Status
				}
				if stored.Status != want {
					t.Fatalf("item %d status=%s want=%s", i, stored.Status, want)
				}
			}
		})
	}
}

func TestProcessedReviewCommandsReturnConflictBeforeGraphWrites(t *testing.T) {
	for _, action := range []string{"approve", "reject", "modify"} {
		t.Run(action, func(t *testing.T) {
			db := setupGraphCleanupTestDB(t)
			sqlDB, _ := db.DB()
			defer sqlDB.Close()
			item := models.ReviewItem{TenantID: 7, GraphID: 11, Content: []byte(`{}`), Status: models.ReviewStatusApproved}
			if err := db.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
			svc := &BuildService{buildRepo: repository.NewBuildRepository(db)}
			var err error
			switch action {
			case "approve":
				err = svc.ApproveReviewItem(t.Context(), 11, item.ID, 7, 9)
			case "reject":
				err = svc.RejectReviewItem(t.Context(), 11, item.ID, 7, 9)
			case "modify":
				err = svc.ModifyAndApproveReviewItem(t.Context(), 11, item.ID, 7, 9, map[string]interface{}{})
			}
			if !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
