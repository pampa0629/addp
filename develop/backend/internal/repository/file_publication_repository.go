package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/production"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const publicationAuthorizationSchema = "develop.initial-authorization/v1"

type privatePublicationRecord struct {
	ResolvedAfterDispatch bool                               `json:"resolved_after_dispatch,omitempty"`
	InitialAuthorization  *production.DeliveryState          `json:"initial_authorization,omitempty"`
	Facts                 client.DevelopFilePublicationFacts `json:"facts"`
	PreparationObserved   bool                               `json:"preparation_observed"`
	ProviderState         []byte                             `json:"provider_state,omitempty"`
	DispatchReleasedAt    *time.Time                         `json:"dispatch_released_at,omitempty"`
	OutcomeObservedAt     *time.Time                         `json:"outcome_observed_at,omitempty"`
	RecoveryObservedAt    *time.Time                         `json:"recovery_observed_at,omitempty"`
	Outcome               *plugin.NewFilePublicationOutcome  `json:"outcome,omitempty"`
	RecoveryOutcome       *plugin.NewFilePublicationOutcome  `json:"recovery_outcome,omitempty"`
}

type privatePublications []privatePublicationRecord

func (records privatePublications) find(operation string) (*privatePublicationRecord, error) {
	for i := range records {
		if records[i].Facts.Request.OperationID == operation {
			return &records[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func privateFilePublications(row *models.FileExecution) (privatePublications, error) {
	if len(row.PublicationJSON) == 0 {
		return nil, nil
	}
	var records privatePublications
	if err := json.Unmarshal(row.PublicationJSON, &records); err != nil {
		return nil, err
	}
	var admission client.DevelopFileAdmissionObservation
	if err := json.Unmarshal(row.AdmissionJSON, &admission); err != nil {
		return nil, err
	}
	if len(records) == 0 || len(records) > len(admission.ApprovalIDs) {
		return nil, commonapi.ErrConflict
	}
	for i := range records {
		if err := records[i].validate(admission.ApprovalIDs[i]); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (record *privatePublicationRecord) validate(approvalID string) error {
	if (record.OutcomeObservedAt != nil && (record.Outcome == nil || record.OutcomeObservedAt.IsZero())) || (record.RecoveryObservedAt != nil && (record.RecoveryOutcome == nil || record.RecoveryObservedAt.IsZero())) {
		return commonapi.ErrConflict
	}
	if record.InitialAuthorization != nil && record.InitialAuthorization.Validate(publicationAuthorizationSchema) != nil {
		return commonapi.ErrConflict
	}
	if record.InitialAuthorization != nil && !((record.Outcome != nil && record.Outcome.State == plugin.NewFilePublished && record.OutcomeObservedAt != nil) || (record.Outcome == nil && record.RecoveryOutcome != nil && record.RecoveryOutcome.State == plugin.NewFilePublished && record.RecoveryObservedAt != nil)) {
		return commonapi.ErrConflict
	}
	digest := record.Facts.PreparationDigest
	if approvalID != record.Facts.ApprovalID ||
		(!record.PreparationObserved && (digest != "" || len(record.ProviderState) != 0)) ||
		(record.DispatchReleasedAt != nil && (record.DispatchReleasedAt.IsZero() || digest == "")) ||
		(digest != "" && (len(record.ProviderState) == 0 || digest != providerPreparationDigest(record.ProviderState))) {
		return commonapi.ErrConflict
	}
	if record.Outcome != nil && (record.DispatchReleasedAt == nil ||
		record.Outcome.State == plugin.NewFileUnknown || record.Outcome.Validate(record.prepared()) != nil) {
		return commonapi.ErrConflict
	}
	if record.RecoveryOutcome != nil && (record.RecoveryOutcome.State == plugin.NewFileUnknown ||
		record.RecoveryOutcome.Validate(record.prepared()) != nil ||
		(record.RecoveryOutcome.State == plugin.NewFilePublished && record.DispatchReleasedAt == nil)) {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *privatePublicationRecord) prepared() plugin.PreparedNewFile {
	return plugin.PreparedNewFile{NewFilePublicationRequest: r.Facts.Request, ProviderState: r.ProviderState}
}

func providerPreparationDigest(state []byte) string {
	digest := sha256.Sum256(state)
	return hex.EncodeToString(digest[:])
}

// RegisterPrivatePublication records the next file’s original operation before target I/O.
// The receipt comes from the Owner's bound Runtime output, not a User request.
func (r *FileExecutionRepository) RegisterPrivatePublication(ctx context.Context, lease execution.Lease, runtimeID, operationID string, output plugin.WorkflowPrivateFile) error {
	return r.withPrivateFileLease(ctx, lease, func(tx *gorm.DB, row *models.FileExecution, issued client.IssuedDevelopFileAuthorization) error {
		if len(issued.Scope.Targets) == 0 || runtimeID == "" || row.RuntimeExecutionID == nil ||
			*row.RuntimeExecutionID != runtimeID {
			return commonapi.ErrConflict
		}
		var manifest plugin.WorkflowPrivateFileManifest
		if err := json.Unmarshal(row.RuntimeManifestJSON, &manifest); err != nil {
			return err
		}
		var admission client.DevelopFileAdmissionObservation
		if err := json.Unmarshal(row.AdmissionJSON, &admission); err != nil {
			return err
		}
		facts, records, err := privateFileFactsAndPublication(row)
		if err != nil {
			return err
		}
		index := len(records)
		if len(manifest.Outputs) != len(facts.Intent.Targets) || !admission.ValidApprovals(len(facts.Intent.Targets)) ||
			index >= len(manifest.Outputs) || manifest.Outputs[index] != output.NodeID || facts.Intent.Targets[index].NodeID != output.NodeID {
			return commonapi.ErrConflict
		}
		if index > 0 {
			previous := records[index-1]
			if previous.Outcome == nil || previous.Outcome.State != plugin.NewFilePublished || previous.ResolvedAfterDispatch {
				return commonapi.ErrConflict
			}
		}
		record := privatePublicationRecord{Facts: client.DevelopFilePublicationFacts{
			ApprovalID: admission.ApprovalIDs[index],
			Request: plugin.NewFilePublicationRequest{OperationID: operationID, Target: issued.Scope.Targets[index].Target,
				SizeBytes: output.Size, SHA256: output.SHA256},
		}}
		records = append(records, record)
		facts.Publications = append(facts.Publications, record.Facts)
		if err := facts.Validate(); err != nil {
			return err
		}
		raw, err := json.Marshal(records)
		if err != nil {
			return err
		}
		return tx.Model(row).Updates(map[string]any{"publication_json": raw, "version": row.Version + 1}).Error
	})
}

// RecordPrivatePublicationPreparation keeps the original Provider state even
// when Prepare is uncertain. Only a confirmed receipt is exposed as prepared.
func (r *FileExecutionRepository) RecordPrivatePublicationPreparation(ctx context.Context, lease execution.Lease, result plugin.PreparedNewFile, confirmed bool) error {
	return r.withPrivateFileLease(ctx, lease, func(tx *gorm.DB, row *models.FileExecution, _ client.IssuedDevelopFileAuthorization) error {
		records, err := privateFilePublications(row)
		if err != nil {
			return err
		}
		record, err := records.find(result.OperationID)
		if err != nil {
			return err
		}
		if record.PreparationObserved || !reflect.DeepEqual(record.Facts.Request, result.NewFilePublicationRequest) ||
			(confirmed && len(result.ProviderState) == 0) {
			return commonapi.ErrConflict
		}
		record.PreparationObserved = true
		record.ProviderState = result.ProviderState
		if confirmed {
			record.Facts.PreparationDigest = providerPreparationDigest(result.ProviderState)
		}
		raw, err := json.Marshal(records)
		if err != nil {
			return err
		}
		return tx.Model(row).Updates(map[string]any{"publication_json": raw, "version": row.Version + 1}).Error
	})
}

// ClaimPrivatePublicationDispatch consumes one live release for the original
// operation. Only the caller that receives a successful commit may invoke
// Publish within the release window. The persisted record is resolve-only.
func (r *FileExecutionRepository) ClaimPrivatePublicationDispatch(ctx context.Context, lease execution.Lease, release client.DevelopFilePublicationObservation) (*plugin.PreparedNewFile, error) {
	var prepared *plugin.PreparedNewFile
	err := r.withPrivateFileLease(ctx, lease, func(tx *gorm.DB, row *models.FileExecution, issued client.IssuedDevelopFileAuthorization) error {
		records, err := privateFilePublications(row)
		if err != nil {
			return err
		}
		record, err := records.find(release.OperationID)
		if err != nil {
			return err
		}
		if record.DispatchReleasedAt != nil || record.Facts.PreparationDigest == "" ||
			release.Stage != client.DevelopFilePublicationRelease || release.Engine != nil ||
			release.AuthorizationID != issued.Authorization.ID || release.ExecutionID != lease.ExecutionID ||
			release.Attempt != lease.Attempt || release.ScopeDigest != issued.ScopeDigest ||
			release.ApprovalID != record.Facts.ApprovalID || release.OperationID != record.Facts.Request.OperationID ||
			release.PreparationDigest != record.Facts.PreparationDigest {
			return commonapi.ErrConflict
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		if release.ObservedAt.IsZero() || release.ObservedAt.After(now) || !now.Before(release.ExpiresAt) ||
			release.ExpiresAt.After(release.BusinessDeadline) || release.BusinessDeadline.After(issued.Authorization.ExpiresAt) {
			return commonapi.ErrConflict
		}
		record.DispatchReleasedAt = &release.ObservedAt
		raw, err := json.Marshal(records)
		if err != nil {
			return err
		}
		update := tx.Model(row)
		if tx.Dialector.Name() == "mysql" {
			update = update.Where("CURRENT_TIMESTAMP(6) < ?", release.ExpiresAt)
		} else if tx.Dialector.Name() == "postgres" {
			update = update.Where("clock_timestamp() < ?", release.ExpiresAt)
		}
		changed := update.Updates(map[string]any{"publication_json": raw, "version": row.Version + 1})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		value := record.prepared()
		prepared = &value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

// PrivateFilePublicationRecovery is only used by the Develop owner. A past
// dispatch claim carries no authority to publish again.
type PrivateFilePublicationRecovery struct {
	TargetIndex        int
	Issued             client.IssuedDevelopFileAuthorization
	ApprovalID         string
	Prepared           plugin.PreparedNewFile
	PreparationDigest  string
	DispatchState      plugin.NewFilePublicationState
	OutcomeObservedAt  *time.Time
	RecoveryObservedAt *time.Time
	Outcome            *plugin.NewFilePublicationOutcome
	RecoveryOutcome    *plugin.NewFilePublicationOutcome
}

func (r *FileExecutionRepository) ReadPrivatePublications(ctx context.Context, tenant uint, executionID string) ([]PrivateFilePublicationRecovery, error) {
	var row models.FileExecution
	if err := r.db.WithContext(ctx).Where("execution_id = ? AND tenant_id = ?", executionID, tenant).First(&row).Error; err != nil {
		return nil, err
	}
	records, err := privateFilePublications(&row)
	if err != nil {
		return nil, err
	}
	var issued client.IssuedDevelopFileAuthorization
	if err := json.Unmarshal(row.IssuedAuthorizationJSON, &issued); err != nil {
		return nil, err
	}
	var result []PrivateFilePublicationRecovery
	for i, record := range records {
		state := plugin.NewFileNotPublished
		if record.DispatchReleasedAt != nil {
			state = plugin.NewFileUnknown
		}
		if record.Outcome != nil {
			state = record.Outcome.State
		}
		result = append(result, PrivateFilePublicationRecovery{TargetIndex: i,
			Issued: issued, ApprovalID: record.Facts.ApprovalID, PreparationDigest: record.Facts.PreparationDigest,
			Prepared: record.prepared(), DispatchState: state, Outcome: record.Outcome, RecoveryOutcome: record.RecoveryOutcome, OutcomeObservedAt: record.OutcomeObservedAt, RecoveryObservedAt: record.RecoveryObservedAt,
		})
	}
	return result, nil
}

// RecordPrivatePublicationOutcome keeps the exact original Provider evidence.
// Unknown is already durable in the dispatch claim and cannot authorize cleanup.
func (r *FileExecutionRepository) ReadPrivatePublication(ctx context.Context, tenant uint, executionID, operationID string) (*PrivateFilePublicationRecovery, error) {
	records, err := r.ReadPrivatePublications(ctx, tenant, executionID)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if records[i].Prepared.OperationID == operationID {
			return &records[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *FileExecutionRepository) RecordPrivatePublicationOutcome(ctx context.Context, lease execution.Lease, prepared plugin.PreparedNewFile, outcome plugin.NewFilePublicationOutcome, resolved bool) error {
	return r.updatePrivatePublicationOutcome(ctx, lease, prepared, func(record *privatePublicationRecord) error {
		if record.Outcome != nil || outcome.State == plugin.NewFileUnknown || outcome.Validate(prepared) != nil {
			return commonapi.ErrConflict
		}
		record.ResolvedAfterDispatch = resolved
		record.Outcome = &outcome
		return nil
	})
}

func (r *FileExecutionRepository) MarkPrivatePublicationCleaned(ctx context.Context, lease execution.Lease, prepared plugin.PreparedNewFile) error {
	return r.updatePrivatePublicationOutcome(ctx, lease, prepared, func(record *privatePublicationRecord) error {
		if record.Outcome == nil || !record.Outcome.CleanupPending {
			return commonapi.ErrConflict
		}
		record.Outcome.CleanupPending = false
		return nil
	})
}

func (r *FileExecutionRepository) updatePrivatePublicationOutcome(ctx context.Context, lease execution.Lease, prepared plugin.PreparedNewFile, change func(*privatePublicationRecord) error) error {
	return r.withPrivateFileLease(ctx, lease, func(tx *gorm.DB, row *models.FileExecution, _ client.IssuedDevelopFileAuthorization) error {
		records, err := privateFilePublications(row)
		if err != nil {
			return err
		}
		record, err := records.find(prepared.OperationID)
		if err != nil {
			return err
		}
		if record.DispatchReleasedAt == nil || !reflect.DeepEqual(record.prepared(), prepared) {
			return commonapi.ErrConflict
		}
		firstOutcome := record.Outcome == nil
		if err := change(record); err != nil {
			return err
		}
		if firstOutcome && record.Outcome != nil {
			now, err := execution.DatabaseNow(tx)
			if err != nil {
				return err
			}
			record.OutcomeObservedAt = &now
			if record.Outcome.State == plugin.NewFilePublished {
				record.InitialAuthorization = &production.DeliveryState{SchemaVersion: publicationAuthorizationSchema, Status: "pending", NextAttemptUnix: now.Unix()}
			}
		}
		raw, err := json.Marshal(records)
		if err != nil {
			return err
		}
		return tx.Model(row).Updates(map[string]any{"publication_json": raw, "version": row.Version + 1}).Error
	})
}

type PrivatePublicationIdentity struct {
	ExecutionID string
	TenantID    uint
}

// Keyset scanning keeps the existing supervisor bounded without starving later
// records behind already completed recovery. No new execution is enqueued.
func (r *FileExecutionRepository) ListPrivatePublicationRecoveries(ctx context.Context, after string) ([]PrivatePublicationIdentity, error) {
	var rows []PrivatePublicationIdentity
	shape := "jsonb_typeof(f.publication_json)='array'"
	switch r.db.Dialector.Name() {
	case "mysql":
		shape = "JSON_TYPE(f.publication_json)='ARRAY'"
	case "sqlite":
		shape = "json_type(f.publication_json)='array'"
	}
	err := r.db.WithContext(ctx).Table("develop.file_executions f").
		Select("f.execution_id,f.tenant_id").
		Joins("JOIN common.task_executions e ON e.execution_id=f.execution_id AND e.tenant_id=f.tenant_id").
		Where(shape+" AND f.execution_id > ? AND e.status NOT IN ?", after, []string{"pending", "running"}).
		Order("f.execution_id").Limit(100).Scan(&rows).Error
	return rows, err
}

// Recovery facts are private observations only. There is deliberately no write
// to common.task_executions and no promotion into the original leased outcome.
func (r *FileExecutionRepository) RecordPrivatePublicationRecovery(ctx context.Context, tenant uint, executionID string, prepared plugin.PreparedNewFile, outcome plugin.NewFilePublicationOutcome) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.FileExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id=? AND tenant_id=?", executionID, tenant).First(&row).Error; err != nil {
			return err
		}
		records, err := privateFilePublications(&row)
		if err != nil {
			return err
		}
		record, err := records.find(prepared.OperationID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(record.prepared(), prepared) || record.Facts.PreparationDigest == "" ||
			outcome.State == plugin.NewFileUnknown || outcome.Validate(prepared) != nil ||
			(outcome.State == plugin.NewFilePublished && record.DispatchReleasedAt == nil) {
			return commonapi.ErrConflict
		}
		for _, prior := range []*plugin.NewFilePublicationOutcome{record.Outcome, record.RecoveryOutcome} {
			if prior != nil && (prior.State != outcome.State || !reflect.DeepEqual(prior.Evidence, outcome.Evidence) ||
				(!prior.CleanupPending && outcome.CleanupPending)) {
				return commonapi.ErrConflict
			}
		}
		if record.RecoveryOutcome == nil {
			now, err := execution.DatabaseNow(tx)
			if err != nil {
				return err
			}
			record.RecoveryObservedAt = &now
			if record.Outcome == nil && outcome.State == plugin.NewFilePublished {
				record.InitialAuthorization = &production.DeliveryState{SchemaVersion: publicationAuthorizationSchema, Status: "pending", NextAttemptUnix: now.Unix()}
			}
		}
		record.RecoveryOutcome = &outcome
		raw, err := json.Marshal(records)
		if err != nil {
			return err
		}
		return tx.Model(&row).Updates(map[string]any{"publication_json": raw, "version": row.Version + 1}).Error
	})
}

// CompletePrivatePublication reads only the original leased Provider outcome.
// Late recovery observations can never turn an expired attempt into success.
func (r *FileExecutionRepository) CompletePrivatePublication(ctx context.Context, item *execution.TaskExecution, lease execution.Lease, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := execution.LockActiveLease(ctx, tx, lease)
		if err != nil {
			return err
		}
		if current.Module != execution.ModuleDevelop || current.TaskType != execution.TaskTypeWorkflow || item == nil || item.ExecutionID != lease.ExecutionID {
			return commonapi.ErrConflict
		}
		var row models.FileExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id=? AND tenant_id=?", lease.ExecutionID, lease.TenantID).First(&row).Error; err != nil {
			return err
		}
		records, err := privateFilePublications(&row)
		if err != nil {
			return err
		}
		var intent execution.DevelopFileIntent
		if err := json.Unmarshal(row.IntentJSON, &intent); err != nil {
			return err
		}
		if len(records) == 0 || len(records) != len(intent.Targets) {
			return commonapi.ErrConflict
		}
		for _, record := range records {
			if record.DispatchReleasedAt == nil || record.Outcome == nil || record.Outcome.State != plugin.NewFilePublished || record.Outcome.Validate(record.prepared()) != nil {
				return commonapi.ErrConflict
			}
		}
		return NewExecutionRepository(tx).CompleteWithLease(ctx, item, lease, execution.ExecutionStatusSuccess, time.Now().UTC(), fields)
	})
}
