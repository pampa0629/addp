package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm/clause"
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
	RecipientType                string
	RecipientID                  int64
	Action                       string
	ExpiryMode                   string
	ExpiresAt                    *time.Time
	RequirementVersion           int64
	OperatorPrincipalID          int64
	OperatorMembershipID         int64
	OperatorAuthorizationVersion int64
	Reason                       *string
	InitializedApproval          bool
}

func (sourceGrant) TableName() string { return "system.engine_access_grants" }

// Both approval adapters must establish their own current qualification and
// exact target boundary before calling the one persistence path. The database
// supplies issuance time and enforces immutable provenance/rule constraints.
func (r *Repository) insertSourceGrant(ctx context.Context, grant *sourceGrant) error {
	err := r.db.WithContext(ctx).Clauses(clause.Returning{}).Create(grant).Error
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "engine_access_grant_relation_exists" {
		return ErrGrantRelationExists
	}
	return err
}
