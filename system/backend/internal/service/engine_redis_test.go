package service

import (
	"encoding/json"
	"net"
	"os"
	"strconv"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
)

func verifyRedisRegistration(t *testing.T, connection models.ConnectionInfo) (*EngineService, uint) {
	t.Helper()
	repo := newEngineServiceTestRepository(t)
	s := NewEngineService(repo, []byte("addp-dev-encryption-key-2025!!!!"), nil)
	descriptors, err := engineplugin.ListEngineTypeDescriptors("general")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range descriptors {
		if d.Type == "redis" {
			found = true
			if d.Capabilities.EngineFamily != "key_value" || d.CatalogModel == nil || d.CatalogModel.RootTerm != "server" || d.CatalogModel.Levels[0].Term != "key" {
				t.Fatal("invalid Redis descriptor")
			}
		}
	}
	if !found {
		t.Fatal("Redis absent from compiled general engine types")
	}
	req := &models.EngineCreateRequest{Name: "Redis fixture", EngineType: "redis", EngineOrigin: "general", ConnectionInfo: connection}
	created, isNew, err := s.Create(req, 42, 7)
	if err != nil || !isNew {
		t.Fatalf("create Redis: %v", err)
	}
	if created.ConnectionInfo["password"] != "******" {
		t.Fatal("public connection must contain only the standard password mask")
	}
	stored, err := repo.GetByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectionInfo["password"] == connection["password"] {
		t.Fatal("stored password is plaintext")
	}
	plain, err := s.decryptStoredConnectionInfo("redis", stored.ConnectionInfo)
	if err != nil {
		t.Fatal(err)
	}
	if plain["password"] != connection["password"] {
		t.Fatal("decrypted password differs")
	}
	var caps engineplugin.EngineCapabilities
	if err := json.Unmarshal([]byte(*stored.Capabilities), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.EngineFamily != "key_value" || caps.Storage == nil || caps.Storage.Catalog == nil || caps.Storage.Facts == nil || caps.Storage.Store == nil || !caps.Storage.Store.KeyValueRead || caps.Storage.Store.TableReadSession || caps.Compute != nil {
		t.Fatal("persisted unsupported access capabilities")
	}
	again, isNew, err := s.Create(req, 42, 7)
	if err != nil || isNew || again.ID != created.ID {
		t.Fatalf("registration was not idempotent: %v", err)
	}
	return s, created.ID
}

func TestRedisRegistrationStoresNativeKeyCapabilities(t *testing.T) {
	verifyRedisRegistration(t, models.ConnectionInfo{"host": "redis.invalid", "user": "default", "password": "secret"})
}

func TestIntegrationRedisSystemConnection(t *testing.T) {
	if os.Getenv("ADDP_REDIS_INTEGRATION") != "1" {
		t.Skip("run make test-common-redis")
	}
	host, port, err := net.SplitHostPort(os.Getenv("ADDP_REDIS_T2_ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(port)
	s, id := verifyRedisRegistration(t, models.ConnectionInfo{"host": host, "port": n, "database": 0, "user": "addp_business_reader", "password": os.Getenv("BUSINESS_REDIS_READER_PASSWORD")})
	if !s.CheckAndUpdateConnectionStatus(id) {
		t.Fatal("System connection probe failed")
	}
	stored, err := s.repo.GetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectionStatus != models.EngineConnectionOnline {
		t.Fatal("System did not persist online observation")
	}
	if _, err := s.GetForExecution(id, 7); err != nil {
		t.Fatal(err)
	}
	capabilities := *stored.Capabilities
	stored.ConnectionInfo, err = s.encryptConnectionInfoForStorage("redis", models.ConnectionInfo{
		"host": host, "port": n, "database": 0, "user": "addp_business_reader", "password": "incorrect",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Update(stored); err != nil {
		t.Fatal(err)
	}
	if s.CheckAndUpdateConnectionStatus(id) {
		t.Fatal("System marked wrong credentials online")
	}
	stored, err = s.repo.GetByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectionStatus != models.EngineConnectionOffline || *stored.Capabilities != capabilities {
		t.Fatal("failed authentication did not preserve capabilities and mark the engine offline")
	}
	if _, err := s.GetForExecution(id, 7); err == nil {
		t.Fatal("offline Redis engine remained executable")
	}
}
