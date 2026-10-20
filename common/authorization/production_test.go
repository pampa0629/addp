package authorization

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestInitialProductionTerminalContract(t *testing.T) {
	now := time.Now().UTC()
	valid := InitialProductionResult{SchemaVersion: InitialProductionResultSchema, TenantID: 7, ProducerOwner: "transfer", ExecutionID: uuid.New(), Outcome: "issued", RecordedAt: now,
		Grant: &InitialProductionGrant{RequestID: uuid.New(), GrantedAt: now, ExpiryMode: SharingExpiryUntilRevoked}}
	for _, name := range []string{"valid", "pending", "wrong_owner", "missing_grant", "contradictory_denial", "bad_expiry", "denied", "unknown_denial"} {
		t.Run(name, func(t *testing.T) {
			r := valid
			g := *valid.Grant
			r.Grant = &g
			switch name {
			case "pending":
				r.Outcome = "pending"
			case "wrong_owner":
				r.ProducerOwner = "other"
			case "missing_grant":
				r.Grant = nil
			case "contradictory_denial":
				r.DenialCode = "production_ineligible"
			case "bad_expiry":
				r.Grant.ExpiresAt = &now
			case "denied":
				r.Outcome = "denied"
				r.Grant = nil
				r.DenialCode = "source_access_denied"
			case "unknown_denial":
				r.Outcome = "denied"
				r.Grant = nil
				r.DenialCode = "unknown"
			}
			if (r.Validate() == nil) != (name == "valid" || name == "denied") {
				t.Fatal("invalid terminal contract accepted", name)
			}
		})
	}
	for _, lookup := range []InitialProductionLookup{{}, {Found: true, Result: &valid}} {
		if err := lookup.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, lookup := range []InitialProductionLookup{{Found: true}, {Result: &valid}} {
		if lookup.Validate() == nil {
			t.Fatal("ambiguous lookup accepted")
		}
	}
}
