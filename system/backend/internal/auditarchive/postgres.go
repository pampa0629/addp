package auditarchive

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

type PostgreSQL struct {
	DSN, SourceID, Slot, Publication string
	RetentionDays                    int
}

func (p PostgreSQL) connect(ctx context.Context) (*pgconn.PgConn, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if !identifier(p.Slot) || !identifier(p.Publication) || p.RetentionDays < 1 || p.RetentionDays > 36500 {
		return nil, ErrUnknown
	}
	if _, e := sourcePrefix(p.SourceID); e != nil {
		return nil, e
	}
	c, e := pgconn.ParseConfig(p.DSN)
	if e != nil {
		return nil, ErrUnknown
	}
	c.RuntimeParams["replication"] = "database"
	c.RuntimeParams["timezone"] = "UTC"
	c.RuntimeParams["DateStyle"] = "ISO,YMD"
	conn, e := pgconn.ConnectConfig(ctx, c)
	if e != nil {
		return nil, ErrUnknown
	}
	return conn, nil
}

func identify(ctx context.Context, c *pgconn.PgConn, p PostgreSQL) (Source, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results, e := c.Exec(ctx, "IDENTIFY_SYSTEM").ReadAll()
	if e != nil || len(results) != 1 || len(results[0].Rows) != 1 || len(results[0].Rows[0]) != 4 {
		return Source{}, ErrUnknown
	}
	r := results[0].Rows[0]
	timeline, e := strconv.ParseUint(string(r[1]), 10, 32)
	if e != nil {
		return Source{}, ErrUnknown
	}
	s := Source{ID: p.SourceID, SystemID: string(r[0]), Timeline: uint32(timeline), Database: string(r[3]), Slot: p.Slot, Publication: p.Publication}
	if !validSource(s) {
		return Source{}, ErrUnknown
	}
	return s, nil
}

func (p PostgreSQL) sql(ctx context.Context) (*pgx.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, e := pgx.ParseConfig(p.DSN)
	if e != nil {
		return nil, ErrUnknown
	}
	c.RuntimeParams["timezone"] = "UTC"
	c.RuntimeParams["DateStyle"] = "ISO,YMD"
	c.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, e := pgx.ConnectConfig(ctx, c)
	if e != nil {
		return nil, ErrUnknown
	}
	var ok bool
	e = conn.QueryRow(ctx, `SELECT NOT pg_is_in_recovery() AND current_setting('transaction_read_only')='off'
 AND EXISTS(SELECT 1 FROM pg_roles WHERE rolname=current_user AND rolreplication AND NOT rolsuper AND NOT rolcreaterole AND NOT rolcreatedb AND NOT rolbypassrls)
 AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=(SELECT oid FROM pg_roles WHERE rolname=current_user))
 AND NOT has_schema_privilege(current_user,'system','CREATE')
 AND NOT has_table_privilege(current_user,'system.audit_logs','INSERT,UPDATE,DELETE,TRUNCATE,TRIGGER')
 AND EXISTS(SELECT 1 FROM pg_publication WHERE pubname=$1 AND pubinsert AND NOT pubupdate AND NOT pubdelete AND NOT pubtruncate AND NOT puballtables)
 AND (SELECT count(*) FROM pg_publication_tables WHERE pubname=$1)=1
 AND EXISTS(SELECT 1 FROM pg_publication_tables WHERE pubname=$1 AND schemaname='system' AND tablename='audit_logs' AND rowfilter IS NULL
 AND cardinality(attnames)=(SELECT count(*) FROM pg_attribute WHERE attrelid='system.audit_logs'::regclass AND attnum>0 AND NOT attisdropped))`, p.Publication).Scan(&ok)
	if e != nil || !ok {
		conn.Close(ctx)
		return nil, ErrUnknown
	}
	return conn, nil
}

