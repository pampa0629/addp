package models

import "time"

const (
	ObservabilityIdentityNodeLimit           = 1000
	ObservabilityIdentityInstanceLimit       = 10000
	ObservabilityIdentityResponseLimit int64 = 4 << 20
	ObservabilityIdentityTimeout             = 5 * time.Second
)

// ObservabilityIdentitySnapshot is a current, bounded System-owned identity
// projection for Monitor discovery. It carries neither endpoints nor credentials.
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
	ModuleName     string    `json:"module_name"`
	InstanceID     string    `json:"instance_id"`
	Role           string    `json:"role"`
	NodeID         string    `json:"node_id"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}
