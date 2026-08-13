package capture

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/addp/transfer/internal/models"
	"github.com/lib/pq"
)

var ErrSourceCaptureResourceActive = errors.New("source capture resource is still active")

type SourceResourceControl interface {
	EnsureOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error
	DropOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error
	Probe(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error
	Observe(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource, offsets *ConnectorOffsets, sampledAt time.Time) (*SourceObservation, error)
}

type SourceObservation struct {
	Recovery          *models.CaptureSourceRecovery
	RecoveryError     error
	Transactions      *models.CaptureSourceTransactions
	TransactionsError error
}

type DatabaseSourceResources struct{}

func (DatabaseSourceResources) Probe(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if plan == nil || resource == nil || plan.SourceType != resource.SourceType ||
		plan.SourceEngineID != resource.SourceEngineID || plan.SourceDatabase != resource.SourceDatabase ||
		plan.SourceConnectionFingerprint != resource.SourceConnectionFingerprint {
		return fmt.Errorf("database capture source probe requires matching frozen source identity")
	}
	var db *sql.DB
	var err error
	switch plan.SourceType {
	case models.CaptureSourcePostgreSQL:
		db, err = openPostgreSQL(plan.SourceConnInfo)
	case models.CaptureSourceMySQL:
		db, err = openMySQL(plan.SourceConnInfo)
	case models.CaptureSourceOracle:
		db, err = openOracle(plan.CDCConnInfo)
	default:
		return fmt.Errorf("unsupported database capture source type %q", plan.SourceType)
	}
	if err != nil {
		return err
	}
	defer db.Close()
	var one int
	if err := db.QueryRowContext(ctx, sourceProbeQuery(plan.SourceType)).Scan(&one); err != nil {
		return fmt.Errorf("probe %s CDC source connection: %w", plan.SourceType, err)
	}
	if one != 1 {
		return fmt.Errorf("probe %s CDC source returned unexpected result", plan.SourceType)
	}
	return nil
}

func sourceProbeQuery(sourceType models.CaptureSourceType) string {
	if sourceType == models.CaptureSourceOracle {
		return "SELECT 1 FROM DUAL"
	}
	return "SELECT 1"
}

func (DatabaseSourceResources) Observe(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource, offsets *ConnectorOffsets, sampledAt time.Time) (*SourceObservation, error) {
	if plan == nil || resource == nil || plan.SourceType != resource.SourceType || plan.SourceConnectionFingerprint != resource.SourceConnectionFingerprint {
		return nil, fmt.Errorf("database capture source observation requires matching frozen source identity")
	}
	if plan.SourceType != models.CaptureSourceOracle {
		return nil, nil
	}
	db, err := openOracle(plan.CDCConnInfo)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	observation := &SourceObservation{}
	if captureSCN, captureErr := oracleCaptureSCN(offsets); captureErr != nil {
		observation.RecoveryError = captureErr
	} else {
		observation.Recovery, observation.RecoveryError = observeOracleRecovery(ctx, db, captureSCN, sampledAt)
	}
	observation.Transactions, observation.TransactionsError = observeOracleTransactions(ctx, db, sampledAt)
	return observation, nil
}

func oracleCaptureSCN(offsets *ConnectorOffsets) (*big.Int, error) {
	if offsets == nil || len(offsets.Offsets) != 1 {
		return nil, fmt.Errorf("Oracle capture recovery observation requires exactly one connector offset")
	}
	raw, ok := offsets.Offsets[0].Offset["scn"]
	if !ok {
		return nil, fmt.Errorf("Oracle connector offset has no SCN")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("Oracle connector offset SCN is invalid")
	}
	scn, ok := new(big.Int).SetString(value, 10)
	if !ok || scn.Sign() < 0 {
		return nil, fmt.Errorf("Oracle connector offset SCN is invalid")
	}
	return scn, nil
}

func observeOracleRecovery(ctx context.Context, db *sql.DB, captureSCN *big.Int, sampledAt time.Time) (*models.CaptureSourceRecovery, error) {
	if db == nil || captureSCN == nil {
		return nil, fmt.Errorf("Oracle recovery observation requires database and capture SCN")
	}
	var currentText, earliestText, earliestTimeText string
	var windowSeconds int64
	if err := db.QueryRowContext(ctx, `
		SELECT TO_CHAR(current_scn) FROM V$DATABASE`).Scan(&currentText); err != nil {
		return nil, fmt.Errorf("query Oracle current SCN: %w", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT TO_CHAR(oldest_change),
		       TO_CHAR(FROM_TZ(CAST(oldest_time AS TIMESTAMP), DBTIMEZONE), 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),
		       ROUND((SYSDATE - oldest_time) * 86400)
		FROM (
		  SELECT MIN(first_change#) AS oldest_change, MIN(first_time) AS oldest_time
		  FROM (
			    SELECT first_change#, first_time FROM V$ARCHIVED_LOG
			    WHERE deleted = 'NO' AND status = 'A' AND standby_dest = 'NO' AND name IS NOT NULL
			      AND resetlogs_change# = (SELECT resetlogs_change# FROM V$DATABASE)
		    UNION ALL
		    SELECT first_change#, first_time FROM V$LOG WHERE status <> 'UNUSED'
		  )
		)`).Scan(&earliestText, &earliestTimeText, &windowSeconds); err != nil {
		return nil, fmt.Errorf("query Oracle redo/archive recovery window: %w", err)
	}
	currentSCN, ok := new(big.Int).SetString(strings.TrimSpace(currentText), 10)
	if !ok || currentSCN.Sign() < 0 {
		return nil, fmt.Errorf("Oracle current SCN is invalid")
	}
	if captureSCN.Cmp(currentSCN) > 0 {
		return nil, fmt.Errorf("Oracle connector capture SCN is ahead of current database SCN")
	}
	earliestSCN, ok := new(big.Int).SetString(strings.TrimSpace(earliestText), 10)
	if !ok || earliestSCN.Sign() < 0 {
		return nil, fmt.Errorf("Oracle earliest available SCN is invalid")
	}
	earliestAt, err := time.Parse(time.RFC3339, strings.TrimSpace(earliestTimeText))
	if err != nil || windowSeconds < 0 {
		return nil, fmt.Errorf("Oracle redo/archive recovery window time is invalid")
	}
	health := "healthy"
	headroom := new(big.Int).Sub(new(big.Int).Set(captureSCN), earliestSCN)
	if headroom.Sign() < 0 {
		health = "critical"
	}
	result := &models.CaptureSourceRecovery{
		SchemaVersion: "capture.source_recovery/v1", Provider: string(models.CaptureSourceOracle), Health: health,
		CapturePosition: captureSCN.String(), CurrentPosition: currentSCN.String(), EarliestAvailablePosition: earliestSCN.String(),
		PositionHeadroom: headroom.String(), EarliestAvailableAt: &earliestAt, WindowSeconds: &windowSeconds, SampledAt: sampledAt,
	}
	var used, reclaimable sql.NullFloat64
	if err := db.QueryRowContext(ctx, `
		SELECT SUM(percent_space_used), SUM(percent_space_reclaimable) FROM V$RECOVERY_AREA_USAGE`).Scan(&used, &reclaimable); err != nil {
		return nil, fmt.Errorf("query Oracle recovery area usage: %w", err)
	}
	if used.Valid {
		result.FRAUsedPercent = &used.Float64
	}
	if reclaimable.Valid {
		result.FRAReclaimablePercent = &reclaimable.Float64
	}
	return result, nil
}

func observeOracleTransactions(ctx context.Context, db *sql.DB, sampledAt time.Time) (*models.CaptureSourceTransactions, error) {
	if db == nil {
		return nil, fmt.Errorf("Oracle transaction observation requires database")
	}
	var activeCount uint64
	var oldestStartPosition, oldestDurationText sql.NullString
	var usedUndoBlocks, usedUndoRecords string
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       TO_CHAR(MIN(start_scn)),
		       TO_CHAR(ROUND((SYSDATE - MIN(start_date)) * 86400)),
		       TO_CHAR(COALESCE(SUM(used_ublk), 0)),
		       TO_CHAR(COALESCE(SUM(used_urec), 0))
		FROM V$TRANSACTION`).Scan(&activeCount, &oldestStartPosition, &oldestDurationText, &usedUndoBlocks, &usedUndoRecords); err != nil {
		return nil, fmt.Errorf("query Oracle active transactions: %w", err)
	}
	return buildOracleTransactionObservation(activeCount, oldestStartPosition, oldestDurationText, usedUndoBlocks, usedUndoRecords, sampledAt)
}

func buildOracleTransactionObservation(activeCount uint64, oldestStartPosition, oldestDurationText sql.NullString, usedUndoBlocks, usedUndoRecords string, sampledAt time.Time) (*models.CaptureSourceTransactions, error) {
	result := &models.CaptureSourceTransactions{
		SchemaVersion: "capture.source_transactions/v1", Provider: string(models.CaptureSourceOracle), Status: "available",
		ActiveCount: activeCount, UsedUndoBlocks: strings.TrimSpace(usedUndoBlocks), UsedUndoRecords: strings.TrimSpace(usedUndoRecords), SampledAt: sampledAt,
	}
	if activeCount == 0 {
		return result, nil
	}
	result.OldestStartPosition = strings.TrimSpace(oldestStartPosition.String)
	duration, err := strconv.ParseInt(strings.TrimSpace(oldestDurationText.String), 10, 64)
	if err != nil || result.OldestStartPosition == "" || duration < 0 {
		return nil, fmt.Errorf("Oracle oldest active transaction facts are invalid")
	}
	result.OldestDurationSeconds = &duration
	return result, nil
}

func (DatabaseSourceResources) EnsureOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if plan == nil || resource == nil || plan.SourceType != resource.SourceType {
		return fmt.Errorf("database capture provisioning requires matching plan and resource")
	}
	if plan.SourceConnectionFingerprint == "" || plan.SourceConnectionFingerprint != resource.SourceConnectionFingerprint {
		return fmt.Errorf("refuse to provision database capture resources because the source connection identity changed")
	}
	if plan.SourceType == models.CaptureSourceOracle {
		return ensureOracleSpatialOwnedResources(ctx, plan, resource)
	}
	return nil
}

func (DatabaseSourceResources) DropOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if plan == nil || resource == nil || plan.SourceType != resource.SourceType {
		return fmt.Errorf("database capture cleanup requires matching plan and resource")
	}
	if plan.SourceConnectionFingerprint == "" || plan.SourceConnectionFingerprint != resource.SourceConnectionFingerprint {
		return fmt.Errorf("refuse to clean database capture resources because the source connection identity changed")
	}
	switch plan.SourceType {
	case models.CaptureSourcePostgreSQL:
		return dropPostgreSQLOwnedResources(ctx, plan, resource)
	case models.CaptureSourceMySQL:
		if resource.MySQL == nil {
			return fmt.Errorf("MySQL capture cleanup requires provider resources")
		}
		return nil
	case models.CaptureSourceOracle:
		if resource.Oracle == nil {
			return fmt.Errorf("Oracle capture cleanup requires provider resources")
		}
		return dropOracleSpatialOwnedResources(ctx, plan, resource)
	default:
		return fmt.Errorf("unsupported database capture source type %q", plan.SourceType)
	}
}

func dropPostgreSQLOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if plan == nil || resource == nil {
		return fmt.Errorf("PostgreSQL capture cleanup requires plan and resource")
	}
	if plan.SourceType != models.CaptureSourcePostgreSQL || resource.SourceType != models.CaptureSourcePostgreSQL || resource.PostgreSQL == nil {
		return fmt.Errorf("PostgreSQL capture cleanup requires matching provider resources")
	}
	db, err := openPostgreSQL(plan.SourceConnInfo)
	if err != nil {
		return err
	}
	defer db.Close()

	provider := resource.PostgreSQL
	if provider.SlotOwned {
		var plugin, database string
		var active bool
		err := db.QueryRowContext(ctx, `
			SELECT plugin, database, active FROM pg_replication_slots WHERE slot_name = $1`, provider.SlotName).
			Scan(&plugin, &database, &active)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("inspect ADDP-owned replication slot: %w", err)
		}
		if err == nil {
			if plugin != "pgoutput" || database != resource.SourceDatabase {
				return fmt.Errorf("refuse to drop replication slot %q because its identity does not match capture registration", provider.SlotName)
			}
			if active {
				return fmt.Errorf("%w: replication slot %q", ErrSourceCaptureResourceActive, provider.SlotName)
			}
			if _, err := db.ExecContext(ctx, `SELECT pg_drop_replication_slot($1)`, provider.SlotName); err != nil {
				return fmt.Errorf("drop ADDP-owned replication slot: %w", err)
			}
		}
	}

	if provider.PublicationOwned {
		rows, err := db.QueryContext(ctx, `
			SELECT schemaname, tablename FROM pg_publication_tables WHERE pubname = $1 ORDER BY schemaname, tablename`, provider.PublicationName)
		if err != nil {
			return fmt.Errorf("inspect ADDP-owned publication: %w", err)
		}
		var tables [][2]string
		for rows.Next() {
			var schema, table string
			if err := rows.Scan(&schema, &table); err != nil {
				rows.Close()
				return err
			}
			tables = append(tables, [2]string{schema, table})
		}
		if err := rows.Close(); err != nil {
			return err
		}
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = $1)`, provider.PublicationName).Scan(&exists); err != nil {
			return err
		}
		if exists {
			if len(tables) != 1 || tables[0][0] != resource.SourceSchema || tables[0][1] != resource.SourceTable {
				return fmt.Errorf("refuse to drop publication %q because its table identity does not match capture registration", provider.PublicationName)
			}
			if _, err := db.ExecContext(ctx, `DROP PUBLICATION `+pq.QuoteIdentifier(provider.PublicationName)); err != nil {
				return fmt.Errorf("drop ADDP-owned publication: %w", err)
			}
		}
	}
	return nil
}
