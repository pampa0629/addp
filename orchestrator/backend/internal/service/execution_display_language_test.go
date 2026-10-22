package service

import (
	"context"
	"testing"

	execution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
)

func TestOrchestrationExecutionFreezesRequestLanguageBeforePersistence(t *testing.T) {
	for _, tc := range []struct{ name, language, trigger, want string }{
		{"english", "en", "manual", "en"},
		{"chinese", "zh-cn", "manual", "zh-cn"},
		{"scheduled_default", "", "scheduled", "zh-cn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newOrchestratorExecutionServiceTestDB(t)
			if err := db.Create(&models.Orchestration{ID: 11, TenantID: 7, Name: "Language", Steps: testSteps(), EditorLayout: commonModels.JSONMap{}}).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if tc.language != "" {
				var err error
				ctx, err = commoni18n.WithLanguage(ctx, tc.language)
				if err != nil {
					t.Fatal(err)
				}
			}
			service := NewExecutionService(db)
			item, err := service.CreateExecutionWithContext(ctx, 11, 7, tc.trigger, "orchestrator", nil, ExecutionActor{9, 19, 3})
			if err != nil {
				t.Fatal(err)
			}
			stored, err := service.GetExecution(context.Background(), uint(item.ID), 7)
			if err != nil {
				t.Fatal(err)
			}
			got, err := execution.DisplayLanguage(stored)
			if err != nil || got != tc.want {
				t.Fatalf("persisted language=%q err=%v, want %q", got, err, tc.want)
			}
			child := &execution.TaskExecution{TenantID: 7, ExecutionID: "child", ParentExecutionID: &stored.ExecutionID}
			if err := execution.InheritDisplayLanguage(child, stored); err != nil {
				t.Fatalf("Develop language inheritance: %v", err)
			}
		})
	}
}

func TestOrchestrationChildInheritsFrozenLanguageAndRejectsInvalidParentLanguage(t *testing.T) {
	for _, tc := range []struct {
		name           string
		value          interface{}
		present, valid bool
	}{
		{"english", "en", true, true},
		{"chinese", "zh-cn", true, true},
		{"missing", nil, false, false},
		{"null", nil, true, false},
		{"integer", 1, true, false},
		{"noncanonical", "EN", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newOrchestratorExecutionServiceTestDB(t)
			if err := db.Create(&models.Orchestration{ID: 11, TenantID: 7, Name: "Child", Steps: testSteps(), EditorLayout: commonModels.JSONMap{}}).Error; err != nil {
				t.Fatal(err)
			}
			principal, membership, version := int64(9), int64(19), int64(3)
			triggered := 9
			parent := &execution.TaskExecution{TenantID: 7, ExecutionID: "11111111-1111-4111-8111-111111111111", Module: "orchestrator", TaskType: "orchestration", Source: "orchestrator", Status: "running", TriggerType: "manual", TriggeredBy: &triggered, ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, ExecutionConfig: commonModels.JSONMap{}}
			if tc.present {
				parent.ExecutionConfig[execution.DisplayLanguageKey] = tc.value
			}
			if err := execution.NewTaskExecutionRepository(db).Create(context.Background(), parent); err != nil {
				t.Fatal(err)
			}
			ctx, err := commoni18n.WithLanguage(context.Background(), "en")
			if err != nil {
				t.Fatal(err)
			}
			item, err := NewExecutionService(db).CreateExecutionWithContext(ctx, 11, 7, "manual", "orchestrator", &parent.ExecutionID, ExecutionActor{})
			if !tc.valid {
				if err == nil {
					t.Fatal("invalid parent language admitted")
				}
				var count int64
				if err := db.Model(&execution.TaskExecution{}).Where("parent_execution_id = ?", parent.ExecutionID).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("child count=%d err=%v", count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := NewExecutionService(db).GetExecution(context.Background(), uint(item.ID), 7)
			if err != nil {
				t.Fatal(err)
			}
			lang, err := execution.DisplayLanguage(stored)
			if err != nil || lang != tc.value {
				t.Fatalf("inherited language=%q err=%v, want %v", lang, err, tc.value)
			}
		})
	}
}

func TestReadExecutionPlanValidatesCommonLanguageWithoutRelaxingPlan(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		value                   interface{}
		present, unknown, valid bool
	}{
		{"english", "en", true, false, true},
		{"missing", nil, false, false, false},
		{"null", nil, true, false, false},
		{"integer", 1, true, false, false},
		{"noncanonical", "EN", true, false, false},
		{"unknown_plan_field", "en", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := freezeExecutionPlan(testSteps())
			if err != nil {
				t.Fatal(err)
			}
			if tc.present {
				config[execution.DisplayLanguageKey] = tc.value
			}
			if tc.unknown {
				config["unexpected"] = true
			}
			_, err = readExecutionPlan(config)
			if (err == nil) != tc.valid {
				t.Fatalf("read error=%v valid=%t", err, tc.valid)
			}
			if tc.present {
				if value, present := config[execution.DisplayLanguageKey]; !present || value != tc.value {
					t.Fatal("read mutated persisted language")
				}
			}
		})
	}
}
