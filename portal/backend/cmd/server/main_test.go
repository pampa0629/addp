package main

import (
	"context"
	"testing"
)

type recordingPortalModuleRegistrar struct {
	moduleName  string
	moduleURL   string
	routePrefix string
	metadata    map[string]interface{}
}

func (r *recordingPortalModuleRegistrar) RegisterAndHeartbeatWithMetadata(
	_ context.Context,
	moduleName string,
	moduleURL string,
	routePrefix string,
	metadata map[string]interface{},
) {
	r.moduleName = moduleName
	r.moduleURL = moduleURL
	r.routePrefix = routePrefix
	r.metadata = metadata
}

func TestRegisterPortalModuleUsesCanonicalRuntimeRegistration(t *testing.T) {
	t.Setenv("SERVICE_HOST", "portal-runtime")
	registrar := &recordingPortalModuleRegistrar{}

	registerPortalModule(context.Background(), registrar, "8184")

	if registrar.moduleName != "portal" || registrar.moduleURL != "http://portal-runtime:8184" ||
		registrar.routePrefix != "/portal" || registrar.metadata != nil {
		t.Fatalf(
			"Portal registration = (%q, %q, %q, %#v)",
			registrar.moduleName, registrar.moduleURL, registrar.routePrefix, registrar.metadata,
		)
	}
}
