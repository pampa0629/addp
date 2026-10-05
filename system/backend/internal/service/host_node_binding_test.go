package service

import (
	"testing"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
)

func TestModuleNodeDeclarationsStayImmutableAndUseCurrentAuthorization(t *testing.T) {
	registry, _, db := newObservationRegistry(t)
	if err := db.AutoMigrate(&models.HostNode{}); err != nil {
		t.Fatal(err)
	}
	first := models.HostNode{NodeID: uuid.NewString(), DisplayName: "first", NodeKind: "virtual", Enabled: true, Version: 1, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{{ClientID: "addp-manager", ModuleName: "manager"}}}
	second := first
	second.NodeID = uuid.NewString()
	second.DisplayName = "second"
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	request := &models.ModuleRegistrationRequest{ModuleName: "manager", InstanceID: "a", Role: "worker", RoutePrefix: "/manager", ProcessStartedAt: time.Now().UTC(), NodeID: first.NodeID, RegistrationClientID: "addp-manager"}
	register := func() {
		t.Helper()
		if err := registry.Register(request); err != nil {
			t.Fatal(err)
		}
	}
	state := func(instance, state, reason, id string) {
		t.Helper()
		module, err := registry.GetModule("manager")
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range module.Instances {
			if value.InstanceID == instance {
				if value.Status != "up" || value.NodeBindingState != state || value.NodeBindingReason != reason || value.NodeID != id {
					t.Fatalf("instance=%+v", value)
				}
				return
			}
		}
		t.Fatalf("missing %s", instance)
	}
	register()
	state("a", "bound", "", first.NodeID)
	request.InstanceID = "b"
	request.NodeID = second.NodeID
	register()
	state("b", "bound", "", second.NodeID)
	// Recovery of the same process never overwrites its original declaration or verified source.
	request.InstanceID = "a"
	request.NodeID = second.NodeID
	request.RegistrationClientID = "addp-meta"
	register()
	state("a", "bound", "", first.NodeID)
	if err := db.Model(&first).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := registry.SendHeartbeat("manager", "a"); err != nil {
		t.Fatal(err)
	}
	state("a", "rejected", "node_disabled", "")
	if err := db.Model(&first).Updates(map[string]interface{}{"enabled": true, "allowed_module_bindings": "[]"}).Error; err != nil {
		t.Fatal(err)
	}
	state("a", "rejected", "source_not_allowed", "")
	request.InstanceID = "c"
	request.NodeID = uuid.NewString()
	request.RegistrationClientID = "addp-manager"
	register()
	state("c", "rejected", "node_unknown", "")
	request.InstanceID = "d"
	request.NodeID = ""
	register()
	state("d", "unbound", "", "")
	request.NodeID = second.NodeID
	register()
	state("d", "unbound", "", "")
	request.InstanceID = "e"
	request.NodeID = second.NodeID
	request.RegistrationClientID = "addp-meta"
	register()
	state("e", "rejected", "source_not_allowed", "")
	rows, total, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{Page: 1, PageSize: 100})
	if err != nil || total != 5 {
		t.Fatalf("rows=%+v total=%d err=%v", rows, total, err)
	}
	for _, row := range rows {
		if row.InstanceID == "a" && row.DeclaredNodeID != first.NodeID {
			t.Fatalf("lost first declaration: %+v", row)
		}
	}
}
