package engineaccess

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func exerciseImageSegmentationAccess(t *testing.T, db *gorm.DB, svc *Service, tenant int64, user userProvenance, facts authorizationfacts.AuthorizationFacts) {
	t.Run("manual child image segmentation access", func(t *testing.T) {
		ctx := t.Context()
		person, err := iam.NewRepository(db).GetPrincipal(ctx, user.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		version := person.AuthorizationVersion
		actor := int(user.PrincipalID)
		now := time.Now().UTC()
		expiry := now.Add(time.Minute)
		owner := "segmentation-postgres-fixture"
		running := func(module, source, kind string) *execution.TaskExecution {
			token := uuid.NewString()
			return &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant),
				Module: module, Source: source, TaskType: kind, TriggeredBy: &actor,
				ActorPrincipalID: &user.PrincipalID, ActorTenantMembershipID: &user.MembershipID, IssuedAuthorizationVersion: &version,
				ExecutionBoundary: "bounded", Status: "running", TriggerType: "manual", Attempt: 1, MaxAttempts: 1,
				StartedAt: &now, LeaseOwner: &owner, LeaseToken: &token, LeaseExpiresAt: &expiry}
		}
		insert := func(run *execution.TaskExecution) {
			t.Helper()
			if err := db.Create(run).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Where("execution_id=?", run.ExecutionID).Delete(&execution.TaskExecution{}).Error; err != nil {
					t.Error(err)
				}
			})
		}
		parent := running("orchestrator", "orchestrator", "orchestration")
		parent.SourceTaskID = execution.NewSourceTaskIDFromUint(11)
		insert(parent)
		child := running("manager", "orchestrator", execution.TaskTypeImageSegmentation)
		child.ParentExecutionID = &parent.ExecutionID
		child.SourceTaskID = execution.NewSourceTaskIDFromUint(12)
		cfg := execution.ImageSegmentationConfig{
			Source: execution.ImageSegmentationSource{EngineID: facts.EngineID, EngineVersion: 1, ItemID: 13,
				Locator:         fmt.Sprintf("addp://engine/%d/path/models/scene.tif?type=object&item_id=13", facts.EngineID),
				ItemFingerprint: facts.ItemFingerprint, Composition: *facts.Boundary.FileComposition},
			Target: execution.ImageSegmentationTarget{EngineID: facts.EngineID, EngineVersion: 1,
				Locator: fmt.Sprintf("addp://engine/%d/path/models/results/buildings.geojson?type=object", facts.EngineID),
				Path:    plugin.ObjectItemPath(facts.EngineID, "models", "results/buildings.geojson")},
			Text: "building", ModelProfileID: "fixture-profile",
		}
		raw, err := json.Marshal(cfg)
		if err != nil || json.Unmarshal(raw, &child.ExecutionConfig) != nil {
			t.Fatal("could not freeze test inputs", err)
		}
		insert(child)
		service := *svc
		service.WithExecutionEngineSnapshot(func(engine *models.Engine) (*models.Engine, error) { return engine, nil })
		req := client.ImageSegmentationAccessRequest{ExecutionID: child.ExecutionID, Attempt: 1, LeaseToken: *child.LeaseToken}
		got, err := service.CheckImageSegmentationAccess(ctx, tenant, req)
		if err != nil || got == nil || got.SourceEngine.ID != facts.EngineID || got.TargetEngine.ID != facts.EngineID {
			t.Fatalf("live manual child rejected: %v", err)
		}
		wrong := req
		wrong.LeaseToken = uuid.NewString()
		if got, err := service.CheckImageSegmentationAccess(ctx, tenant, wrong); got != nil || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("wrong lease accepted: %v", err)
		}
		if got, err := service.CheckImageSegmentationAccess(ctx, tenant+1, req); got != nil || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("wrong tenant accepted: %v", err)
		}
		if err := db.Model(parent).Update("trigger_type", "scheduled").Error; err != nil {
			t.Fatal(err)
		}
		if got, err := service.CheckImageSegmentationAccess(ctx, tenant, req); got != nil || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("scheduled parent accepted: %v", err)
		}
	})
}
