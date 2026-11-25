package config

import (
	"strings"
	"testing"
	"time"
)

func TestDeploymentConfiguration(t *testing.T) {
	t.Setenv("ONTOLOGY_BACKEND_PORT", "8195")
	t.Setenv("ONTOLOGY_SERVICE_CLIENT_SECRET", strings.Repeat("s", 32))
	t.Setenv("INFRA_FALKORDB_ADDRESS", "127.0.0.1:16479")
	t.Setenv("INFRA_FALKORDB_PASSWORD", "private-password")
	t.Setenv("SYSTEM_URL", "http://localhost:8180")
	c := fromEnvironment()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POSTGRES_HOST", "db.internal")
	db := PlatformDatabase()
	if db.Host != "db.internal" || db.ConnectTimeout != 5*time.Second || db.Schema != "ontology" || db.User != "" || db.Password != "" {
		t.Fatal("platform database settings do not match the installation contract")
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.Port = "0" }, func(c *Config) { c.Port = "8195x" }, func(c *Config) { c.ServiceClientSecret = "" }, func(c *Config) { c.FalkorPassword = "" }, func(c *Config) { c.FalkorAddress = "localhost:0" }, func(c *Config) { c.SystemURL = "http://user:secret@localhost:8180" }} {
		bad := *c
		mutate(&bad)
		err := bad.Validate()
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("configuration must fail safely")
		}
	}
}
