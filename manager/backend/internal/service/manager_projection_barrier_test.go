package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type dataProfileBarrierCleaner struct {
	calls map[int64][][]string
}

func (c *dataProfileBarrierCleaner) DeleteByItemFingerprints(_ context.Context, _ *gorm.DB, tenantID int64, fingerprints []string) error {
	if c.calls == nil {
		c.calls = make(map[int64][][]string)
	}
	c.calls[tenantID] = append(c.calls[tenantID], append([]string(nil), fingerprints...))
	return nil
}

type dataProfileBarrierExecutionCleaner struct {
	calls map[int64][][]string
}

type contentSearchBarrierCleaner struct {
	calls map[uint][]string
}

func (c *contentSearchBarrierCleaner) DeleteContentDocument(_ context.Context, tenantID uint, fingerprint string) error {
	if c.calls == nil {
		c.calls = make(map[uint][]string)
	}
	c.calls[tenantID] = append(c.calls[tenantID], fingerprint)
	return nil
}

func (c *dataProfileBarrierExecutionCleaner) SuppressConditionalScopesByItemFingerprints(_ context.Context, _ *gorm.DB, tenantID int64, fingerprints []string) error {
	if c.calls == nil {
		c.calls = make(map[int64][][]string)
	}
	c.calls[tenantID] = append(c.calls[tenantID], append([]string(nil), fingerprints...))
	return nil
}

func TestManagerProjectionBarrierConvergesUpsertAndReleaseTargetsOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := &dataProfileBarrierCleaner{}
	executions := &dataProfileBarrierExecutionCleaner{}
	search := &contentSearchBarrierCleaner{}
	barrier := NewManagerProjectionBarrier(profiles, executions, search)
	targetA := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-a"}
	targetB := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-b"}
	changes := []dataprotection.ProjectionChange{
		{Projection: &dataprotection.Projection{Target: targetB}},
		{Release: &dataprotection.ProjectionRelease{Target: targetA}},
		{Projection: &dataprotection.Projection{Target: targetB}},
		{Projection: &dataprotection.Projection{Target: dataprotection.ResourceReference{OwnerModule: "catalog", ResourceType: "entry", ResourceIdentity: "ignored"}}},
	}
	if err := barrier.ApplyProjectionChanges(context.Background(), db, 7, changes, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"item-a", "item-b"}}
	if !reflect.DeepEqual(profiles.calls[7], want) || !reflect.DeepEqual(executions.calls[7], want) {
		t.Fatalf("profile calls = %#v, execution calls = %#v", profiles.calls, executions.calls)
	}
	if !reflect.DeepEqual(search.calls[7], []string{"item-a", "item-b"}) {
		t.Fatalf("search calls = %#v", search.calls)
	}
}

func TestManagerProjectionBarrierReconcilesInstalledTargetsByTenant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := &dataProfileBarrierCleaner{}
	executions := &dataProfileBarrierExecutionCleaner{}
	search := &contentSearchBarrierCleaner{}
	barrier := NewManagerProjectionBarrier(profiles, executions, search)
	targets := []projectionstore.ManagedTarget{
		{TenantID: 8, Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-c"}},
		{TenantID: 7, Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-b"}},
		{TenantID: 7, Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-a"}},
	}
	if err := barrier.ReconcileInstalled(context.Background(), db, targets); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profiles.calls[7], [][]string{{"item-a", "item-b"}}) || !reflect.DeepEqual(profiles.calls[8], [][]string{{"item-c"}}) {
		t.Fatalf("profile calls = %#v", profiles.calls)
	}
	if !reflect.DeepEqual(executions.calls, profiles.calls) {
		t.Fatalf("execution calls = %#v, profile calls = %#v", executions.calls, profiles.calls)
	}
	if !reflect.DeepEqual(search.calls[7], []string{"item-a", "item-b"}) || !reflect.DeepEqual(search.calls[8], []string{"item-c"}) {
		t.Fatalf("search calls = %#v", search.calls)
	}
}
