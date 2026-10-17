package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
)

type scheduleAuthorizerFunc func(context.Context, string, commonClient.TaskAuthorizationSubjectRequest) (*commonClient.TaskAuthorizationSubject, error)

func (f scheduleAuthorizerFunc) AuthorizeTaskSubject(ctx context.Context, token string, r commonClient.TaskAuthorizationSubjectRequest) (*commonClient.TaskAuthorizationSubject, error) {
	return f(ctx, token, r)
}
func scheduleTestContext(principal string) context.Context {
	return WithTaskAuthorizationToken(commonExecution.WithUserOrigin(context.Background(), authtest.NewTenantUserAuthContext("7", principal, nil)), "addp_at_schedule_test")
}
func validScheduleTestSubject(ctx context.Context, token string, r commonClient.TaskAuthorizationSubjectRequest) (*commonClient.TaskAuthorizationSubject, error) {
	actor := commonExecution.TaskExecution{TenantID: 7}
	if err := commonExecution.ApplyUserOrigin(ctx, &actor); err != nil {
		return nil, err
	}
	return &commonClient.TaskAuthorizationSubject{ID: "81", OwnerModule: r.OwnerModule, TaskType: r.TaskType, TaskRef: r.TaskRef, DefinitionHash: r.DefinitionHash, TenantID: "7", PrincipalID: strconv.FormatInt(*actor.ActorPrincipalID, 10), TenantMembershipID: strconv.FormatInt(*actor.ActorTenantMembershipID, 10), AuthorizationVersion: strconv.FormatInt(*actor.IssuedAuthorizationVersion, 10), AuthorizedAt: time.Now().UTC()}, nil
}

func TestScheduleDefinitionReplacePauseAndResumeUsesCurrentUser(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	svc := NewTaskService(db, nil, nil)
	calls := 0
	svc.SetTaskSubjectAuthorizer(scheduleAuthorizerFunc(func(ctx context.Context, token string, r commonClient.TaskAuthorizationSubjectRequest) (*commonClient.TaskAuthorizationSubject, error) {
		calls++
		if token != "addp_at_schedule_test" || r.OwnerModule != "transfer" || r.TaskType != "sync" {
			t.Fatal("wrong synchronous credential or binding")
		}
		return validScheduleTestSubject(ctx, token, r)
	}))
	task, err := svc.CreateTask(scheduleTestContext("9"), &models.CreateTaskRequest{Name: "scheduled", TaskType: "sync", Config: validTableTransferTaskConfig(), Schedule: "0 */5 * * * *", Enabled: boolPtr(true)}, 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	oldHash := *task.AuthorizationDefinitionHash
	name := "modified"
	task, err = svc.UpdateTask(scheduleTestContext("10"), task.ID, 7, &models.UpdateTaskRequest{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if *task.AuthorizationPrincipalID != 10 || *task.AuthorizationDefinitionHash == oldHash || *task.CreatedBy != 9 {
		t.Fatal("definition retained creator or previous subject")
	}
	if err := svc.PauseTask(context.Background(), task.ID, 7); err != nil {
		t.Fatal(err)
	}
	task, err = svc.GetTask(context.Background(), task.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if task.Enabled || task.NextRunAt != nil || task.AuthorizationSubjectID != nil || task.AuthorizationPrincipalID != nil {
		t.Fatal("pause retained usable authorization")
	}
	if _, err := svc.ResumeTask(scheduleTestContext("11"), task.ID, 7, 11); err != nil {
		t.Fatal(err)
	}
	task, err = svc.GetTask(context.Background(), task.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !task.Enabled || task.NextRunAt == nil || *task.AuthorizationPrincipalID != 11 || calls != 3 {
		t.Fatal("resume did not bind current User")
	}
	encoded, _ := json.Marshal(task)
	if strings.Contains(string(encoded), "authorization_") || strings.Contains(string(encoded), "addp_at_") {
		t.Fatal("private schedule authority leaked")
	}
	var count int64
	db.Model(&commonExecution.TaskExecution{}).Count(&count)
	if count != 0 {
		t.Fatal("definition write created an execution")
	}
}

func TestScheduleRejectsUnverifiedAndMismatchedSubjectsWithoutDefinitionWrite(t *testing.T) {
	for _, scenario := range []string{"no_origin", "no_token", "unavailable", "rejected", "tenant", "principal", "membership", "version", "hash", "subject", "nil"} {
		t.Run(scenario, func(t *testing.T) {
			db := newTransferTaskServiceTestDB(t)
			svc := NewTaskService(db, nil, nil)
			ctx := scheduleTestContext("9")
			if scenario == "no_origin" {
				ctx = WithTaskAuthorizationToken(context.Background(), "addp_at_schedule_test")
			}
			if scenario == "no_token" {
				ctx = transferTestUserOrigin()
			}
			if scenario != "unavailable" {
				svc.SetTaskSubjectAuthorizer(scheduleAuthorizerFunc(func(ctx context.Context, token string, r commonClient.TaskAuthorizationSubjectRequest) (*commonClient.TaskAuthorizationSubject, error) {
					subject, err := validScheduleTestSubject(ctx, token, r)
					if err != nil {
						return nil, err
					}
					switch scenario {
					case "rejected":
						return nil, commonAPI.ErrForbidden
					case "tenant":
						subject.TenantID = "8"
					case "principal":
						subject.PrincipalID = "10"
					case "membership":
						subject.TenantMembershipID = "10"
					case "version":
						subject.AuthorizationVersion = "2"
					case "hash":
						subject.DefinitionHash = "stale"
					case "subject":
						subject.ID = "01"
					case "nil":
						return nil, nil
					}
					return subject, nil
				}))
			}
			_, err := svc.CreateTask(ctx, &models.CreateTaskRequest{Name: "rejected", TaskType: "sync", Config: validTableTransferTaskConfig(), Schedule: "0 */5 * * * *", Enabled: boolPtr(true)}, 7, 9)
			if err == nil {
				t.Fatal("invalid scheduled source accepted")
			}
			var count int64
			db.Model(&models.TransferTask{}).Count(&count)
			if count != 0 {
				t.Fatal("rejected task persisted")
			}
		})
	}
}

func TestRejectedScheduleUpdatePreservesCommittedDefinitionAndSubject(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	svc := NewTaskService(db, nil, nil)
	svc.SetTaskSubjectAuthorizer(scheduleAuthorizerFunc(validScheduleTestSubject))
	task, err := svc.CreateTask(scheduleTestContext("9"), &models.CreateTaskRequest{Name: "original", TaskType: "sync", Config: validTableTransferTaskConfig(), Schedule: "0 */5 * * * *", Enabled: boolPtr(true)}, 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetTaskSubjectAuthorizer(scheduleAuthorizerFunc(func(context.Context, string, commonClient.TaskAuthorizationSubjectRequest) (*commonClient.TaskAuthorizationSubject, error) {
		return nil, errors.New("denied")
	}))
	name := "rejected"
	if _, err := svc.UpdateTask(scheduleTestContext("10"), task.ID, 7, &models.UpdateTaskRequest{Name: &name}); err == nil {
		t.Fatal("rejected update accepted")
	}
	var stored models.TransferTask
	db.First(&stored, task.ID)
	if stored.Name != task.Name || *stored.AuthorizationPrincipalID != 9 || *stored.AuthorizationDefinitionHash != *task.AuthorizationDefinitionHash {
		t.Fatal("failed update changed committed authorization")
	}
}
