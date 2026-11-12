package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/production"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *ScanExecutionService) WithProductionRegistry(registry *production.Registry) *ScanExecutionService {
	s.productionRegistry = registry
	return s
}

// Backend admission and Worker requalification must use the same owner catalog.
func NewProductionRegistry(transfer *client.TransferClient, develop *client.DevelopClient) (*production.Registry, error) {
	return production.NewRegistry(
		production.Owner{Module: "transfer", ClientID: "addp-transfer", ReceiptPermission: "transfer.production_receipt.read", ExecutionPermission: "transfer.task.execute", ReadReceipt: transfer.ReadCommittedProduction},
		production.Owner{Module: "develop", ClientID: "addp-develop", ReceiptPermission: "develop.production_receipt.read", ExecutionPermission: "develop.task.execute", ReadReceipt: develop.ReadCommittedProduction},
	)
}

// The registry is trusted composition; callers supply only an original result
// reference. Source IO is outside Meta transactions, and recovery is read-only.
func (s *ScanExecutionService) EnsureProductionBasicScan(ctx context.Context, tenant uint, ref production.Reference) (*client.MetaProductionIdentification, error) {
	if tenant == 0 || ref.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.db == nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	if _, tx := s.db.Statement.ConnPool.(gorm.TxCommitter); tx {
		return nil, commonapi.ErrBadRequest
	}
	if prior, err := s.ReadProductionIdentification(ctx, tenant, ref); err == nil {
		return prior, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if s.productionRegistry == nil || s.engineService == nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	if _, ok := s.productionRegistry.Resolve(ref.Owner, "addp-"+ref.Owner); !ok {
		return nil, commonapi.ErrForbidden
	}
	receipt, err := s.productionRegistry.Read(ctx, tenant, ref)
	if err != nil {
		return nil, err
	}
	if receipt.Attempt != 1 || receipt.RetryOfExecutionID != nil {
		return nil, commonapi.ErrConflict
	}
	for _, object := range receipt.Objects {
		if !object.Created {
			return nil, commonapi.ErrConflict
		}
	}
	engine, err := s.engineService.GetResourceByIDWithContext(ctx, receipt.Primary.EngineID, tenant)
	if err != nil {
		return nil, err
	}
	if engine.Version != receipt.TargetEngineVersion || engine.LifecycleState != "active" {
		return nil, commonapi.ErrConflict
	}
	req, err := productionScanRequest(*receipt)
	if err != nil {
		return nil, err
	}
	var user uint
	if receipt.Origin != nil {
		user = uint(receipt.Origin.PrincipalID)
	}
	_, err = s.createManualRun(ctx, tenant, user, req, func(tx *gorm.DB, child *execution.TaskExecution) (*execution.TaskExecution, error) {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", fmt.Sprintf("meta.initial_production|%d|%s|%s|%s", tenant, ref.Owner, ref.ExecutionID, ref.ResultID)).Error; err != nil {
				return nil, err
			}
		}
		if prior, err := findProductionBasicScan(tx, tenant, ref); err == nil {
			return prior, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		fresh := true
		for _, object := range receipt.Objects {
			_, known, err := logicalItemAlreadyKnown(tx, tenant, object.Target)
			if err != nil {
				return nil, err
			}
			fresh = fresh && !known
		}
		child.ParentExecutionID = &receipt.Reference.ExecutionID
		if receipt.Origin != nil {
			child.ActorPrincipalID = &receipt.Origin.PrincipalID
			child.ActorTenantMembershipID = &receipt.Origin.MembershipID
			child.IssuedAuthorizationVersion = &receipt.Origin.AuthorizationVersion
		} else {
			child.TriggeredBy = nil
		}
		child.ExecutionConfig["initial_production_receipt"] = receipt
		child.ExecutionConfig["initial_production_new_candidate"] = fresh
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return s.ReadProductionIdentification(ctx, tenant, ref)
}

// One independently committed DataItem: native table or an exact file group.
// File roles are discovered by the existing format processor, not by the caller.
func productionScanRequest(r production.Receipt) (*models.ScanRequest, error) {
	req := &models.ScanRequest{EngineID: r.Primary.EngineID, ScanDepth: "basic", Force: true, Source: r.Reference.Owner}
	switch r.Primary.Segments[0].Term {
	case plugin.FileCatalogModel().RootTerm:
		refs := make([]models.ScanRef, 0, len(r.Objects))
		for _, object := range r.Objects {
			refs = append(refs, models.ScanRef{Path: object.Target.StringPath(), Primary: reflect.DeepEqual(object.Target, r.Primary), Required: true})
		}
		req.RefGroups = []models.ScanRefGroup{{Primary: r.Primary.StringPath(), Refs: refs}}
	case plugin.EngineCatalogTermServer:
		if len(r.Objects) != 1 || len(r.Primary.Segments) != 3 || r.Primary.Segments[2].Kind != plugin.EngineCatalogKindTable {
			return nil, commonapi.ErrConflict
		}
		parts := []string{r.Primary.Segments[1].Name, r.Primary.Segments[2].Name}
		req.Targets = []string{(&resourcetree.ResourceLocator{EngineID: r.Primary.EngineID, Type: resourcetree.TypeTable, Path: parts}).ToURI()}
	default:
		return nil, commonapi.ErrConflict
	}
	return req, nil
}
func findProductionBasicScan(db *gorm.DB, tenant uint, ref production.Reference) (*execution.TaskExecution, error) {
	return productionScanRecord(db.Where("tenant_id=? AND module=? AND task_type=? AND parent_execution_id=? AND execution_config->>'scan_depth'='basic' AND execution_config->'initial_production_receipt'->'reference'->>'owner'=? AND execution_config->'initial_production_receipt'->'reference'->>'result_id'=?", tenant, execution.ModuleMeta, execution.TaskTypeScan, ref.ExecutionID, ref.Owner, ref.ResultID).Order("id"))
}
func frozenProductionReceipt(run *execution.TaskExecution) (*production.Receipt, error) {
	raw, err := json.Marshal(run.ExecutionConfig["initial_production_receipt"])
	if err != nil {
		return nil, commonapi.ErrConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var receipt production.Receipt
	if decoder.Decode(&receipt) != nil || receipt.Validate() != nil || receipt.Attempt != 1 || receipt.RetryOfExecutionID != nil || receipt.TenantID != int64(run.TenantID) || run.Source != receipt.Reference.Owner || run.SourceTaskID != nil || run.ParentExecutionID == nil || *run.ParentExecutionID != receipt.Reference.ExecutionID || run.TriggerType != "manual" || run.ExecutionConfig["force"] != true {
		return nil, commonapi.ErrConflict
	}
	dependency, exists := run.ExecutionConfig["production_basic_execution_id"]
	switch run.ExecutionConfig["scan_depth"] {
	case "basic":
		if exists {
			return nil, commonapi.ErrConflict
		}
	case "deep":
		id, ok := dependency.(string)
		parsed, err := uuid.Parse(id)
		if !ok || err != nil || parsed == uuid.Nil || parsed.String() != id || id == run.ExecutionID || id == receipt.Reference.ExecutionID {
			return nil, commonapi.ErrConflict
		}
	default:
		return nil, commonapi.ErrConflict
	}
	if fmt.Sprint(run.ExecutionConfig["engine_id"]) != strconv.FormatUint(uint64(receipt.Primary.EngineID), 10) || run.ExecutionConfig["engine_version"] != strconv.FormatInt(receipt.TargetEngineVersion, 10) {
		return nil, commonapi.ErrConflict
	}
	if receipt.Origin == nil {
		if run.ActorPrincipalID != nil || run.ActorTenantMembershipID != nil || run.IssuedAuthorizationVersion != nil || run.TriggeredBy != nil {
			return nil, commonapi.ErrConflict
		}
	} else {
		if run.ActorPrincipalID == nil || *run.ActorPrincipalID != receipt.Origin.PrincipalID || run.ActorTenantMembershipID == nil || *run.ActorTenantMembershipID != receipt.Origin.MembershipID || run.IssuedAuthorizationVersion == nil || *run.IssuedAuthorizationVersion != receipt.Origin.AuthorizationVersion || run.TriggeredBy == nil || int64(*run.TriggeredBy) != receipt.Origin.PrincipalID {
			return nil, commonapi.ErrConflict
		}
	}
	profile, ok := run.ExecutionConfig["engine_type"].(string)
	if !ok || run.ExecutionConfig["storage_type"] != scantask.NormalizeStorageType(profile) {
		return nil, commonapi.ErrConflict
	}
	enginePlugin, err := plugin.Get(profile)
	if err != nil {
		return nil, commonapi.ErrConflict
	}
	model := scanflow.EngineCatalogModelForPlugin(enginePlugin)
	if model == nil || model.RootTerm != receipt.Primary.Segments[0].Term {
		return nil, commonapi.ErrConflict
	}
	if model.RootTerm == plugin.EngineCatalogTermServer && !scanPlanNativePathMatchesModel(receipt.Primary, *model) {
		return nil, commonapi.ErrConflict
	}
	expected, err := productionScanRequest(receipt)
	if err != nil {
		return nil, err
	}
	rawTargets, _ := json.Marshal(run.ExecutionConfig["targets"])
	var targets []string
	if json.Unmarshal(rawTargets, &targets) != nil || len(targets) != len(expected.Targets) {
		return nil, commonapi.ErrConflict
	}
	for i, target := range targets {
		if target != expected.Targets[i] {
			return nil, commonapi.ErrConflict
		}
	}
	rawGroups, _ := json.Marshal(run.ExecutionConfig["ref_groups"])
	var groups []models.ScanRefGroup
	groupsDecoder := json.NewDecoder(bytes.NewReader(rawGroups))
	groupsDecoder.DisallowUnknownFields()
	if groupsDecoder.Decode(&groups) != nil || len(groups) != len(expected.RefGroups) {
		return nil, commonapi.ErrConflict
	}
	for i, group := range groups {
		if !reflect.DeepEqual(group, expected.RefGroups[i]) {
			return nil, commonapi.ErrConflict
		}
	}
	if run.ExecutionConfig["source"] != receipt.Reference.Owner {
		return nil, commonapi.ErrConflict
	}
	for _, key := range []string{"catalog_paths", "item_id", "node_id", "task_definition_digest", "planned_run_at", "encoding", "has_header"} {
		if value, exists := run.ExecutionConfig[key]; exists && value != nil {
			if key == "catalog_paths" && (fmt.Sprint(value) == "[]") {
				continue
			}
			return nil, commonapi.ErrConflict
		}
	}
	return &receipt, nil
}
func (s *ScanExecutionService) ReadProductionIdentification(ctx context.Context, tenant uint, ref production.Reference) (*client.MetaProductionIdentification, error) {
	if tenant == 0 || ref.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.db == nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	var result *client.MetaProductionIdentification
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := findProductionBasicScan(tx, tenant, ref)
		if err != nil {
			return err
		}
		result, err = s.productionIdentification(ctx, tx, run, tenant, ref)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
func (s *ScanExecutionService) productionIdentification(ctx context.Context, db *gorm.DB, run *execution.TaskExecution, tenant uint, ref production.Reference) (*client.MetaProductionIdentification, error) {
	receipt, err := frozenProductionReceipt(run)
	if err != nil || receipt.Reference != ref || receipt.TenantID != int64(tenant) {
		return nil, commonapi.ErrConflict
	}
	result := &client.MetaProductionIdentification{Reference: ref, TenantID: int64(tenant), ScanExecutionID: run.ExecutionID, Status: run.Status, EngineVersion: receipt.TargetEngineVersion}
	if run.Status == execution.ExecutionStatusSuccess {
		fingerprint, err := authorizationfacts.PrimaryCandidateFingerprint(receipt.Primary)
		if err != nil {
			return nil, err
		}
		var item models.MetaItem
		if err := db.WithContext(ctx).Where("tenant_id=? AND engine_id=? AND fingerprint=?", tenant, receipt.Primary.EngineID, fingerprint).Take(&item).Error; err != nil {
			return nil, err
		}
		lookup, err := loadMetadataIdentityNodes(db.WithContext(ctx), tenant, []models.MetaItem{item})
		if err != nil {
			return nil, err
		}
		facts, err := projectDataItemAuthorizationFacts(item, run.UpdatedAt, lookup)
		if err != nil {
			return nil, err
		}
		binding, err := facts.ApprovedBinding()
		if err != nil {
			return nil, err
		}
		if receipt.ProveNewBoundary(binding) != nil {
			return nil, commonapi.ErrConflict
		}
		candidate, ok := run.ExecutionConfig["initial_production_new_candidate"].(bool)
		if !ok {
			return nil, commonapi.ErrConflict
		}
		result.Binding = &binding
		result.LogicalItemState = "existing"
		if candidate && !item.CreatedAt.Before(run.CreatedAt) {
			result.LogicalItemState = "new"
		}
	}
	return result, result.Validate()
}
