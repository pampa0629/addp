package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/dataprotection/projectionstore"
	"gorm.io/gorm"
)

type protectionInitializationReader interface {
	Initialized(context.Context, int64) (bool, error)
}

// This fixture observes the Owner's durable checkpoint. Only the running Owner
// may consume Security changes and commit initialization; no test writes it.
func waitForOwnerProtection(ctx context.Context, db *gorm.DB, owners []string, tenantID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	readers := make(map[string]protectionInitializationReader, len(owners))
	for _, owner := range owners {
		store, err := projectionstore.Open(db.WithContext(ctx), owner, owner, nil)
		if err != nil {
			return fmt.Errorf("open %s protection readiness for Tenant %d: %w", owner, tenantID, err)
		}
		readers[owner] = store
	}
	if err := waitProtectionInitialization(ctx, readers, tenantID, time.Second); err != nil {
		return err
	}
	fmt.Printf("Protection initialization verified for Tenant %d, Owners %s\n", tenantID, strings.Join(owners, ","))
	return nil
}

func waitProtectionInitialization(ctx context.Context, readers map[string]protectionInitializationReader, tenantID int64, interval time.Duration) error {
	if tenantID <= 0 || len(readers) == 0 || interval <= 0 {
		return fmt.Errorf("protection readiness requires a Tenant, Owners and positive poll interval")
	}
	owners := make([]string, 0, len(readers))
	for owner := range readers {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("protection readiness for Tenant %d, Owners %s: %w", tenantID, strings.Join(owners, ","), err)
		}
		pending := make([]string, 0, len(owners))
		for _, owner := range owners {
			initialized, err := readers[owner].Initialized(ctx, tenantID)
			if err != nil {
				return fmt.Errorf("read %s protection readiness for Tenant %d: %w", owner, tenantID, err)
			}
			if !initialized {
				pending = append(pending, owner)
			}
		}
		if len(pending) == 0 {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("protection initialization incomplete for Tenant %d, Owners %s: %w", tenantID, strings.Join(pending, ","), ctx.Err())
		case <-timer.C:
		}
	}
}
