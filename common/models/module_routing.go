package models

import "time"

// ModuleRoutingEndpoint identifies a forwarding target for one module.
type ModuleRoutingEndpoint struct {
	ModuleName string    `json:"module_name"`
	EndpointID string    `json:"endpoint_id"`
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type ModuleRoutingSnapshot struct {
	Revision   int64                   `json:"revision"`
	Endpoints  []ModuleRoutingEndpoint `json:"endpoints"`
	ObservedAt time.Time               `json:"observed_at"`
}
