package service

import (
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
	"github.com/addp/common/format"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	"gorm.io/gorm"
)

type uploadReceiptReader interface {
	ReadUploadReceipt(context.Context, uint, string) (*client.UploadReceipt, error)
}

func (s *ScanExecutionService) WithUploadReceiptReader(reader uploadReceiptReader) *ScanExecutionService {
	s.uploadReceipts = reader
	return s
}

func (s *ScanExecutionService) EnsureUploadBasicScan(ctx context.Context, tenant uint, id string) (*client.MetaUploadIdentification, error) {
	if s == nil || s.db == nil || s.uploadReceipts == nil || s.engineService == nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	if prior, err := s.ReadUploadIdentification(ctx, tenant, id); err == nil {
		return prior, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	receipt, err := s.uploadReceipts.ReadUploadReceipt(ctx, tenant, id)
	if err != nil {
		return nil, err
	}
	if receipt == nil || receipt.Validate() != nil || receipt.TenantID != int64(tenant) || receipt.UploadID != id {
		return nil, commonapi.ErrForbidden
	}
	engine, err := s.engineService.GetResourceByIDWithContext(ctx, receipt.EngineID, tenant)
	if err != nil {
		return nil, err
	}
	if engine.EngineType != "nfs" || strconv.FormatInt(engine.Version, 10) != receipt.EngineVersion {
		return nil, commonapi.ErrConflict
	}
	paths := make([]string, 0, len(receipt.Writes))
	for _, write := range receipt.Writes {
		paths = append(paths, write.Target.StringPath())
	}
	refs := uploadReceiptRefGroups(paths)
	req := &models.ScanRequest{EngineID: receipt.EngineID, RefGroups: refs, ScanDepth: "basic", Force: true, Source: execution.ModuleManager}
	_, err = s.createManualRun(ctx, tenant, uint(receipt.Uploader.PrincipalID), req, func(tx *gorm.DB, child *execution.TaskExecution) (*execution.TaskExecution, error) {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", fmt.Sprintf("meta.initial_upload|%d|%s", tenant, id)).Error; err != nil {
				return nil, err
			}
		}
		existing, err := findUploadScan(tx, tenant, id)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		candidates := []string{}
		for _, write := range receipt.Writes {
			if write.Disposition != "created" {
				continue
			}
			path := write.Target.StringPath()
			fp := commonmodels.GenerateItemFingerprint(receipt.EngineID, path)
			var known int64
			// Known whole ownership or an existing logical item's explicit member
			// blocks automatic ownership of an uploaded component.
			if err := tx.Model(&models.MetaItem{}).Where("tenant_id=? AND engine_id=?", tenant, receipt.EngineID).Where(`fingerprint=? OR (attributes->'item'->>'layout'='whole' AND left(?,length(full_name)+1)=full_name||'/') OR EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(attributes->'item'->'refs','[]'::jsonb)) ref WHERE ref->>'path'=?)`, fp, path, path).Count(&known).Error; err != nil {
				return nil, err
			}
			if known == 0 {
				candidates = append(candidates, fp)
			}
		}
		child.ParentExecutionID = &id
		child.ActorPrincipalID = &receipt.Uploader.PrincipalID
		child.ActorTenantMembershipID = &receipt.Uploader.MembershipID
		child.IssuedAuthorizationVersion = &receipt.Uploader.AuthorizationVersion
		if child.Metadata == nil {
			child.Metadata = commonmodels.JSONMap{}
		}
		child.Metadata["manager_upload_id"] = id
		child.ExecutionConfig["upload_receipt"] = receipt
		child.ExecutionConfig["upload_new_candidates"] = candidates
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return s.ReadUploadIdentification(ctx, tenant, id)
}
func uploadReceiptRefGroups(paths []string) []models.ScanRefGroup {
	// Each exact path is a claimed member, not permission to enumerate its parent.
	// The common grouping code is shared with the producer through its DTO helper.
	shared := client.UploadScanRefGroups(paths)
	groups := make([]models.ScanRefGroup, 0, len(shared))
	for _, group := range shared {
		g := models.ScanRefGroup{Primary: group.Primary}
		for _, ref := range group.Refs {
			g.Refs = append(g.Refs, models.ScanRef{Path: ref.Path, Role: ref.Role, Required: ref.Required, Primary: ref.Primary})
		}
		groups = append(groups, g)
	}
	return groups
}
func findUploadScan(db *gorm.DB, tenant uint, id string) (*execution.TaskExecution, error) {
	var run execution.TaskExecution
	err := db.Where("tenant_id=? AND module=? AND task_type=? AND parent_execution_id=? AND metadata->>'manager_upload_id'=?", tenant, execution.ModuleMeta, execution.TaskTypeScan, id, id).Order("id").Take(&run).Error
	return &run, err
}
func (s *ScanExecutionService) ReadUploadIdentification(ctx context.Context, tenant uint, id string) (*client.MetaUploadIdentification, error) {
	var result *client.MetaUploadIdentification
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := findUploadScan(tx, tenant, id)
		if err != nil {
			return err
		}
		result, err = s.uploadIdentification(ctx, tx, run, tenant, id)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

func (s *ScanExecutionService) uploadIdentification(ctx context.Context, db *gorm.DB, run *execution.TaskExecution, tenant uint, id string) (*client.MetaUploadIdentification, error) {
	result := &client.MetaUploadIdentification{UploadID: id, ScanExecutionID: run.ExecutionID, Status: run.Status, NewItems: []authorizationfacts.Binding{}}
	if run.Status == "success" {
		encoded, err := json.Marshal(run.ExecutionConfig["upload_new_candidates"])
		var candidates []string
		if err != nil || json.Unmarshal(encoded, &candidates) != nil {
			return nil, commonapi.ErrConflict
		}
		raw, err := json.Marshal(run.ExecutionConfig["upload_receipt"])
		var receipt client.UploadReceipt
		if err != nil || json.Unmarshal(raw, &receipt) != nil || receipt.Validate() != nil || receipt.UploadID != id || receipt.TenantID != int64(tenant) || run.ParentExecutionID == nil || *run.ParentExecutionID != id || run.Source != "manager" || run.TriggerType != "manual" {
			return nil, commonapi.ErrConflict
		}
		created := map[string]bool{}
		for _, write := range receipt.Writes {
			created[write.Target.StringPath()] = write.Disposition == "created"
		}
		var items []models.MetaItem
		if len(candidates) > 0 {
			if err := db.WithContext(ctx).Where("tenant_id=? AND engine_id=? AND fingerprint IN ? AND created_at>=?", tenant, receipt.EngineID, candidates, run.CreatedAt).Find(&items).Error; err != nil {
				return nil, err
			}
		}
		lookup, err := loadMetadataIdentityNodes(db.WithContext(ctx), tenant, items)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			descriptor := scanflow.KnownItemDescriptorFromAttributes(item.Attributes)
			if (descriptor.Layout != format.LayoutSingle && descriptor.Layout != format.LayoutMulti) || descriptor.Format == "" || descriptor.Format == "unknown" {
				continue
			}
			fresh := created[scanflow.KnownItemPhysicalPath(descriptor, &item)]
			if descriptor.Layout == format.LayoutMulti && len(descriptor.Refs) == 0 {
				fresh = false
			}
			for _, ref := range descriptor.Refs {
				fresh = fresh && created[ref.Path]
			}
			if !fresh {
				continue
			}
			facts, err := projectDataItemAuthorizationFacts(item, run.UpdatedAt, lookup)
			if err != nil {
				return nil, err
			}
			binding, err := facts.MetadataBinding()
			if err != nil {
				continue
			}
			result.NewItems = append(result.NewItems, binding)
		}
	}
	return result, result.Validate()
}

func (s *ScanExecutionService) uploadReadContext(ctx context.Context, run *execution.TaskExecution, lease execution.Lease) (context.Context, error) {
	raw, err := json.Marshal(run.ExecutionConfig["upload_receipt"])
	var frozen client.UploadReceipt
	if err != nil || json.Unmarshal(raw, &frozen) != nil || frozen.Validate() != nil || run.ParentExecutionID == nil || *run.ParentExecutionID != frozen.UploadID || run.Source != "manager" || run.SourceTaskID != nil || run.TriggerType != "manual" || run.ExecutionConfig["scan_depth"] != "basic" || run.ActorPrincipalID == nil || *run.ActorPrincipalID != frozen.Uploader.PrincipalID || run.ActorTenantMembershipID == nil || *run.ActorTenantMembershipID != frozen.Uploader.MembershipID || run.IssuedAuthorizationVersion == nil || *run.IssuedAuthorizationVersion != frozen.Uploader.AuthorizationVersion {
		return nil, commonapi.ErrForbidden
	}
	if s.uploadReceipts == nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	current, err := s.uploadReceipts.ReadUploadReceipt(ctx, uint(run.TenantID), frozen.UploadID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Validate() != nil || !reflect.DeepEqual(*current, frozen) {
		return nil, commonapi.ErrForbidden
	}
	return s.bindUploadedContent(ctx, run, lease, &frozen)
}

func (s *ScanExecutionService) bindUploadedContent(ctx context.Context, run *execution.TaskExecution, lease execution.Lease, receipt *client.UploadReceipt) (context.Context, error) {
	digest, err := execution.ComputeMetaScanExecutionPlanDigest(mustScanJSON(run.ExecutionConfig))
	if err != nil {
		return nil, commonapi.ErrForbidden
	}
	check := scanresource.CollectionReadCheck(func(call context.Context, resource *commonmodels.Engine) error {
		if err := s.checkScanCancellation(call, run.TenantID, run.ExecutionID); err != nil {
			return err
		}
		if resource != nil && (resource.ID != receipt.EngineID || resource.TenantID == nil || int64(*resource.TenantID) != receipt.TenantID || resource.EngineType != "nfs" || strconv.FormatInt(resource.Version, 10) != receipt.EngineVersion) {
			return commonapi.ErrForbidden
		}
		var current execution.TaskExecution
		if err := s.db.WithContext(call).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=? AND status='running' AND attempt=? AND lease_token=? AND lease_owner=? AND lease_expires_at>clock_timestamp()", run.TenantID, run.ExecutionID, execution.ModuleMeta, execution.TaskTypeScan, lease.Attempt, lease.Token, lease.Owner).Take(&current).Error; err != nil {
			return err
		}
		currentDigest, err := execution.ComputeMetaScanExecutionPlanDigest(mustScanJSON(current.ExecutionConfig))
		if err != nil || currentDigest != digest || current.ActorPrincipalID == nil || *current.ActorPrincipalID != receipt.Uploader.PrincipalID || current.ActorTenantMembershipID == nil || *current.ActorTenantMembershipID != receipt.Uploader.MembershipID || current.IssuedAuthorizationVersion == nil || *current.IssuedAuthorizationVersion != receipt.Uploader.AuthorizationVersion || current.Source != "manager" || current.SourceTaskID != nil || current.TriggerType != "manual" || current.ParentExecutionID == nil || *current.ParentExecutionID != receipt.UploadID {
			return commonapi.ErrForbidden
		}
		return nil
	})
	if err := check(ctx, nil); err != nil {
		return nil, err
	}
	targets := make([]plugin.EngineCatalogPath, 0, len(receipt.Writes))
	for _, write := range receipt.Writes {
		targets = append(targets, write.Target)
	}
	return scanresource.WithUploadedContentTargets(scanresource.WithCollectionReadCheck(ctx, check), targets), nil
}
func mustScanJSON(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
