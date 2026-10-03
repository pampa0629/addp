package engineaccess

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	errFulfillmentBinding = errors.New("fulfillment request binding conflict")
	errFulfillmentClosed  = errors.New("fulfillment request is closed")
	errFulfillmentExpired = errors.New("fulfillment request expiry is not future")
)

// Internal only. A future service must establish trusted caller identity and
// verify the persisted human source and current Permissions/delegation BEFORE
// entering the target boundary. The identity baseline below is not that proof.
// This binding is neither proof of business approval nor a data access token.
type fulfillmentRequest struct {
	RequestID          uuid.UUID
	TenantID           int64
	CallerPrincipalID  int64
	Operator           userProvenance
	Path               engineplugin.EngineCatalogPath
	DecisionID         uuid.UUID
	RequirementVersion int64
	RecipientType      string
	RecipientID        int64
	Action             string
	ExpiryMode         string
	ExpiresAt          *time.Time
}

type fulfillmentOutcome struct {
	RequestID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	TenantID          int64
	EngineID          int64
	CallerPrincipalID int64
	CatalogPath       json.RawMessage `gorm:"type:jsonb"`
	Binding           json.RawMessage `gorm:"type:jsonb"`
	ExpiryMode        string
	GrantExpiresAt    *time.Time
	Outcome           string
	RecordedAt        time.Time
	Deadline          *time.Time
}

func (fulfillmentOutcome) TableName() string { return "system.engine_access_fulfillment_outcomes" }

func (request fulfillmentRequest) encode() (json.RawMessage, json.RawMessage, error) {
	expiresAt, expiryErr := authorization.NormalizeSharingExpiry(request.ExpiryMode, request.ExpiresAt)
	if request.RequestID == uuid.Nil || request.TenantID <= 0 || request.CallerPrincipalID <= 0 || !request.Operator.valid() ||
		request.DecisionID == uuid.Nil || request.RequirementVersion <= 0 || request.RecipientID <= 0 ||
		!oneOfRecipient(request.RecipientType) || request.Action != "read" || expiryErr != nil {
		return nil, nil, errFulfillmentBinding
	}
	path, err := encodeFulfillmentPath(request.Path)
	if err != nil {
		return nil, nil, errFulfillmentBinding
	}
	binding, err := json.Marshal(struct {
		Operator           userProvenance `json:"operator"`
		DecisionID         uuid.UUID      `json:"decision_id"`
		RequirementVersion int64          `json:"requirement_version"`
		RecipientType      string         `json:"recipient_type"`
		RecipientID        int64          `json:"recipient_id"`
		Action             string         `json:"action"`
		ExpiryMode         string         `json:"expiry_mode"`
		ExpiresAt          *time.Time     `json:"expires_at"`
	}{request.Operator, request.DecisionID, request.RequirementVersion, request.RecipientType,
		request.RecipientID, request.Action, request.ExpiryMode, expiresAt})
	return path, binding, err
}

func encodeFulfillmentPath(path engineplugin.EngineCatalogPath) (json.RawMessage, error) {
	encoded, err := authorization.EncodeSharingTarget(path)
	if err != nil {
		return nil, errFulfillmentBinding
	}
	return encoded, nil
}

func oneOfRecipient(value string) bool {
	return value == "user" || value == "department" || value == "project_group"
}

// lockFulfillmentTarget is the single precise target boundary for future
// approval requirement changes and Grant writes too. Never call Catalog here.
func (r *Repository) lockFulfillmentTarget(ctx context.Context, tenantID int64, path json.RawMessage) error {
	// JSONB returns reordered keys/whitespace. Lock identity is the canonical
	// structured target, never the database's textual representation.
	var target engineplugin.EngineCatalogPath
	if err := json.Unmarshal(path, &target); err != nil {
		return errFulfillmentBinding
	}
	canonical, err := authorization.EncodeSharingTarget(target)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("engine_access_target|%d|%s", tenantID, canonical)
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error
}

