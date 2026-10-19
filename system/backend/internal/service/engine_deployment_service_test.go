package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestEngineDeploymentAdmissionUsesFreshServerFacts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	enabled := true
	fresh := func() (models.EngineDeploymentAdmissionRequest, models.DeploymentDiscovery) {
		scope := discoveryTestValue(now)
		service := scope.Services[0]
		item := models.EngineDeploymentAdmissionItem{Scope: "infra", Service: "postgres", Fingerprint: deploymentFingerprint("infra", scope.Source, service), DisplayName: "Infra PostgreSQL", Enabled: &enabled, DeclaredNodeID: "10000000-0000-4000-8000-000000000001"}
		return models.EngineDeploymentAdmissionRequest{Items: []models.EngineDeploymentAdmissionItem{item}}, models.DeploymentDiscovery{Scopes: []models.DeploymentDiscoveryScope{scope}}
	}
	input, discovery := fresh()
	values, err := buildDeploymentAdmissions(input, discovery, now)
	if err != nil || len(values) != 1 || values[0].Inventory.Source.Project != "addp-infra" || values[0].Inventory.Schema != "addp.engine-deployment-inventory/v1" {
		t.Fatalf("admission=%+v %v", values, err)
	}
	for _, kind := range []string{"expired", "changed", "missing", "stopped", "duplicate", "empty", "host"} {
		t.Run(kind, func(t *testing.T) {
			input, discovery := fresh()
			want := ErrDeploymentSourceChanged
			switch kind {
			case "expired":
				discovery.Scopes[0].ObservedAt = now.Add(-121 * time.Second).Format(time.RFC3339)
			case "changed":
				discovery.Scopes[0].Services[0].Containers[0].RestartCount++
			case "missing":
				discovery.Scopes = nil
			case "stopped":
				discovery.Scopes[0].Services[0].Containers[0].Status = "exited"
			case "duplicate":
				input.Items = append(input.Items, input.Items[0])
				want = commonapi.ErrBadRequest
			case "empty":
				input.Items = nil
				want = commonapi.ErrBadRequest
			case "host":
				input.Items[0].DeclaredNodeID = ""
				want = commonapi.ErrBadRequest
			}
			if _, err := buildDeploymentAdmissions(input, discovery, now); !errors.Is(err, want) {
				t.Fatalf("accepted invalid admission: %v", err)
			}
		})
	}
	input, discovery = fresh()
	discovery.Scopes[0].ObservedAt = now.Add(time.Second).Format(time.RFC3339)
	if _, err := buildDeploymentAdmissions(input, discovery, now); err != nil {
		t.Fatalf("timestamp-only refresh invalidated facts: %v", err)
	}
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
