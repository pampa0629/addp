package iam

import (
	"context"
	"errors"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	systemauth "github.com/addp/system/internal/authorization"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DevelopFileCreateApproverPreparer locks the target engine and current
// management scope in the approval transaction. The returned check uses the
// final database wall clock, including time spent waiting for locks.
type DevelopFileCreateApproverPreparer func(context.Context, *gorm.DB, int64, int64, execution.DevelopBoundFileTarget) (func(time.Time) error, error)

func (s *DevelopFileAuthorizationService) SetCreateApproverPreparer(prepare DevelopFileCreateApproverPreparer) {
	s.prepareCreateApprover = prepare
}

type ApproveDevelopFileCreateInput struct {
	UserAccessToken string
	AuthorizationID int64
	ScopeDigest     string
	ExpiresAt       time.Time
	Audit           AuditMetadata
}

type DevelopFileCreateApprovalReceipt struct {
	ApprovalID string    `json:"approval_id"`
	ApprovedAt time.Time `json:"approved_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type developFileCreateApproval struct {
	ID                           uuid.UUID `gorm:"type:uuid;primaryKey"`
	AuthorizationID              int64
	ScopeDigest                  string
	ApproverPrincipalID          int64
	ApproverMembershipID         int64
	ApproverAuthorizationVersion int64
	ApprovedAt                   time.Time
	ExpiresAt                    time.Time
}

func (developFileCreateApproval) TableName() string { return "system.develop_file_create_approvals" }

func (s *DevelopFileAuthorizationService) ApproveCreate(ctx context.Context, input ApproveDevelopFileCreateInput) (*DevelopFileCreateApprovalReceipt, error) {
	if input.AuthorizationID <= 0 || !validDevelopDigest(input.ScopeDigest) || input.ExpiresAt.IsZero() {
		return nil, commonapi.ErrBadRequest
	}
	if s == nil || s.repository == nil || s.owner == nil || s.prepareCreateApprover == nil {
		return nil, ErrDevelopFileDependencyUnavailable
	}
	const permission = systemauth.PermissionSystemDevelopFileCreateApprovalCreate
	approver, _, err := s.repository.developFileUser(ctx, input.UserAccessToken, false, permission)
	if err != nil {
		return nil, err
	}
	sealed, err := s.repository.GetExecutionAuthorization(ctx, input.AuthorizationID)
	if errors.Is(err, commonapi.ErrNotFound) {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	if err != nil {
		return nil, err
	}
	if sealed.TenantID != approver.TenantID || sealed.ActorPrincipalID == approver.ActorPrincipalID ||
		sealed.DevelopFileScope == nil || sealed.DevelopFileScope.Target == nil ||
		!strings.EqualFold(path.Ext(sealed.DevelopFileScope.Target.Target.StringPath()), ".gpkg") {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	binding := CheckDevelopFileInput{AuthorizationID: sealed.ID, ExecutionID: sealed.ExecutionID, TenantID: approver.TenantID,
		ExpectedAttempt: sealed.DevelopFileScope.ExpectedAttempt, ScopeDigest: input.ScopeDigest}
	if err := matchDevelopFileAuthorization(sealed, binding); err != nil {
		return nil, err
	}
	facts, err := s.readOwner(ctx, sealed, sealed.ExecutionID)
	if err != nil {
		return nil, err
	}
	if facts.AuthorizationID != strconv.FormatInt(sealed.ID, 10) || facts.ScopeDigest != input.ScopeDigest ||
		facts.AdmissionState != "pending" || !reflect.DeepEqual(facts.BoundScope, sealed.DevelopFileScope) {
		return nil, ErrExecutionAuthorizationConflict
	}
	expires := input.ExpiresAt.UTC().Truncate(time.Microsecond)
	var receipt *DevelopFileCreateApprovalReceipt
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		current, _, err := tx.developFileUser(ctx, input.UserAccessToken, true, permission)
		if err != nil {
			return err
		}
		if !sameDevelopActor(approver, current) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		_, membership, _, err := lockAndValidateExecutionSource(ctx, tx, current)
		if err != nil {
			return err
		}
		row, err := tx.LockExecutionAuthorization(ctx, sealed.ID)
		if err != nil {
			return err
		}
		if err := matchDevelopFileAuthorization(row, binding); err != nil {
			return err
		}
		if _, err := tx.developFileExecution(ctx, row, facts, binding.ExpectedAttempt, uuid.Nil, true); err != nil {
			return err
		}
		checkScope, err := s.prepareCreateApprover(ctx, tx.db, approver.TenantID, approver.TenantMembershipID, *row.DevelopFileScope.Target)
		if err != nil {
			return err
		}
		if checkScope == nil {
			return ErrDevelopFileDependencyUnavailable
		}
		check := func(now time.Time) error {
			if !expires.After(now) || expires.After(row.ExpiresAt) {
				return commonapi.ErrBadRequest
			}
			if membership.ExpiresAt != nil && !membership.ExpiresAt.After(now) {
				return ErrExecutionAuthorizationPermissionDenied
			}
			return checkScope(now)
		}
		now, err := tx.internalTaskDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if err := check(now); err != nil {
			return err
		}
		id, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		record := developFileCreateApproval{ID: id, AuthorizationID: row.ID, ScopeDigest: input.ScopeDigest,
			ApproverPrincipalID: approver.ActorPrincipalID, ApproverMembershipID: approver.TenantMembershipID,
			ApproverAuthorizationVersion: approver.IssuedAuthorizationVersion, ApprovedAt: now, ExpiresAt: expires}
		created := tx.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "authorization_id"}}, DoNothing: true}).Create(&record)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected != 1 {
			return ErrExecutionAuthorizationConflict
		}
		audit := input.Audit
		user, tenant := PrincipalTypeUser, ContextTypeTenant
		audit.PrincipalID, audit.PrincipalType, audit.ContextType, audit.TenantID = &record.ApproverPrincipalID, &user, &tenant, &row.TenantID
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: audit, EventName: "iam.develop_file_create_approval.created", Result: AuditResultSucceeded,
			RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "develop_file_create_approval", EntityID: id.String(),
			Details: map[string]any{"execution_id": row.ExecutionID.String(), "scope_digest": input.ScopeDigest}}); err != nil {
			return err
		}
		final, credential, err := tx.developFileUser(ctx, input.UserAccessToken, true, permission)
		if err != nil {
			return err
		}
		now, err = tx.internalTaskDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if !sameDevelopActor(approver, final) || !credential.Token.ExpiresAt.After(now) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		if err := check(now); err != nil {
			return err
		}
		receipt = &DevelopFileCreateApprovalReceipt{ApprovalID: id.String(), ApprovedAt: record.ApprovedAt, ExpiresAt: expires}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}
