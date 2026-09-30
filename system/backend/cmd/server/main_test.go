package main

import (
	"os"
	"testing"

	commonconfiguration "github.com/addp/common/configuration"
)

func TestSystemRegistrationDeclarationIsValid(t *testing.T) {
	t.Setenv("ADDP_HOST_NODE_NAME", "system-host")
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	request := newSystemRegistrationRequest("http://localhost:8180", "system-instance")
	if request.ModuleName != "system" || request.Role != "backend" || request.ModuleURL != "http://localhost:8180" {
		t.Fatalf("unexpected System registration endpoint: %+v", request)
	}
	if err := commonconfiguration.ValidateManagementDeclaration(request.ModuleName, request.ConfigurationManagement); err != nil {
		t.Fatalf("System configuration management declaration is invalid: %v", err)
	}
	if request.HostNodeName != "system-host" || request.RuntimeHostname != hostname {
		t.Fatalf("System node identity = %#v", request)
	}
	entries := request.ConfigurationManagement.Entries
	if len(entries) != 1 || entries[0].FrontendRoute != "/system/iam/security" {
		t.Fatalf("unexpected System configuration management route: %+v", entries)
	}
}
