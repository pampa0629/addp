package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func exerciseScanTaskFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	task := models.ScanTask{TenantID: 7, EngineID: 9, Name: "display", Scope: models.JSONMap{"type": "engine"},
		Parameters: models.JSONMap{"scan_depth": "basic", "force": false, "password": "must-not-leak"}}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatUint(uint64(task.ID), 10)
	s := NewScanExecutionService(db, nil, nil, nil)
	facts, err := s.GetScanTaskFacts(context.Background(), 7, id)
	if err != nil || facts.Validate() != nil || facts.EngineID != 9 || facts.TaskID != id || facts.ScanDepth != "basic" || facts.ScopeKind != "engine" {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	raw, _ := json.Marshal(facts)
	for _, secret := range []string{"password", "must-not-leak", "execution_id", "connection", "scope\"", "parameters"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private field leaked", secret)
		}
	}
	if err := db.Model(&task).Updates(map[string]any{"name": "renamed", "schedule": "0 2 * * *"}).Error; err != nil {
		t.Fatal(err)
	}
	renamed, err := s.GetScanTaskFacts(context.Background(), 7, id)
	if err != nil || renamed.TaskDefinitionDigest != facts.TaskDefinitionDigest {
		t.Fatal("display/schedule changed configuration", err)
	}
	if _, err := s.GetScanTaskFacts(context.Background(), 8, id); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("wrong tenant visible", err)
	}
	for _, bad := range []string{"0", "01", "-1", "1/other", "9223372036854775808"} {
		if _, err := s.GetScanTaskFacts(context.Background(), 7, bad); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatal("bad identity accepted", bad, err)
		}
	}
	for _, test := range []struct {
		name          string
		scope, params models.JSONMap
		unavailable   bool
	}{
		{"deep fact is not approval", models.JSONMap{"type": "engine"}, models.JSONMap{"scan_depth": "deep", "force": true}, false},
		{"read options", models.JSONMap{"type": "engine"}, models.JSONMap{"scan_depth": "basic", "encoding": "GBK", "has_header": false}, false},
		{"path never becomes whole", models.JSONMap{"type": "catalog_path", "catalog_paths": []string{"outdoor"}}, models.JSONMap{"scan_depth": "basic"}, true},
		{"empty path never becomes whole", models.JSONMap{"type": "catalog_path", "catalog_paths": []string{}}, models.JSONMap{"scan_depth": "basic"}, true},
		{"mixed engine selector", models.JSONMap{"type": "engine", "ref_groups": []any{map[string]any{"primary": "other"}}}, models.JSONMap{"scan_depth": "basic"}, true},
		{"malformed force", models.JSONMap{"type": "engine"}, models.JSONMap{"scan_depth": "basic", "force": "false"}, true},
		{"malformed depth", models.JSONMap{"type": "engine"}, models.JSONMap{"scan_depth": nil}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := db.Model(&task).Updates(map[string]any{"scope": test.scope, "parameters": test.params}).Error; err != nil {
				t.Fatal(err)
			}
			got, err := s.GetScanTaskFacts(context.Background(), 7, id)
			if test.unavailable {
				if !errors.Is(err, ErrScanPlanFactsUnavailable) {
					t.Fatalf("facts=%+v err=%v", got, err)
				}
				return
			}
			if err != nil || got.TaskDefinitionDigest == facts.TaskDefinitionDigest {
				t.Fatalf("configuration change lost: %+v %v", got, err)
			}
		})
	}
	if err := db.Delete(&task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetScanTaskFacts(context.Background(), 7, id); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("deleted task visible", err)
	}
	var count int64
	if err := db.Model(&execution.TaskExecution{}).Where("source_task_id = ?", id).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("approval facts created an execution", err)
	}
}

func TestScanTaskFactsBeforeExecution(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	createScanTaskTable(t, db)
	createTaskExecutionTable(t, db)
	exerciseScanTaskFacts(t, db)
}
