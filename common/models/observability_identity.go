package models

import "time"

const (
	ObservabilityIdentityNodeLimit           = 1000
	ObservabilityIdentityInstanceLimit       = 10000
	ObservabilityIdentityResponseLimit int64 = 4 << 20
	ObservabilityIdentityTimeout             = 5 * time.Second
)

// ObservabilityIdentitySnapshot is a current, bounded System-owned identity
// private projection for Monitor discovery. Process endpoints are deployment
// declarations, never browser DTOs. Credentials are never included.
type ObservabilityIdentitySnapshot struct {
	ObservedAt      time.Time                     `json:"observed_at"`
	Nodes           []ObservabilityNodeIdentity   `json:"nodes"`
	ModuleInstances []ObservabilityModuleIdentity `json:"module_instances"`
}

type ObservabilityNodeIdentity struct {
	NodeID  string `json:"node_id"`
	Version int64  `json:"version"`
}

type ObservabilityModuleIdentity struct {
	ProcessMetrics   *ProcessMetricsDeclaration `json:"process_metrics,omitempty"`
	ProcessStartedAt *time.Time                 `json:"process_started_at,omitempty"`
	ModuleName       string                     `json:"module_name"`
	InstanceID       string                     `json:"instance_id"`
	Role             string                     `json:"role"`
	NodeID           string                     `json:"node_id"`
	LeaseExpiresAt   time.Time                  `json:"lease_expires_at"`
}
