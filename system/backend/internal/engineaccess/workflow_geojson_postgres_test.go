package engineaccess

import (
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/execution"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func exerciseWorkflowGeoJSONAccess(t *testing.T, db *gorm.DB, service *Service, tenant int64, user userProvenance, facts authorizationfacts.AuthorizationFacts) {
	t.Run("GeoJSON manual child source access", func(t *testing.T) {
		actor, err := iam.NewRepository(db).GetPrincipal(t.Context(), user.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		expires := now.Add(time.Minute)
		by := int(user.PrincipalID)
		owner := "workflow-geojson-fixture"
		running := func(module, kind, source string) *execution.TaskExecution {
			token := uuid.NewString()
			return &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant), Module: module, TaskType: kind, Source: source, Status: "running", ExecutionBoundary: "bounded", TriggerType: "manual", Attempt: 1, MaxAttempts: 1, StartedAt: &now, LeaseOwner: &owner, LeaseToken: &token, LeaseExpiresAt: &expires, TriggeredBy: &by, ActorPrincipalID: &user.PrincipalID, ActorTenantMembershipID: &user.MembershipID, IssuedAuthorizationVersion: &actor.AuthorizationVersion}
		}
		parent := running("orchestrator", "orchestration", "orchestrator")
		if err := db.Create(parent).Error; err != nil {
			t.Fatal(err)
		}
		child := running("develop", "workflow", "orchestrator")
		child.ParentExecutionID = &parent.ExecutionID
		child.ExecutionConfig = map[string]interface{}{"content": map[string]interface{}{"workflow_definition": map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "load", "operator": "load", "params": map[string]interface{}{"locator": fmt.Sprintf("addp://engine/%d/path/models/scene.geojson?type=object", facts.EngineID)}}}}}}
		auth := iam.ExecutionAuthorization{ExecutionID: uuid.MustParse(child.ExecutionID), Audience: "develop", TenantID: tenant, SourceType: "user", ActorPrincipalID: user.PrincipalID, TenantMembershipID: user.MembershipID, IssuedAuthorizationVersion: actor.AuthorizationVersion, ExpiresAt: expires, CreatedAt: now}
		if err := db.Create(&auth).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO system.execution_authorization_engine_accesses(authorization_id,engine_id,effects) VALUES (?,?,ARRAY['read'])", auth.ID, facts.EngineID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&auth).Update("sealed_at", now).Error; err != nil {
			t.Fatal(err)
		}
		child.ExecutionAuthorizationID = &auth.ID
		child.AuthorizationExpiresAt = &expires
		if err := db.Create(child).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			db.Where("execution_id IN ?", []string{child.ExecutionID, parent.ExecutionID}).Delete(&execution.TaskExecution{})
			db.Delete(&iam.ExecutionAuthorization{}, auth.ID)
		})
		copy := *service
		copy.WithExecutionEngineSnapshot(func(e *models.Engine) (*models.Engine, error) { return e, nil })
		req := client.WorkflowGeoJSONAccessRequest{ExecutionID: child.ExecutionID, Attempt: 1, LeaseToken: *child.LeaseToken, NodeID: "load"}
		if got, err := copy.CheckWorkflowGeoJSONAccess(t.Context(), tenant, req); err != nil || got == nil {
			t.Fatalf("actual child rejected: %v", err)
		}
		wrong := req
		wrong.NodeID = "unbound"
		if got, err := copy.CheckWorkflowGeoJSONAccess(t.Context(), tenant, wrong); err == nil || got != nil {
			t.Fatal("unbound source accepted")
		}
		wrong = req
		wrong.LeaseToken = uuid.NewString()
		if got, err := copy.CheckWorkflowGeoJSONAccess(t.Context(), tenant, wrong); err == nil || got != nil {
			t.Fatal("wrong lease accepted")
		}
		if err := db.Model(&auth).Updates(map[string]interface{}{"revoked_at": now, "revoked_reason": "test revocation"}).Error; err != nil {
			t.Fatal(err)
		}
		if got, err := copy.CheckWorkflowGeoJSONAccess(t.Context(), tenant, req); err == nil || got != nil {
			t.Fatal("revoked authorization accepted")
		}
	})
}
