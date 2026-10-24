package models

import (
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
)

const (
	ObservabilityIdentityNodeLimit           = 1000
	ObservabilityIdentityInstanceLimit       = 10000
	ObservabilityIdentityEngineLimit         = 1000
	ObservabilityIdentityResponseLimit int64 = 4 << 20
	ObservabilityIdentityTimeout             = 5 * time.Second
)

// ObservabilityIdentitySnapshot is a current, bounded System-owned identity
// private projection for Monitor discovery. Process endpoints are deployment
// declarations, never browser DTOs. Credentials are never included.
type ObservabilityIdentitySnapshot struct {
	ObservedAt        time.Time                     `json:"observed_at"`
	Nodes             []ObservabilityNodeIdentity   `json:"nodes"`
	ModuleInstances   []ObservabilityModuleIdentity `json:"module_instances"`
	EngineDeployments []ObservabilityEngineIdentity `json:"engine_deployments"`
}

// ObservabilityEngineSourceKey is the credential-free deployment source tuple.
// It identifies an admitted deployment, not a live SQL endpoint or tenant engine.
type ObservabilityEngineSourceKey struct {
	DockerEngineID string `json:"docker_engine_id"`
	Project        string `json:"project"`
	Service        string `json:"service"`
}

type ObservabilityEngineIdentity struct {
	DeploymentID string                       `json:"deployment_id"`
	Version      int64                        `json:"version"`
	Scope        string                       `json:"scope"`
	EngineType   string                       `json:"engine_type"`
	NodeID       string                       `json:"node_id"`
	SourceKey    ObservabilityEngineSourceKey `json:"source_key"`
	Metrics      *PostgreSQLMetricsSource     `json:"metrics,omitempty"`
}

var observabilityDockerEngineID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}$`)

// Validate checks the single supported platform engine identity contract.
// Snapshot consumers must additionally check node references and uniqueness.
func (i ObservabilityEngineIdentity) Validate() error {
	validID := func(value string) bool {
		id, err := uuid.Parse(value)
		return err == nil && id != uuid.Nil && id.String() == value
	}
	if !validID(i.DeploymentID) || !validID(i.NodeID) || i.Version < 1 || i.EngineType != "postgresql" ||
		!observabilityDockerEngineID.MatchString(i.SourceKey.DockerEngineID) || i.SourceKey.Service != "postgres" ||
		!((i.Scope == "infra" && i.SourceKey.Project == "addp-infra") || (i.Scope == "business" && i.SourceKey.Project == "business")) {
		return errors.New("invalid observability engine identity")
	}
	return i.Metrics.Validate()
}

type ObservabilityNodeIdentity struct {
	NodeID  string `json:"node_id"`
	Version int64  `json:"version"`
}

type ObservabilityModuleIdentity struct {
	Container        *ContainerProof            `json:"container,omitempty"`
	ProcessMetrics   *ProcessMetricsDeclaration `json:"process_metrics,omitempty"`
	ProcessStartedAt *time.Time                 `json:"process_started_at,omitempty"`
	ModuleName       string                     `json:"module_name"`
	InstanceID       string                     `json:"instance_id"`
	Role             string                     `json:"role"`
	NodeID           string                     `json:"node_id"`
	LeaseExpiresAt   time.Time                  `json:"lease_expires_at"`
}
