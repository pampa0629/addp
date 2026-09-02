package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"gorm.io/gorm"
)

type dataProfileResultCleaner interface {
	DeleteByItemFingerprints(context.Context, *gorm.DB, int64, []string) error
}

type dataProfileExecutionCleaner interface {
	SuppressConditionalScopesByItemFingerprints(context.Context, *gorm.DB, int64, []string) error
}

type contentSearchProjectionCleaner interface {
	DeleteContentDocument(context.Context, uint, string) error
}

// ManagerProjectionBarrier makes every value-bearing derived projection
// subordinate to the locally installed protection revision.
type ManagerProjectionBarrier struct {
	profiles   dataProfileResultCleaner
	executions dataProfileExecutionCleaner
	search     contentSearchProjectionCleaner
}

func NewManagerProjectionBarrier(profiles dataProfileResultCleaner, executions dataProfileExecutionCleaner, search contentSearchProjectionCleaner) *ManagerProjectionBarrier {
	return &ManagerProjectionBarrier{profiles: profiles, executions: executions, search: search}
}

func (b *ManagerProjectionBarrier) ApplyProjectionChanges(
	ctx context.Context,
	tx *gorm.DB,
	tenantID int64,
	changes []dataprotection.ProjectionChange,
	_ time.Time,
) error {
	if b == nil || b.profiles == nil || b.executions == nil || b.search == nil || tx == nil || tenantID <= 0 {
		return errors.New("manager protection projection barrier is unavailable")
	}
	fingerprints := make(map[string]struct{})
	for _, change := range changes {
		target, ok := managerProjectionChangeTarget(change)
		if !ok {
			continue
		}
		fingerprints[target.ResourceIdentity] = struct{}{}
	}
	return b.converge(ctx, tx, tenantID, sortedFingerprintSet(fingerprints))
}

// ReconcileInstalled removes material created before the profile executor and
// transaction barrier existed. It runs before Manager serves requests.
func (b *ManagerProjectionBarrier) ReconcileInstalled(
	ctx context.Context,
	db *gorm.DB,
	targets []projectionstore.ManagedTarget,
) error {
	if b == nil || b.profiles == nil || b.executions == nil || b.search == nil || db == nil {
		return errors.New("manager protection projection barrier is unavailable")
	}
	byTenant := make(map[int64]map[string]struct{})
	for _, installed := range targets {
		if !isManagerDataItemProjectionTarget(installed.Target) || installed.TenantID <= 0 {
			continue
		}
		if byTenant[installed.TenantID] == nil {
			byTenant[installed.TenantID] = make(map[string]struct{})
		}
		byTenant[installed.TenantID][installed.Target.ResourceIdentity] = struct{}{}
	}
	if len(byTenant) == 0 {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tenantIDs := make([]int64, 0, len(byTenant))
		for tenantID := range byTenant {
			tenantIDs = append(tenantIDs, tenantID)
		}
		sort.Slice(tenantIDs, func(i, j int) bool { return tenantIDs[i] < tenantIDs[j] })
		for _, tenantID := range tenantIDs {
			if err := b.converge(ctx, tx, tenantID, sortedFingerprintSet(byTenant[tenantID])); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b *ManagerProjectionBarrier) converge(ctx context.Context, tx *gorm.DB, tenantID int64, fingerprints []string) error {
	if len(fingerprints) == 0 {
		return nil
	}
	if err := b.profiles.DeleteByItemFingerprints(ctx, tx, tenantID, fingerprints); err != nil {
		return err
	}
	if err := b.executions.SuppressConditionalScopesByItemFingerprints(ctx, tx, tenantID, fingerprints); err != nil {
		return err
	}
	for _, fingerprint := range fingerprints {
		if err := b.search.DeleteContentDocument(ctx, uint(tenantID), fingerprint); err != nil {
			return err
		}
	}
	return nil
}

func managerProjectionChangeTarget(change dataprotection.ProjectionChange) (dataprotection.ResourceReference, bool) {
	switch {
	case change.Projection != nil:
		return change.Projection.Target, isManagerDataItemProjectionTarget(change.Projection.Target)
	case change.Release != nil:
		return change.Release.Target, isManagerDataItemProjectionTarget(change.Release.Target)
	default:
		return dataprotection.ResourceReference{}, false
	}
}

func isManagerDataItemProjectionTarget(target dataprotection.ResourceReference) bool {
	return target.OwnerModule == "meta" && target.ResourceType == "data_item" && strings.TrimSpace(target.ResourceIdentity) != ""
}

func sortedFingerprintSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
