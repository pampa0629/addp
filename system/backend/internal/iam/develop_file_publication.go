package iam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

type developFilePublication struct {
	ApprovalID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	OperationID       uuid.UUID
	RequestDigest     string
	LeaseDigest       string
	PrepareAdmittedAt time.Time
}

func (developFilePublication) TableName() string { return "system.develop_file_publications" }

type developFilePublicationUse struct {
	request client.DevelopFilePublicationRequest
	result  *client.DevelopFilePublicationObservation
}

func (s *DevelopFileAuthorizationService) AuthorizePublication(ctx context.Context, input CheckDevelopFileInput, request client.DevelopFilePublicationRequest) (*client.DevelopFilePublicationObservation, error) {
	if request.Validate() != nil || input.Source != nil || input.ExecutionID.String() != request.ExecutionID ||
		input.ExpectedAttempt != request.Attempt || input.ScopeDigest != request.ScopeDigest || input.LeaseToken.String() != request.LeaseToken {
		return nil, commonapi.ErrBadRequest
	}
	use := developFilePublicationUse{request: request}
	input.publication = &use
	if _, _, err := s.Check(ctx, input); err != nil {
		return nil, err
	}
	return use.result, nil
}

func (u *developFilePublicationUse) matches(facts *client.DevelopFileExecutionFacts) bool {
	return facts.Publication != nil && facts.Publication.ApprovalID == u.request.ApprovalID &&
		facts.Publication.Request.OperationID == u.request.OperationID &&
		facts.Publication.PreparationDigest == u.request.PreparationDigest
}

func (u *developFilePublicationUse) audit(ctx context.Context, tx *Repository, metadata AuditMetadata) error {
	r := u.request
	return NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: metadata, EventName: "iam.develop_file_publication." + r.Stage,
		Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system",
		EntityType: "develop_file_publication", EntityID: r.OperationID,
		Details: map[string]any{"approval_id": r.ApprovalID, "execution_id": r.ExecutionID, "scope_digest": r.ScopeDigest, "attempt": r.Attempt}})
}

// Called with the EA locked after the final current eligibility check. No
// credentials or raw lease secret are persisted in the publication record.
func (u *developFilePublicationUse) consume(ctx context.Context, tx *Repository, row *ExecutionAuthorization, prepared *DevelopFileReadPreparation, frozen plugin.NewFilePublicationRequest, observed, expires, deadline time.Time) error {
	request := u.request
	approval, operation := uuid.MustParse(request.ApprovalID), uuid.MustParse(request.OperationID)
	sum := sha256.Sum256([]byte(request.LeaseToken))
	lease := hex.EncodeToString(sum[:])
	raw, err := json.Marshal(frozen)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	requestDigest := hex.EncodeToString(digest[:])
	result := &client.DevelopFilePublicationObservation{AuthorizationID: strconv.FormatInt(row.ID, 10), ExecutionID: request.ExecutionID,
		Attempt: request.Attempt, ScopeDigest: request.ScopeDigest, ApprovalID: request.ApprovalID, OperationID: request.OperationID,
		Stage: request.Stage, PreparationDigest: request.PreparationDigest, ObservedAt: observed, ExpiresAt: expires, BusinessDeadline: deadline}
	db := tx.db.WithContext(ctx)
	if request.Stage == client.DevelopFilePublicationPrepare {
		record := developFilePublication{ApprovalID: approval, OperationID: operation, RequestDigest: requestDigest, LeaseDigest: lease, PrepareAdmittedAt: observed}
		created := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected != 1 {
			return ErrExecutionAuthorizationConflict
		}
		if prepared.TargetSnapshot == nil {
			return ErrDevelopFileDependencyUnavailable
		}
		engine, err := prepared.TargetSnapshot(ctx, tx.db)
		if err != nil {
			return err
		}
		target := row.DevelopFileScope.Target
		version, _ := strconv.ParseInt(target.EngineVersion, 10, 64)
		if engine == nil || engine.ID != target.Target.EngineID || engine.TenantID == nil || int64(*engine.TenantID) != row.TenantID ||
			engine.Version != version || engine.EngineType != "nfs" || engine.LifecycleState != "active" ||
			engine.DeletedAt != nil || len(engine.ConnectionInfo) == 0 {
			return ErrDevelopFileDependencyUnavailable
		}
		result.Engine = engine
	} else {
		changed := db.Model(&developFilePublication{}).
			Where("approval_id=? AND operation_id=? AND request_digest=? AND lease_digest=? AND released_at IS NULL AND clock_timestamp() < ?", approval, operation, requestDigest, lease, expires).
			Updates(map[string]any{"released_at": observed, "preparation_digest": request.PreparationDigest})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrExecutionAuthorizationConflict
		}
	}
	now, err := tx.internalTaskDatabaseTime(ctx)
	if err != nil {
		return err
	}
	if !expires.After(now) {
		return ErrExecutionAuthorizationUnavailable
	}
	u.result = result
	return nil
}
