package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

type transferObjectAuthorizationIssuer interface {
	IssueTransferObject(context.Context, string, client.IssueManagerFileAuthorizationRequest) (*client.IssuedTransferObjectAuthorization, error)
}

func (s *TaskService) SetObjectAuthorizationIssuer(issuer transferObjectAuthorizationIssuer) {
	s.objectAuthorization = issuer
}

// prepareObjectExecution freezes a server-derived source, without modifying the saved definition.
func (s *TaskService) prepareObjectExecution(ctx context.Context, task *models.TransferTask, record *execution.TaskExecution) (bool, error) {
	source, _ := record.ExecutionConfig["source"].(map[string]interface{})
	uri, _ := source["locator"].(string)
	loc, err := resourcetree.ParseURI(uri)
	if err != nil || loc.Type != resourcetree.TypeObject {
		return false, nil
	}
	if s.engineDescriptor == nil {
		return false, ErrTaskAuthorizationUnavailable
	}
	resolver := planner.DescriptorEngineResolver{Context: ctx, TenantID: task.TenantID, Lookup: s.engineDescriptor}
	engine, err := resolver.ResolveEngine(planner.EngineRef{ID: loc.EngineID})
	if err != nil {
		return false, err
	}
	if engine.Type != "rustfs" {
		return false, nil
	}
	denied := fmt.Errorf("%w: RustFS import requires a manual single Parquet source and PostgreSQL target", commonAPI.ErrForbidden)
	if task.Enabled || task.Schedule != "" || record.Source != "transfer" || record.TriggerType != "manual" || record.ParentExecutionID != nil ||
		record.ExecutionBoundary != "bounded" || (loc.ItemID == nil || *loc.ItemID == 0) || len(loc.Path) < 2 || s.objectAuthorization == nil ||
		s.metaClient == nil || engine.EngineVersion <= 0 {
		return false, denied
	}
	item, err := s.metaClient.WithTenantID(task.TenantID).GetItemByID(ctx, *loc.ItemID)
	if err != nil {
		return false, err
	}
	p := plugin.ObjectItemPath(loc.EngineID, loc.Path[0], strings.Join(loc.Path[1:], "/"))
	section, _ := item.Attributes["item"].(map[string]interface{})
	if item.ID != *loc.ItemID || item.EngineID != loc.EngineID || item.ItemType != "object" || item.FullName != p.StringPath() ||
		item.Fingerprint != commonModels.GenerateItemFingerprint(loc.EngineID, p.StringPath()) || section["layout"] != "single" ||
		section["data_type"] != "table" || section["format"] != "parquet" {
		return false, denied
	}
	binding, err := authorizationfacts.NativeCandidateBinding(p)
	if err != nil {
		return false, err
	}
	raw, err := json.Marshal(record.ExecutionConfig)
	if err != nil {
		return false, err
	}
	var frozen commonModels.JSONMap
	if json.Unmarshal(raw, &frozen) != nil {
		return false, denied
	}
	frozen["object_source"] = execution.TransferObjectSource{EngineVersion: strconv.FormatInt(engine.EngineVersion, 10), Binding: binding}
	raw, err = json.Marshal(frozen)
	if err != nil {
		return false, err
	}
	scope, err := execution.NewTransferObjectReadScope(raw)
	if err != nil {
		return false, denied
	}
	if _, err := resolver.ResolveEngine(planner.EngineRef{ID: scope.TargetEngineID, Type: "postgresql"}); err != nil {
		return false, denied
	}
	record.ExecutionConfig = frozen
	record.MaxAttempts = 1
	return true, nil
}

func (s *TaskService) issueObjectExecution(ctx context.Context, record *execution.TaskExecution) error {
	token, _ := ctx.Value(taskAuthorizationTokenKey{}).(string)
	issued, err := s.objectAuthorization.IssueTransferObject(ctx, token, client.IssueManagerFileAuthorizationRequest{ExecutionID: record.ExecutionID, ExpiresIn: 3600})
	if err == nil {
		err = s.taskRepo.BindTransferObjectAuthorization(ctx, record, issued)
	}
	if err != nil {
		if rejectErr := s.taskRepo.RejectObjectAuthorization(ctx, record); rejectErr != nil {
			return fmt.Errorf("%w: reject pending import: %v", err, rejectErr)
		}
		if status, ok := client.SystemAPIStatusCode(err); ok && status == 403 {
			return commonAPI.ErrForbidden
		}
		if status, ok := client.SystemAPIStatusCode(err); ok && status == 401 {
			return commonAPI.ErrUnauthorized
		}
		return err
	}
	return nil
}

func (s *ExecutionEngineService) objectImportSource(ctx context.Context, record *execution.TaskExecution, targetResolver planner.EngineResolver) (planner.EngineResolver, executor.ObjectSourceAuthorizer, error) {
	denied := fmt.Errorf("%w: exact object execution authorization unavailable", executor.ErrProtectionPreparation)
	if record == nil || record.Source != "transfer" || record.SourceTaskID == nil || record.ExecutionAuthorizationID == nil ||
		record.AuthorizationExpiresAt == nil || s.systemRuntime == nil {
		return nil, nil, denied
	}
	lease, ok := execution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != record.ExecutionID || lease.TenantID != record.TenantID || lease.Attempt != record.Attempt {
		return nil, nil, denied
	}
	raw, err := json.Marshal(record.ExecutionConfig)
	if err != nil {
		return nil, nil, denied
	}
	scope, err := execution.NewTransferObjectReadScope(raw)
	if err != nil {
		return nil, nil, denied
	}
	system := s.systemRuntime.WithTenantID(uint(record.TenantID))
	request := client.TransferObjectAccessRequest{ExecutionID: lease.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, SourceReadScope: *scope}
	id := strconv.FormatInt(*record.ExecutionAuthorizationID, 10)
	observed, err := system.CheckTransferObjectAccess(ctx, id, request)
	if err != nil {
		return nil, nil, denied
	}
	source, err := planner.EngineBindingFromEngine(observed.Engine)
	if err != nil {
		return nil, nil, denied
	}
	target, err := targetResolver.ResolveEngine(planner.EngineRef{ID: scope.TargetEngineID, Type: "postgresql"})
	if err != nil {
		return nil, nil, denied
	}
	p := scope.ObjectSource.Binding.NativeTarget
	authorize := func(ctx context.Context, path plugin.EngineCatalogPath) error {
		if !reflect.DeepEqual(path, p) {
			return denied
		}
		if _, err := system.CheckTransferObjectAccess(ctx, id, request); err != nil {
			return denied
		}
		return nil
	}
	return planner.StaticEngineResolver{p.EngineID: source, scope.TargetEngineID: target}, authorize, nil
}
