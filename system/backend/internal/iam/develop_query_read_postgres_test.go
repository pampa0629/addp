package iam

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Shares the existing EA PostgreSQL fixture and gate. This proves EA/actor/
// claim qualification, not the separate Meta/Grant or native execution proof.
func testDevelopQueryReadObservation(t *testing.T, ctx context.Context, db *gorm.DB, issued *IssuedExecutionAuthorization, engineID int64, token string) {
	t.Run("Develop query observation current actor and lease", func(t *testing.T) {
		set, _ := plugin.NewQueryReadSet()
		var version int64
		if err := db.Table("system.engines").Select("version").Where("id=?", engineID).Scan(&version).Error; err != nil {
			t.Fatal(err)
		}
		base := client.DevelopQueryReadRequest{AuthorizationID: strconv.FormatInt(issued.ID, 10), ExecutionID: issued.ExecutionID.String(), EngineID: strconv.FormatInt(engineID, 10), EngineVersion: strconv.FormatInt(version, 10), ReadSet: set, SampleCredential: token}
		for _, scenario := range []string{"sample", "no-grant-observation", "sample-other-user", "execution", "wrong-attempt", "wrong-lease", "lease-changed-after-Meta", "sample-cannot-bypass-existing-execution", "expired-source-boundary"} {
			t.Run(scenario, func(t *testing.T) {
				tx := db.Begin()
				if tx.Error != nil {
					t.Fatal(tx.Error)
				}
				defer tx.Rollback()
				s, err := NewExecutionAuthorizationService(NewRepository(tx))
				if err != nil {
					t.Fatal(err)
				}
				request := base
				lease := uuid.New()
				asynchronous := scenario == "execution" || scenario == "wrong-attempt" || scenario == "wrong-lease" || scenario == "lease-changed-after-Meta" || scenario == "sample-cannot-bypass-existing-execution"
				if asynchronous {
					err = tx.Exec(`INSERT INTO common.task_executions (execution_id,execution_authorization_id,tenant_id,module,task_type,status,attempt,lease_token,lease_expires_at,actor_principal_id,actor_tenant_membership_id,issued_authorization_version) VALUES (?,?,?,'develop','query','running',1,?,clock_timestamp()+interval '1 minute',?,?,?)`, issued.ExecutionID, issued.ID, issued.TenantID, lease, issued.ActorPrincipalID, issued.TenantMembershipID, issued.IssuedAuthorizationVersion).Error
					if err != nil {
						t.Fatal(err)
					}
					if scenario != "sample-cannot-bypass-existing-execution" {
						request.SampleCredential = ""
						request.Attempt = 1
						request.LeaseToken = lease.String()
					}
				}
				switch scenario {
				case "sample-other-user":
					request.SampleCredential = "addp_at_missing"
				case "wrong-attempt":
					request.Attempt = 2
				case "wrong-lease":
					request.LeaseToken = uuid.NewString()
				}
				prepared, verified := 0, 0
				result, err := s.ObserveDevelopQueryRead(ctx, issued.TenantID, request, func(context.Context, *ExecutionAuthorization) (DevelopQueryReadVerifier, error) {
					prepared++
					if scenario == "lease-changed-after-Meta" {
						if err := tx.Exec("UPDATE common.task_executions SET lease_token=? WHERE execution_id=?", uuid.New(), issued.ExecutionID).Error; err != nil {
							t.Fatal(err)
						}
					}
					return func(ctx context.Context, db *gorm.DB, a *ExecutionAuthorization) (*client.DevelopQueryReadObservation, error) {
						verified++
						if scenario == "no-grant-observation" {
							return nil, commonapi.ErrForbidden
						}
						var now time.Time
						if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
							return nil, err
						}
						out := &client.DevelopQueryReadObservation{ObservedAt: now}
						if scenario == "expired-source-boundary" {
							out.ExpiresAt = now.Add(-time.Second)
						}
						return out, nil
					}, nil
				})
				allowed := scenario == "sample" || scenario == "execution"
				if allowed {
					if err != nil || result == nil || !result.ExpiresAt.After(result.ObservedAt) || prepared != 1 || verified != 1 {
						t.Fatalf("observation=%+v err=%v prepare=%d verify=%d", result, err, prepared, verified)
					}
				} else if err == nil || result != nil {
					t.Fatalf("invalid source/claim admitted: %+v %v", result, err)
				}
				if scenario == "lease-changed-after-Meta" && (prepared != 1 || verified != 0) {
					t.Fatal("stale lease reached source rules", prepared, verified)
				}
				if scenario == "no-grant-observation" && !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatal(err)
				}
			})
		}
	})
}
