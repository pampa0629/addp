package config

import (
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

func TestDeploymentConfiguration(t *testing.T) {
	t.Setenv("ONTOLOGY_BACKEND_PORT", "8195")
	t.Setenv("ONTOLOGY_SERVICE_CLIENT_SECRET", strings.Repeat("s", 32))
	t.Setenv("INFRA_FALKORDB_ADDRESS", "127.0.0.1:16479")
	t.Setenv("INFRA_FALKORDB_PASSWORD", "private-password")
	t.Setenv("SYSTEM_URL", "http://localhost:8180")
	c := fromEnvironment()
	c.DBHost = "127.0.0.1"
	c.DBPort = "15432"
	c.DBName = "addp_test"
	c.DBUser = "addp"
	c.DBPassword = "quote' slash\\ @ : secret"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	parsed, err := pgx.ParseConfig(c.DatabaseDSN())
	if err != nil || parsed.Password != c.DBPassword || parsed.RuntimeParams["search_path"] != "ontology" {
		t.Fatal("DSN encoding or schema mismatch")
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
