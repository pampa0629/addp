package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
)

func deploymentTestInput() models.EngineDeploymentInput {
	enabled := true
	return models.EngineDeploymentInput{DisplayName: "Business PostgreSQL", Enabled: &enabled, DeclaredNodeID: "10000000-0000-4000-8000-000000000001",
		Inventory: models.EngineDeploymentInventory{Schema: "addp.engine-deployment-inventory/v1", ObservedAt: "2026-10-19T00:00:00Z",
			Source:   models.EngineDeploymentSource{Type: "docker_compose", Runtime: "desktop_linux_vm", DockerEngineID: "source-daemon", Project: "business"},
			Services: []models.EngineDeploymentService{{Service: "postgres", EngineType: "postgresql", Containers: []models.EngineDeploymentContainer{{ContainerID: strings.Repeat("a", 64), StartedAt: "2026-10-18T00:00:00.123456789Z", PublishedPorts: []models.EngineDeploymentPort{{Address: "::", Port: 5433, ContainerPort: 5432, Protocol: "tcp"}}}}}}}}
}

func TestEngineDeploymentDeclarationValidation(t *testing.T) {
	input := deploymentTestInput()
	value, err := validateEngineDeployment(input)
	if err != nil || value.EngineType != "postgresql" || value.DeploymentID != "" || value.DeclaredNodeID != input.DeclaredNodeID {
		t.Fatalf("valid declaration=%+v err=%v", value, err)
	}
	mapped := deploymentTestInput()
	mapped.Inventory.Services[0].Containers[0].PublishedPorts[0].Address = "::ffff:c000:201"
	if _, err := validateEngineDeployment(mapped); err != nil {
		t.Fatalf("Python ipaddress canonical mapped IPv6 rejected: %v", err)
	}

	mutations := map[string]func(*models.EngineDeploymentInput){
		"blank name":        func(v *models.EngineDeploymentInput) { v.DisplayName = " " },
		"control name":      func(v *models.EngineDeploymentInput) { v.DisplayName = "db\x00" },
		"long name":         func(v *models.EngineDeploymentInput) { v.DisplayName = strings.Repeat("中", 256) },
		"missing intent":    func(v *models.EngineDeploymentInput) { v.Enabled = nil },
		"missing host":      func(v *models.EngineDeploymentInput) { v.DeclaredNodeID = "" },
		"zero host":         func(v *models.EngineDeploymentInput) { v.DeclaredNodeID = "00000000-0000-0000-0000-000000000000" },
		"legacy schema":     func(v *models.EngineDeploymentInput) { v.Inventory.Schema = "addp.host-admission/v2" },
		"not a runtime":     func(v *models.EngineDeploymentInput) { v.Inventory.Source.Runtime = "macos" },
		"foreign owner":     func(v *models.EngineDeploymentInput) { v.Inventory.Source.Project = "infra" },
		"unsafe daemon":     func(v *models.EngineDeploymentInput) { v.Inventory.Source.DockerEngineID = "x\n" },
		"bad observation":   func(v *models.EngineDeploymentInput) { v.Inventory.ObservedAt = "yesterday" },
		"missing service":   func(v *models.EngineDeploymentInput) { v.Inventory.Services = nil },
		"other engine":      func(v *models.EngineDeploymentInput) { v.Inventory.Services[0].EngineType = "mysql" },
		"another service":   func(v *models.EngineDeploymentInput) { v.Inventory.Services[0].Service = "system-postgres" },
		"missing container": func(v *models.EngineDeploymentInput) { v.Inventory.Services[0].Containers = nil },
		"multiple containers": func(v *models.EngineDeploymentInput) {
			c := v.Inventory.Services[0].Containers[0]
			v.Inventory.Services[0].Containers = append(v.Inventory.Services[0].Containers, c)
		},
		"short runtime id": func(v *models.EngineDeploymentInput) { v.Inventory.Services[0].Containers[0].ContainerID = "short" },
		"bad start": func(v *models.EngineDeploymentInput) {
			v.Inventory.Services[0].Containers[0].StartedAt = "0001-01-01T00:00:00Z"
		},
		"negative restarts": func(v *models.EngineDeploymentInput) { v.Inventory.Services[0].Containers[0].RestartCount = -1 },
		"missing port":      func(v *models.EngineDeploymentInput) { v.Inventory.Services[0].Containers[0].PublishedPorts = nil },
		"bad address": func(v *models.EngineDeploymentInput) {
			v.Inventory.Services[0].Containers[0].PublishedPorts[0].Address = "database.example"
		},
		"bad port": func(v *models.EngineDeploymentInput) {
			v.Inventory.Services[0].Containers[0].PublishedPorts[0].Port = 65536
		},
		"wrong target": func(v *models.EngineDeploymentInput) {
			v.Inventory.Services[0].Containers[0].PublishedPorts[0].ContainerPort = 5433
		},
		"udp": func(v *models.EngineDeploymentInput) {
			v.Inventory.Services[0].Containers[0].PublishedPorts[0].Protocol = "udp"
		},
		"duplicate mapping": func(v *models.EngineDeploymentInput) {
			c := &v.Inventory.Services[0].Containers[0]
			c.PublishedPorts = append(c.PublishedPorts, c.PublishedPorts[0])
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := deploymentTestInput()
			mutate(&v)
			if _, err := validateEngineDeployment(v); !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("accepted invalid declaration: %v", err)
			}
		})
	}
	encoded, _ := json.Marshal(value)
	for _, key := range []string{"tenant_id", "engine_id", "password", "connection_info"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Fatalf("unexpected business identity or credential field %s", key)
		}
	}
}
