package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/transfer/internal/models"
)

func TestCDCTaskListFiltersBeforePaginationAndTenantScope(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	svc := NewTaskService(db, nil, nil)
	for i := 0; i < 7; i++ {
		tenant := uint(7)
		config := validPostgreSQLCDCTaskConfig()
		if i == 3 {
			tenant = 8
		}
		if i == 4 {
			config = validContinuousTaskConfig()
		}
		if i == 5 {
			config = validTableTransferTaskConfig()
		}
		task := models.TransferTask{TenantID: tenant, Name: "fixture", TaskType: "sync", Config: config, Status: models.TaskStatusIdle}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		if i == 6 {
			if err := db.Delete(&task).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	seen := map[uint]bool{}
	for page := 1; page <= 3; page++ {
		rows, total, err := svc.ListTasks(context.Background(), 7, &models.ListTasksRequest{ChangeDetection: "cdc", RuntimeBoundary: "continuous", Page: page, PageSize: 1})
		if err != nil || total != 3 || len(rows) != 1 || seen[rows[0].ID] {
			t.Fatalf("page=%d total=%d rows=%v err=%v", page, total, rows, err)
		}
		seen[rows[0].ID] = true
	}
	for _, request := range []models.ListTasksRequest{{ChangeDetection: "other"}, {ChangeDetection: "cdc", RuntimeBoundary: "bounded"}} {
		if _, _, err := svc.ListTasks(context.Background(), 7, &request); !errors.Is(err, ErrInvalidTaskConfig) {
			t.Fatalf("invalid filter accepted: %v", err)
		}
	}
}
