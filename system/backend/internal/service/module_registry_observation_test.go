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
