package authorization

import "time"

// EngineAccessHandlingScope is a current System observation, not a credential,
// delegation, business approval or acceptance receipt.
type EngineAccessHandlingScope struct {
	TenantID   int64                      `json:"tenant_id,string" swaggertype:"string"`
	EngineID   int64                      `json:"engine_id,string" swaggertype:"string"`
	Operator   SharingFulfillmentOperator `json:"operator"`
	VerifiedAt time.Time                  `json:"verified_at"`
}
