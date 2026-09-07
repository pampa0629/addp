package mongodb

import (
	"os"
	"strconv"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func mongoIntegrationConnectionInfo(t *testing.T, database string) plugin.ConnectionInfo {
	t.Helper()
	portText := mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_PORT", "27017")
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		t.Fatalf("invalid ADDP_TEST_MONGODB_PORT %q", portText)
	}
	connectionInfo := plugin.ConnectionInfo{
		"host":        mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_HOST", "127.0.0.1"),
		"port":        port,
		"user":        mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_USER", "admin"),
		"password":    mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_PASSWORD", "admin_password"),
		"auth_source": mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_AUTH_SOURCE", "admin"),
	}
	if database != "" {
		connectionInfo["database"] = database
	}
	return connectionInfo
}

func mongoIntegrationEnvOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func TestMongoIntegrationConnectionInfoUsesTestEnvironment(t *testing.T) {
	t.Setenv("ADDP_TEST_MONGODB_HOST", "mongo-ci")
	t.Setenv("ADDP_TEST_MONGODB_PORT", "37017")
	t.Setenv("ADDP_TEST_MONGODB_USER", "addp_ci")
	t.Setenv("ADDP_TEST_MONGODB_PASSWORD", "addp_ci_password")
	t.Setenv("ADDP_TEST_MONGODB_AUTH_SOURCE", "ci_admin")

	got := mongoIntegrationConnectionInfo(t, "Outdoor")
	want := plugin.ConnectionInfo{
		"host":        "mongo-ci",
		"port":        37017,
		"user":        "addp_ci",
		"password":    "addp_ci_password",
		"auth_source": "ci_admin",
		"database":    "Outdoor",
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("connection info[%q] = %#v, want %#v", key, got[key], wantValue)
		}
	}
}
