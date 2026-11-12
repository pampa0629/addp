package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/addp/common/production"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProductionAuthorizationClaim struct {
	TenantID  uint
	Reference production.Reference
	State     production.DeliveryState
}

// Claim delivery independently of the business lease/parent status. No lock is
// held while calling System or Meta. Attempts fence delayed duplicate replies.
func (r *FileExecutionRepository) ClaimProductionAuthorization(ctx context.Context, now time.Time) (*ProductionAuthorizationClaim, error) {
	var claim *ProductionAuthorizationClaim
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.FileExecution
		query := tx.Where("publication_json->'initial_authorization'->>'status'='pending' AND CAST(publication_json->'initial_authorization'->>'next_attempt_unix' AS BIGINT)<=?", now.Unix()).
			Order("CAST(publication_json->'initial_authorization'->>'next_attempt_unix' AS BIGINT),execution_id")
		if err := query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Take(&row).Error; err != nil {
			return err
		}
		record, err := privateFilePublication(&row)
		if err != nil {
			return err
		}
		state := record.InitialAuthorization
		state.Claim(now)
		body, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{"publication_json": body, "version": row.Version + 1}).Error; err != nil {
			return err
		}
		claim = &ProductionAuthorizationClaim{TenantID: row.TenantID, Reference: production.Reference{Owner: "develop", ExecutionID: row.ExecutionID, ResultID: record.Facts.Request.OperationID}, State: *state}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return claim, err
}

func (r *FileExecutionRepository) RecordProductionAuthorization(ctx context.Context, claim ProductionAuthorizationClaim, state production.DeliveryState) error {
	if state.Validate(publicationAuthorizationSchema) != nil || state.Attempts != claim.State.Attempts || state.NextAttemptUnix != claim.State.NextAttemptUnix {
		return production.ErrEvidence
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.FileExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND execution_id=?", claim.TenantID, claim.Reference.ExecutionID).Take(&row).Error; err != nil {
			return err
		}
		record, err := privateFilePublication(&row)
		if err != nil {
			return err
		}
		prior := record.InitialAuthorization
		if claim.Reference.Owner != "develop" || record.Facts.Request.OperationID != claim.Reference.ResultID {
			return production.ErrEvidence
		}
		if prior == nil || prior.Status != "pending" || prior.Attempts != claim.State.Attempts {
			return nil
		}
		record.InitialAuthorization = &state
		body, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return tx.Model(&row).Updates(map[string]any{"publication_json": body, "version": row.Version + 1}).Error
	})
}
