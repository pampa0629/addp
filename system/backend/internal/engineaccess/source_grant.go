package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm/clause"
)

const (
	approvalModeCatalog     = "catalog"
	approvalModeIndependent = "independent"
)

var ErrGrantRelationExists = errors.Join(commonapi.ErrConflict, errors.New("source grant relation already exists"))

// sourceGrant owns the immutable source rule. An accepted Catalog receipt is
// one approval provenance, not a rule store required by read/revoke consumers.
// RequestID identifies the issuance command; it is not an access credential.
type sourceGrant struct {
	RequestID                    uuid.UUID `gorm:"type:uuid;primaryKey"`
	GrantedAt                    time.Time
	ApprovalMode                 string
	CatalogRequestID             *uuid.UUID `gorm:"type:uuid"`
	TenantID                     int64
	EngineID                     int64
	CatalogPath                  json.RawMessage `gorm:"type:jsonb"`
	ItemFingerprint              string
	BoundaryHash                 string
	FileComposition              json.RawMessage `gorm:"type:jsonb"`
	ScopeBoundary                json.RawMessage `gorm:"type:jsonb"`
	RecipientType                string
	RecipientID                  int64
	Action                       string
	ExpiryMode                   string
	ExpiresAt                    *time.Time
	DirectPermissionVersion      int64
	DirectPermissionSource       string
	OperatorPrincipalID          int64
	OperatorMembershipID         int64
	OperatorAuthorizationVersion int64
	Reason                       *string
	ProducerOwner                *string
	ProductionExecutionID        *uuid.UUID `gorm:"type:uuid"`
	IssuerPrincipalID            *int64
	IssuerMembershipID           *int64
}

func (sourceGrant) TableName() string { return "system.engine_access_grants" }

// All source adapters must establish their own current qualification and
// exact target boundary before calling the one persistence path. The database
// supplies issuance time and enforces immutable provenance/rule constraints.
func (r *Repository) insertSourceGrant(ctx context.Context, grant *sourceGrant) error {
	if grant == nil {
		return commonapi.ErrBadRequest
	}
	var primary plugin.EngineCatalogPath
	if json.Unmarshal(grant.CatalogPath, &primary) != nil {
		return commonapi.ErrBadRequest
	}
	file := len(primary.Segments) > 0 && (primary.Segments[len(primary.Segments)-1].Term == plugin.EngineCatalogTermFile || primary.Segments[len(primary.Segments)-1].Term == plugin.EngineCatalogTermObject)
	if len(grant.ScopeBoundary) != 0 || authorizationfacts.ValidWholeScopeRoot(primary) {
		var scope authorizationfacts.ScopeBoundary
		if grant.ApprovalMode != approvalModeIndependent || len(grant.FileComposition) != 0 || json.Unmarshal(grant.ScopeBoundary, &scope) != nil {
			return commonapi.ErrBadRequest
		}
		binding := authorizationfacts.Binding{ItemFingerprint: grant.ItemFingerprint, BoundaryHash: grant.BoundaryHash, Scope: &scope}
		target, err := binding.PrimaryTarget()
		if err != nil || !reflect.DeepEqual(target, primary) {
			return commonapi.ErrBadRequest
		}
	} else if file || len(grant.FileComposition) != 0 {
		var composition authorizationfacts.FileCompositionBoundary
		if !file || grant.ApprovalMode != approvalModeIndependent || json.Unmarshal(grant.FileComposition, &composition) != nil {
			return commonapi.ErrBadRequest
		}
		binding := authorizationfacts.Binding{ItemFingerprint: grant.ItemFingerprint, BoundaryHash: grant.BoundaryHash, FileComposition: &composition}
		target, err := binding.PrimaryTarget()
		if err != nil || !reflect.DeepEqual(target, primary) {
			return commonapi.ErrBadRequest
		}
	}
	err := r.db.WithContext(ctx).Clauses(clause.Returning{}).Create(grant).Error
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "engine_access_grant_relation_exists" {
		return ErrGrantRelationExists
	}
	return err
}
