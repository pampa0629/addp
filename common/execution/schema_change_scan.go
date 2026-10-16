package execution

import "time"

// SchemaChangeScanSource references an Owner-published applied approval and its
// current submission claim. It never declares an actor or a causal parent.
type SchemaChangeScanSource struct {
	ExecutionID string `json:"execution_id"`
	RequestID   uint   `json:"request_id"`
	ClaimToken  string `json:"claim_token"`
}

const SchemaChangeScanOriginKey = "schema_change_scan_origin"

// AppliedSchemaChangeScanOrigin is private durable provenance published by the
// approval Owner in the same transaction as its applied state. Claim fields are
// updated in the Owner's existing claim transactions. It carries no credential
// or execution authorization and is excluded from professional observations.
type AppliedSchemaChangeScanOrigin struct {
	RequestID                  uint       `json:"request_id"`
	TaskID                     uint       `json:"task_id"`
	Generation                 uint64     `json:"generation"`
	FromRevision               uint64     `json:"from_revision"`
	ToRevision                 uint64     `json:"to_revision"`
	TargetLocator              string     `json:"target_locator"`
	AppliedAt                  time.Time  `json:"applied_at"`
	TriggeredBy                int        `json:"triggered_by"`
	ActorPrincipalID           int64      `json:"actor_principal_id"`
	ActorTenantMembershipID    int64      `json:"actor_tenant_membership_id"`
	IssuedAuthorizationVersion int64      `json:"issued_authorization_version"`
	ClaimToken                 string     `json:"claim_token"`
	Attempt                    uint64     `json:"attempt"`
	LeaseUntil                 *time.Time `json:"lease_until,omitempty"`
}
