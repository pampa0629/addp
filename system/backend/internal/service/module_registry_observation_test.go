package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newObservationRegistry(t *testing.T) (*ModuleRegistryService, *repository.ModuleRegistryRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewModuleRegistryRepository(db)
	return NewModuleRegistryService(repo), repo, db
}

func TestGatewayIngressIsObservableButNeverRoutable(t *testing.T) {
	registry, _, _ := newObservationRegistry(t)
	startedAt := time.Now().Add(-26 * time.Hour).UTC().Truncate(time.Second)
	request := &models.ModuleRegistrationRequest{
		ModuleName: "gateway", InstanceID: "gateway-a", Role: models.ModuleRuntimeRoleIngress,
		ModuleURL: "http://gateway:8000", HealthCheckURL: "http://gateway:8000/health/ready",
		ProcessStartedAt: startedAt,
	}
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	module, err := registry.GetModule("gateway")
	if err != nil {
		t.Fatal(err)
	}
	if !module.Enabled || module.RoutePrefix != "" || len(module.Instances) != 1 ||
		module.Instances[0].Role != models.ModuleRuntimeRoleIngress ||
		module.Instances[0].ProcessStartedAt == nil ||
		!module.Instances[0].ProcessStartedAt.Equal(startedAt) {
		t.Fatalf("gateway observation = %#v", module)
	}
	active, err := registry.ListActiveModules()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("gateway ingress appeared in routing snapshot: %#v", active)
	}
	for _, invalid := range []*models.ModuleRegistrationRequest{
		{ModuleName: "manager", InstanceID: "other-ingress", Role: models.ModuleRuntimeRoleIngress, ModuleURL: "http://manager", ProcessStartedAt: startedAt},
		{ModuleName: "gateway", InstanceID: "gateway-backend", Role: models.ModuleRuntimeRoleBackend, ModuleURL: "http://gateway", RoutePrefix: "/gateway", ProcessStartedAt: startedAt},
		{ModuleName: "gateway", InstanceID: "gateway-routed", Role: models.ModuleRuntimeRoleIngress, ModuleURL: "http://gateway", RoutePrefix: "/gateway", ProcessStartedAt: startedAt},
	} {
		if err := registry.Register(invalid); !errors.Is(err, ErrInvalidModuleRegistration) {
			t.Fatalf("invalid ingress registration error = %v", err)
		}
	}
	if _, err := registry.UpdateModuleDefinition("gateway", &models.ModuleDefinitionUpdateRequest{Enabled: boolPointer(false), Version: module.Version}); !errors.Is(err, ErrBootstrapModuleImmutable) {
		t.Fatalf("gateway disable error = %v", err)
	}
}

func TestRuntimeObservationDistinguishesGracefulFromLeaseExpiry(t *testing.T) {
	registry, repo, db := newObservationRegistry(t)
	startedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	request := &models.ModuleRegistrationRequest{
		ModuleName: "manager", InstanceID: "manager-a", Role: models.ModuleRuntimeRoleBackend,
		ModuleURL: "http://manager:8081", RoutePrefix: "/manager", ProcessStartedAt: startedAt,
	}
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	if err := registry.Deregister("manager", "manager-a"); err != nil {
		t.Fatal(err)
	}
	module, err := registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	instance := module.Instances[0]
	if instance.Status != models.ModuleRuntimeStatusDown || instance.StopReason != models.ModuleRuntimeStopGraceful || instance.StoppedAt == nil {
		t.Fatalf("graceful observation = %#v", instance)
	}
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	module, err = registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	instance = module.Instances[0]
	if instance.Status != models.ModuleRuntimeStatusUp || instance.StopReason != "" || instance.StoppedAt != nil {
		t.Fatalf("re-registered observation = %#v", instance)
	}
	if _, err := repo.MarkStaleModules(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	module, err = registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	instance = module.Instances[0]
	if instance.Status != models.ModuleRuntimeStatusDown || instance.StopReason != models.ModuleRuntimeStopExpired ||
		instance.StoppedAt == nil || !instance.StoppedAt.Equal(instance.LeaseExpiresAt) {
		t.Fatalf("expired observation = %#v", instance)
	}
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", request.InstanceID).
		Update("lease_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := registry.Deregister("manager", request.InstanceID); err != nil {
		t.Fatal(err)
	}
	module, err = registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	instance = module.Instances[0]
	if instance.StopReason != models.ModuleRuntimeStopExpired || instance.StoppedAt == nil ||
		!instance.StoppedAt.Equal(instance.LeaseExpiresAt) {
		t.Fatalf("late deregistration observation = %#v", instance)
	}
}

func boolPointer(value bool) *bool { return &value }

func TestRuntimeNodesLocateWorkersWithoutChangingRoutingFacts(t *testing.T) {
	registry, repo, _ := newObservationRegistry(t)
	request := &models.ModuleRegistrationRequest{ModuleName: "meta", InstanceID: "worker-a", Role: models.ModuleRuntimeRoleWorker,
		RoutePrefix: "/meta", ProcessStartedAt: time.Now(), HostNodeName: " host-a ", RuntimeHostname: " container-a "}
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	before, _ := registry.GetModule("meta")
	revision, err := repo.GetRegistryRevision()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HOST-A", "container-a"} {
		rows, total, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{NodeName: name, Role: "worker", Page: 1, PageSize: 10})
		if err != nil || total != 1 || len(rows) != 1 || rows[0].HostNodeName != "host-a" || rows[0].RuntimeHostname != "container-a" || rows[0].RegisteredHost != "" {
			t.Fatalf("node query %q: rows=%#v total=%d err=%v", name, rows, total, err)
		}
	}
	_, total, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{NodeName: "host-a", ModuleName: "manager", Page: 1, PageSize: 10})
	if err != nil || total != 0 {
		t.Fatalf("node filter escaped module condition: total=%d err=%v", total, err)
	}
	request.HostNodeName = "host-b"
	request.RuntimeHostname = "container-b"
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	after, _ := registry.GetModule("meta")
	afterRevision, err := repo.GetRegistryRevision()
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || afterRevision != revision || after.Instances[0].RuntimeHostname != "container-b" {
		t.Fatalf("node-only change altered routing/version: before=%#v after=%#v revisions=%d/%d", before, after, revision, afterRevision)
	}
	request.HostNodeName = ""
	request.RuntimeHostname = ""
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	after, _ = registry.GetModule("meta")
	if after.Instances[0].HostNodeName != "" || after.Instances[0].RuntimeHostname != "" {
		t.Fatal("unknown node kept stale identity")
	}

	backend := &models.ModuleRegistrationRequest{ModuleName: "meta", InstanceID: "backend-a", Role: models.ModuleRuntimeRoleBackend,
		RoutePrefix: "/meta", ModuleURL: "http://meta.local:8082", ProcessStartedAt: time.Now(), HostNodeName: "host-a"}
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	before, _ = registry.GetModule("meta")
	revision, err = repo.GetRegistryRevision()
	if err != nil {
		t.Fatal(err)
	}
	backend.HostNodeName = "host-b"
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	after, _ = registry.GetModule("meta")
	afterRevision, err = repo.GetRegistryRevision()
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || afterRevision != revision {
		t.Fatal("backend node change altered module version or routing revision")
	}
}
