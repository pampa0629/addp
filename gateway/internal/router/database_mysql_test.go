package router

import (
	"os"
	"testing"

	"github.com/addp/gateway/internal/config"
	"github.com/addp/gateway/internal/middleware"
)

func TestGatewayMySQLRuntime(t *testing.T) {
	if os.Getenv("ADDP_TEST_GATEWAY_MYSQL") != "1" {
		t.Skip("requires the disposable platform-gateway-install gate")
	}
	db := initDatabase(config.Load())
	if db == nil {
		t.Fatal("Gateway disabled access logging on the installed database")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var target string
	if err := db.Raw("SELECT DATABASE()").Row().Scan(&target); err != nil || target != "gateway" {
		t.Fatalf("database=%q err=%v", target, err)
	}
	log := middleware.AccessLog{ServiceName: "gateway-install-probe", RequestParams: "{}", ResponseStatus: 200}
	if err := db.Create(&log).Error; err != nil {
		t.Fatal(err)
	}
}
