package capture

import (
	"context"
	"database/sql"
	"errors"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
	"github.com/lib/pq"
	"net"
	"strconv"
	"time"
)

type postgresSlotSample struct {
	SlotType, Plugin, Database                sql.NullString
	Active                                    bool
	WALStatus, Retained, Unacknowledged, Safe sql.NullString
	Unlimited                                 bool
	PublicationAllTables                      sql.NullBool
	PublicationTables, PublicationMatches     int
}

// lib/pq's context-aware dial bounds TCP establishment, but not PostgreSQL/TLS
// startup. This connection belongs to one read-only probe, including its query.
type postgresObservationDialer struct{ deadline time.Time }

func (d postgresObservationDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d postgresObservationDialer) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return d.DialContext(ctx, network, address)
}

func (d postgresObservationDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{Deadline: d.deadline}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(d.deadline); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func postgresSlotObservation(resource *models.CaptureResource, sample postgresSlotSample, now time.Time, validity time.Duration) *models.CapturePostgreSQLObservation {
	result := &models.CapturePostgreSQLObservation{Status: "identity_mismatch", RiskState: "unknown", SampledAt: now, ExpiresAt: now.Add(validity)}
	if sample.SlotType.String != "logical" || sample.Plugin.String != "pgoutput" || !sample.Database.Valid || sample.Database.String != resource.SourceDatabase || !sample.PublicationAllTables.Valid || sample.PublicationAllTables.Bool || sample.PublicationTables != 1 || sample.PublicationMatches != 1 {
		return result
	}
	for _, value := range []sql.NullString{sample.Retained, sample.Unacknowledged, sample.Safe} {
		if value.Valid {
			if _, err := strconv.ParseUint(value.String, 10, 64); err != nil {
				result.Status = "unavailable"
				return result
			}
		}
	}
	result.Status = "available"
	result.SlotActive = &sample.Active
	result.WALKeepUnlimited = &sample.Unlimited
	if sample.WALStatus.Valid {
		switch sample.WALStatus.String {
		case "reserved", "extended", "unreserved", "lost":
			result.WALStatus = &sample.WALStatus.String
			result.RiskState = sample.WALStatus.String
		}
	}
	if sample.Retained.Valid {
		result.RetainedWALBytes = &sample.Retained.String
	}
	if sample.Unacknowledged.Valid {
		result.UnacknowledgedWALBytes = &sample.Unacknowledged.String
	}
	if sample.Safe.Valid {
		result.SafeWALSizeBytes = &sample.Safe.String
	}
	return result
}

func readPostgreSQLObservation(ctx context.Context, db *sql.DB, resource *models.CaptureResource, now time.Time, validity time.Duration) *models.CapturePostgreSQLObservation {
	result := &models.CapturePostgreSQLObservation{Status: "unavailable", RiskState: "unknown", SampledAt: now, ExpiresAt: now.Add(validity)}
	provider := resource.PostgreSQL
	if provider == nil || !provider.SlotOwned || !provider.PublicationOwned {
		result.Status = "identity_mismatch"
		return result
	}
	var sample postgresSlotSample
	err := db.QueryRowContext(ctx, `WITH current_wal AS MATERIALIZED (
 SELECT CASE WHEN pg_is_in_recovery() THEN NULL ELSE pg_current_wal_lsn() END AS lsn
 ) SELECT s.slot_type,s.plugin,s.database,s.active,s.wal_status,
 pg_wal_lsn_diff(w.lsn,s.restart_lsn)::text,
 pg_wal_lsn_diff(w.lsn,s.confirmed_flush_lsn)::text,s.safe_wal_size::text,
 current_setting('max_slot_wal_keep_size')='-1',p.puballtables,
 (SELECT count(*) FROM pg_publication_tables WHERE pubname=$2),
 (SELECT count(*) FROM pg_publication_tables WHERE pubname=$2 AND schemaname=$3 AND tablename=$4)
 FROM pg_replication_slots s CROSS JOIN current_wal w
 LEFT JOIN pg_publication p ON p.pubname=$2 WHERE s.slot_name=$1`, provider.SlotName, provider.PublicationName, resource.SourceSchema, resource.SourceTable).Scan(&sample.SlotType, &sample.Plugin, &sample.Database, &sample.Active, &sample.WALStatus, &sample.Retained, &sample.Unacknowledged, &sample.Safe, &sample.Unlimited, &sample.PublicationAllTables, &sample.PublicationTables, &sample.PublicationMatches)
	if errors.Is(err, sql.ErrNoRows) {
		result.Status = "missing"
		return result
	}
	if err != nil {
		return result
	}
	return postgresSlotObservation(resource, sample, now, validity)
}

// PostgreSQL and Connect observations fail independently and share only their publication CAS.
func (s *Supervisor) observePostgreSQLTask(ctx context.Context, resource *models.CaptureResource) error {
	if resource.SourceType != models.CaptureSourcePostgreSQL {
		return nil
	}
	now := time.Now().UTC()
	validity := 2 * s.config.MonitorInterval
	pg := &models.CapturePostgreSQLObservation{Status: "unavailable", RiskState: "unknown", SampledAt: now, ExpiresAt: now.Add(validity)}
	probeCtx, cancel := context.WithTimeout(ctx, s.config.SourceProbeTimeout)
	plan, err := s.plans.ResolveForObservation(probeCtx, resource)
	if err == nil {
		dsn, dsnErr := engineplugin.BuildPostgreSQLDSN(plan.SourceConnInfo, 5432)
		if dsnErr == nil {
			connector, connectorErr := pq.NewConnector(dsn)
			if connectorErr == nil {
				deadline, _ := probeCtx.Deadline() // Established by WithTimeout above.
				connector.Dialer(postgresObservationDialer{deadline: deadline})
				db := sql.OpenDB(connector)
				pg = readPostgreSQLObservation(probeCtx, db, resource, now, validity)
				db.Close()
			}
		}
	}
	cancel()
	delay := &models.CaptureDelayObservation{Status: "unavailable", SampledAt: now, ExpiresAt: now.Add(validity)}
	delayCtx, cancelDelay := context.WithTimeout(ctx, s.config.SourceProbeTimeout)
	value, err := s.connect.Delay(delayCtx, resource.ConnectorName)
	cancelDelay()
	if err == nil && value != nil {
		delay.Status = value.Status
		delay.Connected = value.Connected
		delay.DelaySeconds = value.Seconds
	}
	return s.advance(ctx, resource, map[string]interface{}{"postgresql_observation": sourceObservationMap(pg), "delay_observation": sourceObservationMap(delay)})
}
