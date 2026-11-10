package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"

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
