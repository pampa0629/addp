package capture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/testpg"
	"github.com/lib/pq"
	"github.com/twmb/franz-go/pkg/kadm"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgreSQLCaptureControlLifecycle(t *testing.T) {
	if os.Getenv("ADDP_CDC_CONTROL_E2E") != "1" {
		t.Skip("set ADDP_CDC_CONTROL_E2E=1 to run PostgreSQL CDC capture control integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	infraInfo := testpg.ConnInfoFromEnv(t)
	infraDSN, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(infraInfo)
	if err != nil {
		t.Fatal(err)
	}
	infraDB, err := gorm.Open(postgresdriver.Open(infraDSN), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := infraDB.Exec("CREATE SCHEMA IF NOT EXISTS transfer").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.MigrateCaptureProviderResources(infraDB); err != nil {
		t.Fatalf("migrate legacy capture resources: %v", err)
	}
	if err := infraDB.AutoMigrate(&models.TransferTask{}, &models.CaptureResource{}, &models.PostgreSQLCaptureResource{}, &models.MySQLCaptureResource{}, &models.OracleCaptureResource{}); err != nil {
		t.Fatalf("migrate capture control models: %v", err)
	}

	businessInfo := integrationBusinessPostgresConnInfo()
	businessDSN, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(businessInfo)
	if err != nil {
		t.Fatal(err)
	}
	businessDB, err := sql.Open("postgres", businessDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer businessDB.Close()
	if err := businessDB.PingContext(ctx); err != nil {
		t.Fatalf("business PostgreSQL is unavailable: %v", err)
	}
	suffix := time.Now().UnixNano()
	schema := fmt.Sprintf("cdc_control_%d", suffix)
	sourceTable := "orders"
	targetTable := "orders_target"
	if _, err := businessDB.ExecContext(ctx, `CREATE SCHEMA `+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer businessDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+pq.QuoteIdentifier(schema)+` CASCADE`)
	if _, err := businessDB.ExecContext(ctx, `CREATE TABLE `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(sourceTable)+` (id bigint PRIMARY KEY, name text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := businessDB.ExecContext(ctx, `INSERT INTO `+pq.QuoteIdentifier(schema)+`.`+pq.QuoteIdentifier(sourceTable)+` VALUES (1, 'snapshot')`); err != nil {
		t.Fatal(err)
	}

	tenantID := uint(900000 + suffix%90000)
	task := models.TransferTask{
		TenantID: tenantID, Name: "cdc-control-e2e", TaskType: commonExecution.TaskTypeSync,
		Config: integrationCDCConfig(schema, sourceTable, targetTable), Status: models.TaskStatusIdle,
		DesiredState: models.TaskDesiredStateStopped, BatchSize: 100,
	}
	if err := infraDB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = infraDB.Where("task_id = ?", task.ID).Delete(&models.CaptureResource{}).Error
		_ = infraDB.Unscoped().Delete(&models.TransferTask{}, task.ID).Error
	}()

	topicAdmin, err := NewKafkaTopicAdmin(KafkaAdminConfig{
		BootstrapServers: integrationEnv("ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS", "localhost:19092"),
		Username:         integrationEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_USERNAME", "admin"),
		Password:         integrationEnv("ADDP_TEST_INFRA_KAFKA_ADMIN_PASSWORD", "addp_kafka_admin"),
		SecurityProtocol: integrationEnv("ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL", "sasl_plaintext"),
		SASLMechanism:    integrationEnv("ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM", "scram-sha-256"),
		TLSCACertFile:    integrationEnv("ADDP_TEST_INFRA_KAFKA_TLS_CA_CERT_FILE", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer topicAdmin.Close()
	connectClient, err := NewConnectClient(integrationEnv("ADDP_TEST_KAFKA_CONNECT_URL", "http://localhost:18083"), "", "", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	resolver := planner.StaticEngineResolver{
		12: {Type: "postgresql", EngineID: 12, ConnInfo: businessInfo},
		20: {Type: "postgresql", EngineID: 20, ConnInfo: businessInfo},
	}
	supervisor, err := NewSupervisor(
		repository.NewCaptureRepository(infraDB), NewDatabasePlanResolver(resolver), connectClient, topicAdmin,
		DatabaseSourceResources{}, SupervisorConfig{
			TopicRetention: time.Hour, TopicReplication: integrationEnvInt16("ADDP_TEST_INFRA_KAFKA_REPLICATION_FACTOR", 1),
			ConnectLoopbackHost: integrationEnv("ADDP_TEST_KAFKA_CONNECT_LOOPBACK_HOST", "host.docker.internal"),
			ProvisioningTimeout: 60 * time.Second, StatusPollInterval: 500 * time.Millisecond, MonitorInterval: time.Second,
		}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := supervisor.Start(ctx, &task)
	if err != nil {
		t.Fatalf("start capture generation: %v", err)
	}
	defer supervisor.Stop(context.Background(), &task)
	assertCaptureDatabaseResources(t, ctx, businessDB, resource, true)
	if err := supervisor.Pause(ctx, &task); err != nil {
		t.Fatalf("pause target apply observation: %v", err)
	}
	status, err := connectClient.Status(ctx, resource.ConnectorName)
	if err != nil || status.ConnectorState != "RUNNING" {
		t.Fatalf("connector after pause = %#v, err=%v", status, err)
	}
	if err := supervisor.Stop(ctx, &task); err != nil {
		t.Fatalf("stop capture generation: %v", err)
	}
	assertCaptureDatabaseResources(t, ctx, businessDB, resource, false)
	if _, err := connectClient.Status(ctx, resource.ConnectorName); !errors.Is(err, ErrConnectorNotFound) {
		t.Fatalf("connector still exists after stop: %v", err)
	}
	details, err := topicAdmin.admin.ListTopics(ctx, resource.TopicName)
	if err != nil {
		t.Fatal(err)
	}
	if detail, ok := details[resource.TopicName]; ok && detail.Err == nil {
		t.Fatalf("CDC topic %q still exists after stop", resource.TopicName)
	}
	aclResults, err := topicAdmin.admin.DescribeACLs(ctx,
		kadm.NewACLs().Topics(resource.TopicName).Groups(resource.ConsumerGroup).
			ResourcePatternType(kadm.ACLPatternLiteral).Allow().AllowHosts().Operations(kadm.OpAny),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range aclResults {
		if len(result.Described) != 0 {
			t.Fatalf("task-level CDC ACLs still exist after stop: %+v", result.Described)
		}
	}
}

func integrationCDCConfig(schema, sourceTable, targetTable string) map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "continuous", "record_failure": map[string]interface{}{"mode": "block"}},
		"load":    map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"}},
		"source": map[string]interface{}{
			"locator": fmt.Sprintf("addp://engine/12/path/%s/%s?type=table", schema, sourceTable), "data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": fmt.Sprintf("addp://engine/20/path/%s?type=schema", schema), "name": targetTable,
			"data_type": "table", "representation": "native", "policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{
				map[string]interface{}{"source": "id", "target": "id", "target_type": "bigint", "nullable": false},
				map[string]interface{}{"source": "name", "target": "name", "target_type": "string", "nullable": false},
			},
		}},
	}
}

func integrationBusinessPostgresConnInfo() engineplugin.ConnectionInfo {
	return engineplugin.ConnectionInfo{
		"host":     integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_HOST", "localhost"),
		"port":     integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_PORT", "5433"),
		"user":     integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_USER", "business"),
		"password": integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_PASSWORD", "business_password"),
		"database": integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_DATABASE", "business"),
		"sslmode":  integrationEnv("ADDP_TEST_BUSINESS_POSTGRES_SSLMODE", "disable"),
	}
}

func integrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func integrationEnvInt16(key string, fallback int16) int16 {
	value := integrationEnv(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 16)
	if err != nil || parsed <= 0 {
		panic(fmt.Sprintf("invalid %s=%q", key, value))
	}
	return int16(parsed)
}

func assertCaptureDatabaseResources(t *testing.T, ctx context.Context, db *sql.DB, resource *models.CaptureResource, want bool) {
	t.Helper()
	if resource.PostgreSQL == nil {
		t.Fatal("PostgreSQL capture provider facts are missing")
	}
	var slot, publication bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name=$1)`, resource.PostgreSQL.SlotName).Scan(&slot); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname=$1)`, resource.PostgreSQL.PublicationName).Scan(&publication); err != nil {
		t.Fatal(err)
	}
	if slot != want || publication != want {
		t.Fatalf("capture database resources slot=%v publication=%v, want %v", slot, publication, want)
	}
}
