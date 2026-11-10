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
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

type privatePublicationRecord struct {
	Facts               client.DevelopFilePublicationFacts `json:"facts"`
	PreparationObserved bool                               `json:"preparation_observed"`
	ProviderState       []byte                             `json:"provider_state,omitempty"`
	DispatchReleasedAt  *time.Time                         `json:"dispatch_released_at,omitempty"`
}

func privateFilePublication(row *models.FileExecution) (*privatePublicationRecord, error) {
	var record privatePublicationRecord
	if err := json.Unmarshal(row.PublicationJSON, &record); err != nil {
		return nil, err
	}
	var admission client.DevelopFileAdmissionObservation
	if err := json.Unmarshal(row.AdmissionJSON, &admission); err != nil {
		return nil, err
	}
	digest := record.Facts.PreparationDigest
	if admission.ApprovalID != record.Facts.ApprovalID ||
		(!record.PreparationObserved && (digest != "" || len(record.ProviderState) != 0)) ||
		(record.DispatchReleasedAt != nil && (record.DispatchReleasedAt.IsZero() || digest == "")) ||
		(digest != "" && (len(record.ProviderState) == 0 || digest != providerPreparationDigest(record.ProviderState))) {
		return nil, commonapi.ErrConflict
	}
	return &record, nil
}

func providerPreparationDigest(state []byte) string {
	digest := sha256.Sum256(state)
	return hex.EncodeToString(digest[:])
}

// RegisterPrivatePublication records the sole operation before any target I/O.
// The receipt comes from the Owner's bound Runtime output, not a User request.
func (r *FileExecutionRepository) RegisterPrivatePublication(ctx context.Context, lease execution.Lease, runtimeID, operationID string, output plugin.WorkflowPrivateFile) error {
	return r.withPrivateFileLease(ctx, lease, func(tx *gorm.DB, row *models.FileExecution, issued client.IssuedDevelopFileAuthorization) error {
		if issued.Scope.Target == nil || runtimeID == "" || row.RuntimeExecutionID == nil ||
			*row.RuntimeExecutionID != runtimeID || len(row.PublicationJSON) != 0 {
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
		facts, err := privateFileFacts(row)
		if err != nil {
			return err
		}
		if len(manifest.Outputs) != 1 || facts.Intent.Target == nil ||
			manifest.Outputs[0] != facts.Intent.Target.NodeID || output.NodeID != facts.Intent.Target.NodeID {
			return commonapi.ErrConflict
		}
		record := privatePublicationRecord{Facts: client.DevelopFilePublicationFacts{
			ApprovalID: admission.ApprovalID,
			Request: plugin.NewFilePublicationRequest{OperationID: operationID, Target: issued.Scope.Target.Target,
				SizeBytes: output.Size, SHA256: output.SHA256},
		}}
		facts.Publication = &record.Facts
		if err := facts.Validate(); err != nil {
			return err
		}
		raw, err := json.Marshal(record)
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
		record, err := privateFilePublication(row)
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
		raw, err := json.Marshal(record)
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
		record, err := privateFilePublication(row)
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
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		update := tx.Model(row)
		if tx.Dialector.Name() == "postgres" {
			update = update.Where("clock_timestamp() < ?", release.ExpiresAt)
		}
		changed := update.Updates(map[string]any{"publication_json": raw, "version": row.Version + 1})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		prepared = &plugin.PreparedNewFile{NewFilePublicationRequest: record.Facts.Request, ProviderState: record.ProviderState}
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
	Issued            client.IssuedDevelopFileAuthorization
	ApprovalID        string
	Prepared          plugin.PreparedNewFile
	PreparationDigest string
	DispatchState     plugin.NewFilePublicationState
}

func (r *FileExecutionRepository) ReadPrivatePublication(ctx context.Context, tenant uint, executionID string) (*PrivateFilePublicationRecovery, error) {
	var row models.FileExecution
	if err := r.db.WithContext(ctx).Where("execution_id = ? AND tenant_id = ?", executionID, tenant).First(&row).Error; err != nil {
		return nil, err
	}
	if len(row.PublicationJSON) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	record, err := privateFilePublication(&row)
	if err != nil {
		return nil, err
	}
	var issued client.IssuedDevelopFileAuthorization
	if err := json.Unmarshal(row.IssuedAuthorizationJSON, &issued); err != nil {
		return nil, err
	}
	state := plugin.NewFileNotPublished
	if record.DispatchReleasedAt != nil {
		state = plugin.NewFileUnknown
	}
	return &PrivateFilePublicationRecovery{
		Issued: issued, ApprovalID: record.Facts.ApprovalID, PreparationDigest: record.Facts.PreparationDigest,
		Prepared:      plugin.PreparedNewFile{NewFilePublicationRequest: record.Facts.Request, ProviderState: record.ProviderState},
		DispatchState: state,
	}, nil
}
