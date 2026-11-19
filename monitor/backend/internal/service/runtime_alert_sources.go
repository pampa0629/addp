package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	monitormodels "github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/resourcequery"
	"gorm.io/gorm"
)

var errCollectionPaused = errors.New("metric collection paused")

type runtimeAlertSources struct {
	scopes             []resourcequery.ProcessScope
	instances          []string
	signature          string
	lease              time.Time
	collectionVersions map[string]int64
	collectionChanged  time.Time
}

func runtimeSources(ctx context.Context, o *ResourceObservationService, identities metricsdiscovery.IdentityReader, policyVersion uint64, matches func(commonmodels.ObservabilityModuleIdentity) bool) (runtimeAlertSources, error) {
	result := runtimeAlertSources{}
	if identities == nil || o == nil || o.policy == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	before := time.Now().UTC()
	snapshot, err := identities.GetObservabilityIdentities(ctx)
	if err != nil || snapshot == nil || snapshot.ModuleInstances == nil || len(snapshot.ModuleInstances) > commonmodels.ObservabilityIdentityInstanceLimit || snapshot.ObservedAt.Before(before.Add(-5*time.Second)) || snapshot.ObservedAt.After(time.Now().UTC().Add(5*time.Second)) {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	var collection map[string]monitormodels.AutomaticCollectionScope
	if o.automaticCollection != nil {
		collection, err = o.automaticCollection.Snapshot(ctx)
		if err != nil {
			return result, err
		}
		result.collectionVersions = map[string]int64{}
	}
	rows := append([]commonmodels.ObservabilityModuleIdentity{}, snapshot.ModuleInstances...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].InstanceID < rows[j].InstanceID })
	physical := map[string]bool{}
	instances := map[string]bool{}
	for _, r := range rows {
		if !matches(r) {
			continue
		}
		if collection != nil {
			id := metricsdiscovery.ProcessCollectionScope(r).ID
			if id != "" {
				intent := collection[id]
				if intent.Version != 0 && !intent.Enabled {
					return result, errCollectionPaused
				}
				result.collectionVersions[id] = intent.Version
				if intent.ChangedAt.After(result.collectionChanged) {
					result.collectionChanged = intent.ChangedAt
				}
			}
		}
		if !r.LeaseExpiresAt.After(before) || !r.LeaseExpiresAt.After(snapshot.ObservedAt) || r.ProcessMetrics == nil || r.ProcessMetrics.Validate() != nil || r.ProcessStartedAt == nil || r.ProcessStartedAt.IsZero() || r.ProcessStartedAt.After(snapshot.ObservedAt) || instances[r.InstanceID] {
			return result, metricsdiscovery.ErrIdentityUnavailable
		}
		instances[r.InstanceID] = true
		resolved, err := o.policy.Resolve(ctx, r.ProcessMetrics.Endpoint)
		if err != nil {
			return result, err
		}
		if physical[resolved.Address] {
			return result, resourcequery.ErrUnavailable
		}
		physical[resolved.Address] = true
		scope := resourcequery.ProcessScope{ID: uint(len(result.scopes) + 1), ModuleName: r.ModuleName, InstanceID: r.InstanceID, Role: r.Role, Instance: resolved.Address, StartedAt: *r.ProcessStartedAt}
		if scope.Validate() != nil {
			return result, metricsdiscovery.ErrIdentityUnavailable
		}
		result.scopes = append(result.scopes, scope)
		result.instances = append(result.instances, r.InstanceID)
		if result.lease.IsZero() || r.LeaseExpiresAt.Before(result.lease) {
			result.lease = r.LeaseExpiresAt
		}
	}
	if len(result.scopes) == 0 {
		return result, metricsdiscovery.ErrUnconfigured
	}
	encoded, _ := json.Marshal([]any{policyVersion, result.scopes, result.collectionVersions})
	result.signature = fmt.Sprintf("%x", sha256.Sum256(encoded))
	return result, nil
}

// Hold the same lock as Save only while committing a verdict. A pause
// committed after the query started cannot publish stale trigger/recovery evidence.
func (s runtimeAlertSources) collectionCurrent(tx *gorm.DB) (bool, error) {
	if len(s.collectionVersions) == 0 {
		return true, nil
	}
	if err := repository.LockAutomaticCollection(tx); err != nil {
		return false, err
	}
	current, err := repository.NewAutomaticCollectionRepository(tx).Snapshot(tx.Statement.Context)
	if err != nil {
		return false, err
	}
	for id, version := range s.collectionVersions {
		if current[id].Version != version {
			return false, nil
		}
	}
	return true, nil
}
