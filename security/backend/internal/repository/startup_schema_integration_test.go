package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/addp/security/internal/config"
)

func TestSecurityPreparedRuntime(t *testing.T) {
	mode := os.Getenv("ADDP_TEST_SECURITY_PREPARED_RUNTIME")
	if mode == "" {
		t.Skip("requires an isolated installation and runtime identity")
	}
	db, err := OpenRuntime(config.Load().PlatformDatabase())
	if mode == "missing" {
		if err == nil || !strings.Contains(err.Error(), "security") {
			t.Fatalf("missing Security revision: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var rows int64
	if err := db.Table("security.protection_access_requests").Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
}
