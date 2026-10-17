package authorization

import (
	"errors"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

// SharingFulfillmentBinding transports immutable request history, not approval,
// current permission, or a data access credential.
type SharingFulfillmentOperator struct {
	PrincipalID          int64 `json:"principal_id,string" swaggertype:"string"`
	MembershipID         int64 `json:"tenant_membership_id,string" swaggertype:"string"`
	AuthorizationVersion int64 `json:"authorization_version,string" swaggertype:"string"`
}

type SharingFulfillmentBinding struct {
	CallerPrincipalID  int64                      `json:"caller_principal_id,string" swaggertype:"string"`
	Operator           SharingFulfillmentOperator `json:"operator"`
	Path               plugin.EngineCatalogPath   `json:"path"`
	ItemFingerprint    string                     `json:"item_fingerprint"`
	BoundaryHash       string                     `json:"boundary_hash"`
	DecisionID         uuid.UUID                  `json:"decision_id"`
	RequirementVersion int64                      `json:"requirement_version,string" swaggertype:"string"`
	RecipientType      string                     `json:"recipient_type"`
	RecipientID        int64                      `json:"recipient_id,string" swaggertype:"string"`
	Action             string                     `json:"action"`
	ExpiryMode         string                     `json:"expiry_mode"`
	ExpiresAt          *time.Time                 `json:"expires_at"`
}

func (b SharingFulfillmentBinding) Validate() error {
	if b.CallerPrincipalID <= 0 || b.Operator.PrincipalID <= 0 || b.Operator.MembershipID <= 0 ||
		b.Operator.AuthorizationVersion <= 0 || b.DecisionID == uuid.Nil || b.RequirementVersion <= 0 || b.RecipientID <= 0 ||
		(b.RecipientType != "user" && b.RecipientType != "project_group") || b.Action != "read" {
		return errors.New("invalid sharing fulfillment binding")
	}
	if _, err := EncodeSharingTarget(b.Path); err != nil {
		return err
	}
	if err := (authorizationfacts.Binding{ItemFingerprint: b.ItemFingerprint, BoundaryHash: b.BoundaryHash, NativeTarget: b.Path}).Validate(); err != nil {
		return err
	}
	_, err := NormalizeSharingExpiry(b.ExpiryMode, b.ExpiresAt)
	return err
}

type SharingFulfillmentResolution struct {
	RequestID  uuid.UUID                 `json:"request_id"`
	TenantID   int64                     `json:"tenant_id,string" swaggertype:"string"`
	Binding    SharingFulfillmentBinding `json:"binding"`
	Outcome    string                    `json:"outcome"`
	RecordedAt time.Time                 `json:"recorded_at"`
	Deadline   *time.Time                `json:"deadline"`
}

type SharingFulfillmentLookup struct {
	Found      bool                          `json:"found"`
	Resolution *SharingFulfillmentResolution `json:"resolution"`
}

// SharingFulfillmentGrant projects immutable System issuance history. It is
// neither a stored Grant copy nor a current data-access decision or credential.
type SharingFulfillmentGrant struct {
	RequestID uuid.UUID `json:"request_id"`
	GrantedAt time.Time `json:"granted_at"`
}

type SharingFulfillmentGrantLookup struct {
	Found bool                     `json:"found"`
	Grant *SharingFulfillmentGrant `json:"grant,omitempty"`
	// Current System observation, not an immutable failure or access decision.
	UnissuedReason string `json:"unissued_reason,omitempty" enums:"relation_exists,window_expired,closed"`
}

const (
	SharingGrantRelationExists = "relation_exists"
	SharingGrantWindowExpired  = "window_expired"
	SharingGrantClosed         = "closed"
)

func (g SharingFulfillmentGrantLookup) Validate(requestID uuid.UUID) error {
	if g.Found {
		if g.Grant == nil || g.Grant.RequestID != requestID || g.Grant.GrantedAt.IsZero() || g.UnissuedReason != "" {
			return errors.New("invalid fulfillment grant lookup")
		}
		return nil
	}
	if g.Grant != nil {
		return errors.New("invalid fulfillment grant lookup")
	}
	switch g.UnissuedReason {
	case "", SharingGrantRelationExists, SharingGrantWindowExpired, SharingGrantClosed:
		return nil
	default:
		return errors.New("invalid fulfillment grant lookup")
	}
}

// SharingFulfillmentBasis is returned only by Catalog's dedicated runtime
// endpoint for an exact committed pending request. It is not an access token.
type SharingFulfillmentBasis struct {
	RequestID    uuid.UUID                  `json:"request_id"`
	TenantID     int64                      `json:"tenant_id,string" swaggertype:"string"`
	Binding      SharingFulfillmentBinding  `json:"binding"`
	Confirmation SharingFulfillmentOperator `json:"confirmation"`
}
