package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iamcli"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This test oracle never initializes identities or writes authorization facts.
// It cannot be used against the shared developer database, nor as an API/ACL.
type catalogGrantObservation struct {
	RequestID          string `json:"request_id"`
	ReceiptCount       int64  `json:"receipt_count"`
	GrantCount         int64  `json:"grant_count"`
	GrantedAt          string `json:"granted_at"`
	IssuanceAuditCount int64  `json:"issuance_audit_count"`
}

func observationEnvironment(environment []string) map[string]string {
	values := map[string]string{}
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	return values
}

func catalogObservationScope(id string, environment []string) (uuid.UUID, int64, int64, error) {
	values := observationEnvironment(environment)
	requestID, err := uuid.Parse(id)
	tenantID, tenantErr := strconv.ParseInt(values["ADDP_ONLINE_TEST_TENANT_ID"], 10, 64)
	engineID, engineErr := strconv.ParseInt(values["ADDP_ONLINE_TEST_ENGINE_ID"], 10, 64)
	host := values["POSTGRES_HOST"]
	port, portErr := strconv.Atoi(values["POSTGRES_PORT"])
	if err != nil || requestID == uuid.Nil || requestID.String() != id || tenantErr != nil || tenantID <= 1 || engineErr != nil || engineID <= 0 ||
		values["ADDP_ONLINE_HOST"] != "1" || values["ADDP_ONLINE_TEST"] != "1" || values["POSTGRES_DB"] != onlineDatabase ||
		(host != "localhost" && host != "127.0.0.1" && host != "::1") || portErr != nil || port < 1 || port > 65535 || values["POSTGRES_USER"] == "" {
		return uuid.Nil, 0, 0, errors.New("issuance observation requires a canonical request UUID and an explicit dedicated loopback Online database/Tenant/Engine")
	}
	return requestID, tenantID, engineID, nil
}

func readCatalogGrantObservation(db *gorm.DB, tenantID, engineID int64, id uuid.UUID) (*catalogGrantObservation, error) {
	result := &catalogGrantObservation{}
	err := db.Raw(`SELECT COUNT(o.request_id) AS receipt_count, COUNT(g.request_id) AS grant_count,
 COALESCE(MAX(CAST(g.granted_at AS TEXT)), '') AS granted_at,
 (SELECT COUNT(*) FROM system.audit_logs a JOIN system.engine_access_fulfillment_outcomes r
  ON a.entity_id = CAST(r.request_id AS TEXT)
  WHERE a.tenant_id = ? AND a.entity_id = ? AND r.tenant_id = a.tenant_id AND r.engine_id = ? AND r.outcome = 'accepted'
  AND a.module_name = 'system' AND a.entity_type = 'engine_access_grant'
  AND a.event_name = 'system.engine_access_grant.issued' AND a.result = 'succeeded') AS issuance_audit_count
 FROM system.engine_access_fulfillment_outcomes o
 LEFT JOIN system.engine_access_grants g ON g.request_id = o.request_id
 WHERE o.tenant_id = ? AND o.engine_id = ? AND o.request_id = ? AND o.outcome = 'accepted'`,
		tenantID, id.String(), engineID, tenantID, engineID, id).Scan(result).Error
	if err != nil {
		return nil, errors.New("System issuance observation query failed")
	}
	result.RequestID = id.String()
	return result, nil
}

func observeCatalogGrant(id string, environment []string, output io.Writer) error {
	requestID, tenantID, engineID, err := catalogObservationScope(id, environment)
	if err != nil {
		return err
	}
	// Do not LoadEnv: the dedicated gate already injected the owner-only config.
	values := observationEnvironment(environment)
	cfg := &config.Config{PostgresHost: values["POSTGRES_HOST"], PostgresPort: values["POSTGRES_PORT"],
		PostgresUser: values["POSTGRES_USER"], PostgresPassword: values["POSTGRES_PASSWORD"], PostgresDB: onlineDatabase}
	db, err := gorm.Open(postgres.Open(cfg.PostgreSQLDSN()+"&connect_timeout=5"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("connect dedicated Online System database failed")
	}
	connection, err := db.DB()
	if err != nil {
		return errors.New("open dedicated Online database handle failed")
	}
	defer connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result *catalogGrantObservation
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var database string
		if err := tx.Raw("SELECT current_database()").Scan(&database).Error; err != nil || database != onlineDatabase {
			return errors.New("issuance observation connected outside the dedicated Online database")
		}
		if err := iamcli.RequireCurrentMigration(tx); err != nil {
			return errors.New("issuance observation requires the current clean System migration")
		}
		var readErr error
		result, readErr = readCatalogGrantObservation(tx, tenantID, engineID, requestID)
		return readErr
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}