// Initialize is explicit and one-shot. It leaves a failed slot for operator
// investigation rather than silently resetting an unknown source history.
func (p PostgreSQL) Initialize(ctx context.Context, store Store) error {
	prefix, e := sourcePrefix(p.SourceID)
	if e != nil {
		return e
	}
	versions, e := store.Versions(ctx, prefix)
	if e != nil || len(versions) != 0 {
		return ErrConflict
	}
	sql, e := p.sql(ctx)
	if e != nil {
		return e
	}
	defer sql.Close(context.Background())
	c, e := p.connect(ctx)
	if e != nil {
		return e
	}
	defer c.Close(context.Background())
	source, e := identify(ctx, c, p)
	if e != nil {
		return e
	}
	results, e := c.Exec(ctx, `CREATE_REPLICATION_SLOT `+p.Slot+` LOGICAL pgoutput EXPORT_SNAPSHOT`).ReadAll()
	if e != nil || len(results) != 1 || len(results[0].Rows) != 1 || len(results[0].Rows[0]) < 3 {
		return ErrUnknown
	}
	r := results[0].Rows[0]
	lsn, e := parseLSN(string(r[1]))
	if e != nil {
		return e
	}
	snapshot := string(r[2])
	if len(snapshot) == 0 || len(snapshot) > 128 || strings.ContainsAny(snapshot, "'\\\x00") {
		return ErrUnknown
	}
	tx, e := sql.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return ErrUnknown
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET TRANSACTION SNAPSHOT '`+snapshot+`'`); e != nil {
		return ErrUnknown
	}
	rows, e := tx.Query(ctx, `SELECT * FROM system.audit_logs ORDER BY id`)
	if e != nil {
		return ErrUnknown
	}
	var events []Row
	size := 0
	for rows.Next() {
		r := Row{}
		raw := rows.RawValues()
		fields := rows.FieldDescriptions()
		size += len(fields) * 32
		for i, v := range raw {
			if v == nil {
				r[fields[i].Name] = nil
			} else {
				s := string(v)
				r[fields[i].Name] = &s
				size += len(v)
			}
		}
		if size > MaxObjectBytes/2 {
			rows.Close()
			return ErrBudget
		}
		events = append(events, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return ErrUnknown
	}
	if e = tx.Commit(ctx); e != nil {
		return ErrUnknown
	}
	now := time.Now().UTC().Truncate(time.Second)
	_, e = Save(ctx, store, Batch{Format: format, Source: source, EndLSN: lsn, ArchivedAt: now, CommitAt: now, RetentionDays: p.RetentionDays, Rows: events})
	return e
}

// Collect runs until cancellation or an integrity/transport error. It never
// retries a write, recreates a slot, or modifies business data automatically.
func (p PostgreSQL) Collect(ctx context.Context, store Store, onCheckpoint func(*Chain)) error {
	loadCtx, loadCancel := context.WithTimeout(ctx, 2*time.Minute)
	chain, e := Load(loadCtx, store, p.SourceID)
	loadCancel()
	if e != nil {
		return e
	}
	if len(chain.Batches) >= MaxChainObjects {
		return ErrBudget
	}
	if p.RetentionDays < chain.Batches[len(chain.Batches)-1].RetentionDays {
		return ErrUnknown
	}
	sql, e := p.sql(ctx)
	if e != nil {
		return e
	}
	var confirmed, restart string
	var active bool
	e = sql.QueryRow(ctx, `SELECT confirmed_flush_lsn::text,restart_lsn::text,active FROM pg_replication_slots WHERE slot_name=$1 AND plugin='pgoutput' AND database=current_database() AND slot_type='logical' AND wal_status IN ('reserved','extended')`, p.Slot).Scan(&confirmed, &restart, &active)
	sql.Close(context.Background())
	fl, fe := parseLSN(confirmed)
	rl, re := parseLSN(restart)
	if e != nil || fe != nil || re != nil || active || fl > chain.EndLSN || rl > chain.EndLSN {
		return ErrUnknown
	}
	c, e := p.connect(ctx)
	if e != nil {
		return e
	}
	defer c.Close(context.Background())
	source, e := identify(ctx, c, p)
	if e != nil || source != chain.Source {
		return ErrUnknown
	}
	query := fmt.Sprintf("START_REPLICATION SLOT %s LOGICAL %s (proto_version '1', publication_names '%s')", p.Slot, formatLSN(chain.EndLSN), p.Publication)
	c.Frontend().Send(&pgproto3.Query{String: query})
	if flush(c) != nil {
		return ErrUnknown
	}
	startCtx, startCancel := context.WithTimeout(ctx, 15*time.Second)
	m, e := c.ReceiveMessage(startCtx)
	startCancel()
	if e != nil {
		return ErrUnknown
	}
	if _, ok := m.(*pgproto3.CopyBothResponse); !ok {
		return ErrUnknown
	}
	d := decoder{}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e = feedback(c, chain.EndLSN); e != nil {
			return ErrUnknown
		}
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		m, e = c.ReceiveMessage(readCtx)
		cancel()
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if pgconn.Timeout(e) {
				continue
			}
			return ErrUnknown
		}
		switch message := m.(type) {
		case *pgproto3.CopyData:
			if len(message.Data) < 1 {
				return ErrUnknown
			}
			switch message.Data[0] {
			case 'k':
				if len(message.Data) != 18 {
					return ErrUnknown
				}
			case 'w':
				if len(message.Data) < 26 {
					return ErrUnknown
				}
				committed, e := d.parse(message.Data[25:])
				if e != nil {
					return e
				}
				if committed == nil || committed.EndLSN <= chain.EndLSN {
					continue
				}
				if len(chain.Batches) >= MaxChainObjects {
					return ErrBudget
				}
				b := Batch{Format: format, Source: source, Sequence: uint64(len(chain.Batches)), Previous: chain.LastSHA, EndLSN: committed.EndLSN, ArchivedAt: time.Now().UTC().Truncate(time.Second), CommitAt: committed.At, RetentionDays: p.RetentionDays, Rows: committed.Rows}
				encoded, e := encodeBatch(b)
				if e != nil {
					return e
				}
				if chain.Bytes+len(encoded) > MaxChainBytes {
					return ErrBudget
				}
				saveCtx, saveCancel := context.WithTimeout(ctx, 30*time.Second)
				sha, e := Save(saveCtx, store, b)
				saveCancel()
				if e != nil {
					return e
				}
				chain.Bytes += len(encoded)
				chain.Batches = append(chain.Batches, b)
				chain.LastSHA = sha
				chain.EndLSN = b.EndLSN
				chain.Count += len(b.Rows)
				chain.ArchivedAt = b.ArchivedAt
				chain.LastCommitAt = b.CommitAt
				if onCheckpoint != nil {
					onCheckpoint(chain)
				}
			default:
				return ErrUnknown
			}
		default:
			return ErrUnknown
		}
	}
}

func feedback(c *pgconn.PgConn, lsn uint64) error {
	b := make([]byte, 34)
	b[0] = 'r'
	for _, off := range []int{1, 9, 17} {
		binary.BigEndian.PutUint64(b[off:], lsn)
	}
	micros := time.Since(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).Microseconds()
	binary.BigEndian.PutUint64(b[25:], uint64(micros))
	c.Frontend().Send(&pgproto3.CopyData{Data: b})
	return flush(c)
}

func flush(c *pgconn.PgConn) error {
	if e := c.Conn().SetWriteDeadline(time.Now().Add(10 * time.Second)); e != nil {
		return e
	}
	defer c.Conn().SetWriteDeadline(time.Time{})
	return c.Frontend().Flush()
}

func parseLSN(s string) (uint64, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, ErrUnknown
	}
	a, e := strconv.ParseUint(parts[0], 16, 32)
	if e != nil {
		return 0, ErrUnknown
	}
	b, e := strconv.ParseUint(parts[1], 16, 32)
	if e != nil {
		return 0, ErrUnknown
	}
	return a<<32 | b, nil
}
func formatLSN(v uint64) string { return fmt.Sprintf("%X/%X", v>>32, v&0xffffffff) }
