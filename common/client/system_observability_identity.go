package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

// GetObservabilityIdentities reads only the current System control-plane
// identity projection. It cannot be used with a tenant-bound client.
func (c *SystemServiceClient) GetObservabilityIdentities(ctx context.Context) (*models.ObservabilityIdentitySnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, models.ObservabilityIdentityTimeout)
	defer cancel()
	started := time.Now().UTC()
	var result models.ObservabilityIdentitySnapshot
	if err := c.doPlatformJSONBounded(ctx, http.MethodGet, "/api/v1/system/runtime/observability-identities", nil, &result, models.ObservabilityIdentityResponseLimit); err != nil {
		return nil, err
	}
	if err := validateObservabilityIdentities(result, started, time.Now().UTC()); err != nil {
		return nil, err
	}
	return &result, nil
}

func validateObservabilityIdentities(snapshot models.ObservabilityIdentitySnapshot, started, received time.Time) error {
	invalid := func() error { return errors.New("System returned an invalid observability identity snapshot") }
	if snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.Before(started.Add(-models.ObservabilityIdentityTimeout)) ||
		snapshot.ObservedAt.After(received.Add(models.ObservabilityIdentityTimeout)) || snapshot.Nodes == nil || snapshot.ModuleInstances == nil ||
		len(snapshot.Nodes) > models.ObservabilityIdentityNodeLimit || len(snapshot.ModuleInstances) > models.ObservabilityIdentityInstanceLimit {
		return invalid()
	}
	nodes := make(map[string]bool, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		id, err := uuid.Parse(node.NodeID)
		if err != nil || id == uuid.Nil || id.String() != node.NodeID || node.Version < 1 || nodes[node.NodeID] {
			return invalid()
		}
		nodes[node.NodeID] = true
	}
	type identity struct{ module, instance string }
	instances := make(map[identity]bool, len(snapshot.ModuleInstances))
	validName := func(value string, max int) bool {
		return value != "" && strings.TrimSpace(value) == value && utf8.ValidString(value) &&
			utf8.RuneCountInString(value) <= max && strings.IndexFunc(value, unicode.IsControl) < 0
	}
	for _, instance := range snapshot.ModuleInstances {
		key := identity{instance.ModuleName, instance.InstanceID}
		if !validName(instance.ModuleName, 50) || !validName(instance.InstanceID, 100) || (instance.NodeID != "" && !nodes[instance.NodeID]) || (instance.NodeID == "" && instance.ProcessMetrics == nil) ||
			instances[key] || !instance.LeaseExpiresAt.After(snapshot.ObservedAt) {
			return invalid()
		}
		if instance.ProcessMetrics != nil && (instance.ProcessMetrics.Validate() != nil || instance.ProcessStartedAt == nil || instance.ProcessStartedAt.IsZero() || instance.ProcessStartedAt.After(snapshot.ObservedAt)) {
			return invalid()
		}
		switch instance.Role {
		case "backend", "worker", "scheduler", "ingress":
		default:
			return invalid()
		}
		instances[key] = true
	}
	return nil
}
