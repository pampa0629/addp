package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/schema"
	"github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestGatewayAccessLogAgainstMySQL(t *testing.T) {
	databaseName := os.Getenv("ADDP_TEST_MYSQL_PLATFORM_DATABASE")
	if databaseName == "" {
		t.Skip("run make test-common-mysql-data-protection with --test platform-database")
	}
	if databaseName != "addp_platform_mysql_test" && databaseName != "addp_ci" {
		t.Fatalf("unexpected disposable database %q", databaseName)
	}
	adapter, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
		DBName: databaseName, ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(adapter, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	for attempt := 0; attempt < 2; attempt++ {
		if err := MigrateAccessLog(db); err != nil {
			t.Fatal(err)
		}
	}
	if err := schema.RequireMySQL(db, "gateway", accessLogVersion); err != nil {
		t.Fatal(err)
	}

	logger := NewAccessLoggerMiddleware(db)
	path := fmt.Sprintf("/api/query/infra-test-%d/query", time.Now().UnixNano())
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST(path, func(c *gin.Context) {
		c.Set("api_consumer_info", &client.APIConsumerCredentialValidationResponse{APIConsumerID: 101})
		c.Set("api_credential_prefix", "addp_test")
	}, logger.Handler(), func(c *gin.Context) { c.Status(http.StatusTooManyRequests) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path+"?search=visible&token=sample-secret", nil))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("access logging changed the response to %d", response.Code)
	}
	var log AccessLog
	for deadline := time.Now().Add(6 * time.Second); log.ID == 0 && time.Now().Before(deadline); {
		if err := db.Table(mysqlAccessLogTable).Where("request_path = ?", path).Scan(&log).Error; err != nil {
			t.Fatal(err)
		}
		if log.ID == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if log.ID == 0 || log.APIConsumerID == nil || *log.APIConsumerID != 101 ||
		log.CredentialPrefix != "addp_test" || !log.RateLimited || log.ResponseStatus != http.StatusTooManyRequests {
		t.Fatalf("Gateway access facts were not recorded: %+v", log)
	}
	if !strings.Contains(log.RequestParams, "[REDACTED]") || !strings.Contains(log.RequestParams, "visible") || strings.Contains(log.RequestParams, "sample-secret") {
		t.Fatalf("unexpected request parameters: %s", log.RequestParams)
	}
	old := log
	old.ID = 0
	old.RequestPath += "/expired"
	old.AccessedAt = time.Now().AddDate(0, 0, -2)
	if err := db.Table(mysqlAccessLogTable).Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := logger.CleanupOldLogs(1); err != nil {
		t.Fatal(err)
	}
	var remaining []uint
	if err := db.Table(mysqlAccessLogTable).Where("id IN ?", []uint{log.ID, old.ID}).Pluck("id", &remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0] != log.ID {
		t.Fatalf("unexpected rows after cleanup: %v", remaining)
	}
}