// readFulfillment only observes a committed immutable outcome. A missing row
// is NOT a closed request: an in-flight accept may still commit afterwards.
// Authentication and current caller qualification belong to the runtime
// recovery service; this private method cannot be called as a public API.
func (r *Repository) readFulfillment(ctx context.Context, request fulfillmentRequest) (*fulfillmentOutcome, error) {
	path, binding, err := request.encode()
	if err != nil {
		return nil, err
	}
	var result *fulfillmentOutcome
	err = r.readCommittedFulfillmentHistory(ctx, func(tx *Repository) error {
		var err error
		result, err = tx.findFulfillment(ctx, request, path, binding)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// History owns its read-only transaction, before the caller's qualification
// transaction. Never expose the caller's own uncommitted arbitration writes.
func (r *Repository) readCommittedFulfillmentHistory(ctx context.Context, read func(*Repository) error) error {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); ok {
		return errFulfillmentBinding
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return read(NewRepository(tx))
	}, &sql.TxOptions{ReadOnly: true})
}

func (r *Repository) findFulfillment(ctx context.Context, request fulfillmentRequest, path, binding json.RawMessage) (*fulfillmentOutcome, error) {
	var row fulfillmentOutcome
	if err := r.db.WithContext(ctx).Where("request_id = ?", request.RequestID).Take(&row).Error; err != nil {
		return nil, err
	}
	if row.TenantID != request.TenantID || row.EngineID != int64(request.Path.EngineID) || row.CallerPrincipalID != request.CallerPrincipalID ||
		!equalJSON(row.CatalogPath, path) || !equalJSON(row.Binding, binding) || !authorization.EqualSharingExpiry(row.ExpiryMode, row.GrantExpiresAt, request.ExpiryMode, request.ExpiresAt) {
		return nil, errFulfillmentBinding
	}
	return &row, nil
}

// settleFulfillment must be called on the owner's transaction Repository.
// verify rechecks locally locked authority facts; it must not acquire earlier
// IAM locks or perform network IO. nil cannot create an accepted outcome.
// confirmation identifies the original confirmer from owner-persisted history;
// its audit version is not compared to current IAM. Supplying it does not prove
// Catalog responsibility or trusted provenance, which verify must establish.
// The row and audit commit or roll back with that transaction, not this method.
func (r *Repository) settleFulfillment(ctx context.Context, request fulfillmentRequest, accept bool,
	confirmation userProvenance, verify func(*Repository) error,
) (*fulfillmentOutcome, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, errFulfillmentBinding
	}
	path, binding, err := request.encode()
	if err != nil {
		return nil, err
	}
	var operator *lockedUserProvenance
	var scope *lockedManagementScope
	var recipient *lockedFulfillmentRecipient
	var confirmer *lockedBusinessConfirmer
	if accept {
		// Observe without arbitration locks first, so IAM always precedes the
		// request/target boundary. An existing immutable outcome is historical
		// recovery, not a new acceptance. A miss is NOT a close or acceptance.
		_, err := r.findFulfillment(ctx, request, path, binding)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !confirmation.valid() {
				return nil, errFulfillmentBinding
			}
			accountIDs := []int64{request.Operator.PrincipalID, confirmation.PrincipalID}
			if request.RecipientType == "user" {
				accountIDs = append(accountIDs, request.RecipientID)
			}
			principals, err := r.identity().LockUserAuthorizationPrincipals(ctx, accountIDs...)
			if err != nil {
				return nil, err
			}
			if request.RecipientType == "user" {
				recipient, err = r.lockFulfillmentRecipient(ctx, request, principals)
				if err != nil {
					return nil, err
				}
			}
			operator, err = r.lockUserProvenance(ctx, request.TenantID, request.Operator)
			if err != nil {
				return nil, err
			}
			now, err := r.wallClock(ctx)
			if err != nil {
				return nil, err
			}
			if err := operator.check(now); err != nil {
				return nil, err
			}
			// All accounts were locked above, before any lower IAM facts. The
			// original confirmer membership cannot be replaced by a new one.
			confirmer, err = r.loadBusinessConfirmer(ctx, confirmation, request.TenantID)
			if err != nil {
				return nil, err
			}
			now, err = r.wallClock(ctx)
			if err != nil {
				return nil, err
			}
			if err := confirmer.check(now); err != nil {
				return nil, err
			}
			if recipient == nil {
				recipient, err = r.lockFulfillmentRecipient(ctx, request, principals)
				if err != nil {
					return nil, err
				}
			}
			now, err = r.wallClock(ctx)
			if err != nil {
				return nil, err
			}
			if err := recipient.check(now); err != nil {
				return nil, err
			}
			scope, err = r.lockManagementScope(ctx, request.TenantID, int64(request.Path.EngineID), request.Operator.MembershipID)
			if err != nil {
				return nil, err
			}
			now, err = r.wallClock(ctx)
			if err != nil {
				return nil, err
			}
			if err := scope.check(now); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
	}
	// Request before target, everywhere. This also detects reuse across tenants
	// or targets without allowing their independent locks to race an INSERT.
	if err := r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
		"engine_access_request|"+request.RequestID.String()).Error; err != nil {
		return nil, err
	}
	if err := r.lockFulfillmentTarget(ctx, request.TenantID, path); err != nil {
		return nil, err
	}
	previous, err := r.findFulfillment(ctx, request, path, binding)
	if err == nil {
		if accept && previous.Outcome == "closed" {
			return nil, errFulfillmentClosed
		}
		return previous, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if accept {
		if verify == nil {
			return nil, errFulfillmentBinding
		}
		if err := r.requireCatalogApproval(ctx, request, path); err != nil {
			return nil, err
		}
		if err := verify(r); err != nil {
			return nil, err
		}
	}
	// Database wall clock AFTER lock waiting and verification, never tx now().
	now, err := r.wallClock(ctx)
	if err != nil {
		return nil, err
	}
	expiresAt, err := authorization.NormalizeSharingExpiry(request.ExpiryMode, request.ExpiresAt)
	if err != nil {
		return nil, errFulfillmentBinding
	}
	row := fulfillmentOutcome{RequestID: request.RequestID, TenantID: request.TenantID,
		EngineID: int64(request.Path.EngineID), CallerPrincipalID: request.CallerPrincipalID,
		CatalogPath: path, Binding: binding, ExpiryMode: request.ExpiryMode, GrantExpiresAt: expiresAt, Outcome: "closed", RecordedAt: now}
	if accept {
		if err := operator.check(now); err != nil {
			return nil, err
		}
		if err := scope.check(now); err != nil {
			return nil, err
		}
		if err := recipient.check(now); err != nil {
			return nil, err
		}
		// Principal locks stabilize IAM writes, not naturally expiring roles.
		// Recheck Permission validity at the post-verification wall clock.
		if err := confirmer.check(now); err != nil {
			return nil, err
		}
		if !authorization.SharingExpiryFuture(request.ExpiryMode, expiresAt, now) {
			return nil, errFulfillmentExpired
		}
		deadline := now.Add(5 * time.Minute)
		if expiresAt != nil && expiresAt.Before(deadline) {
			deadline = *expiresAt
		}
		row.Outcome, row.Deadline = "accepted", &deadline
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	principalType, contextType := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
	err = iam.NewAuditWriter(r.identity()).Write(ctx, iam.AuditEvent{
		Metadata: iam.AuditMetadata{PrincipalID: &request.CallerPrincipalID, PrincipalType: &principalType,
			ContextType: &contextType, TenantID: &request.TenantID},
		EventName: "system.engine_access_fulfillment." + row.Outcome,
		Result:    iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskHigh, ModuleName: "system",
		EntityType: "engine_access_fulfillment", EntityID: request.RequestID.String(),
		Details: map[string]any{"engine_id": request.Path.EngineID, "decision_id": request.DecisionID,
			"requirement_version": request.RequirementVersion, "operator": request.Operator},
	})
	return &row, err
}

func equalJSON(left, right json.RawMessage) bool {
	var a, b bytes.Buffer
	// JSONB changes object ordering/spacing. Compare canonical decoded values;
	// never coerce IDs through float64.
	canonical := func(value json.RawMessage, result *bytes.Buffer) error {
		decoder := json.NewDecoder(strings.NewReader(string(value)))
		decoder.UseNumber()
		var decoded any
		if err := decoder.Decode(&decoded); err != nil {
			return err
		}
		return json.NewEncoder(result).Encode(decoded)
	}
	return canonical(left, &a) == nil && canonical(right, &b) == nil && bytes.Equal(a.Bytes(), b.Bytes())
}
