package engineaccess

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

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
}

func (sourceGrant) TableName() string { return "system.engine_access_grants" }

// Both approval adapters must establish their own current qualification and
// exact target boundary before calling the one persistence path. The database
// supplies issuance time and enforces immutable provenance/rule constraints.
func (r *Repository) insertSourceGrant(ctx context.Context, grant *sourceGrant) error {
	return r.db.WithContext(ctx).Clauses(clause.Returning{}).Create(grant).Error
}
