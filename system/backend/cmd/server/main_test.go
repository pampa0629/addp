package main

import (
	"testing"

	commonconfiguration "github.com/addp/common/configuration"
)

func TestSystemRegistrationDeclarationIsValid(t *testing.T) {
	request := newSystemRegistrationRequest("http://localhost:8180", "system-instance")
	if request.ModuleName != "system" || request.Role != "backend" || request.ModuleURL != "http://localhost:8180" {
		t.Fatalf("unexpected System registration endpoint: %+v", request)
	}
	if err := commonconfiguration.ValidateManagementDeclaration(request.ModuleName, request.ConfigurationManagement); err != nil {
		t.Fatalf("System configuration management declaration is invalid: %v", err)
	}
	entries := request.ConfigurationManagement.Entries
	if len(entries) != 1 || entries[0].FrontendRoute != "/system/iam/security" {
		t.Fatalf("unexpected System configuration management route: %+v", entries)
	}
}
