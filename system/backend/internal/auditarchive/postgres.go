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
	Root                             Ref
	testPartLimit                    int
}

func (p PostgreSQL) connect(ctx context.Context) (*pgconn.PgConn, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if !identifier(p.Slot) || !identifier(p.Publication) || p.RetentionDays < minRetentionDays || p.RetentionDays > 36500 {
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
func (p PostgreSQL) initialize(ctx context.Context, store Store) (Ref, error) {
	prefix, e := sourcePrefix(p.SourceID)
	if e != nil {
		return Ref{}, e
	}
	exists := false
	e = store.Walk(ctx, prefix, func(Version) error { exists = true; return ErrConflict })
	if e != nil || exists {
		return Ref{}, ErrConflict
	}
	sql, e := p.sql(ctx)
	if e != nil {
		return Ref{}, e
	}
	defer sql.Close(context.Background())
	c, e := p.connect(ctx)
	if e != nil {
		return Ref{}, e
	}
	defer c.Close(context.Background())
	source, e := identify(ctx, c, p)
	if e != nil {
		return Ref{}, e
	}
	results, e := c.Exec(ctx, `CREATE_REPLICATION_SLOT `+p.Slot+` LOGICAL pgoutput EXPORT_SNAPSHOT`).ReadAll()
	if e != nil || len(results) != 1 || len(results[0].Rows) != 1 || len(results[0].Rows[0]) < 3 {
		return Ref{}, ErrUnknown
	}
	r := results[0].Rows[0]
	lsn, e := parseLSN(string(r[1]))
	if e != nil {
		return Ref{}, e
	}
	snapshot := string(r[2])
	if len(snapshot) == 0 || len(snapshot) > 128 || strings.ContainsAny(snapshot, "'\\\x00") {
		return Ref{}, ErrUnknown
	}
	tx, e := sql.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Ref{}, ErrUnknown
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET TRANSACTION SNAPSHOT '`+snapshot+`'`); e != nil {
		return Ref{}, ErrUnknown
	}
	rows, e := tx.Query(ctx, `SELECT * FROM system.audit_logs ORDER BY id`)
	if e != nil {
		return Ref{}, ErrUnknown
	}
	w := newWriter(store, source, p.RetentionDays, &segment{Baseline: true, BeforeLSN: lsn, EndLSN: lsn})
	if p.testPartLimit > 0 {
		w.partLimit = p.testPartLimit
	}
	for rows.Next() {
		r := Row{}
		raw := rows.RawValues()
		fields := rows.FieldDescriptions()
		for i, v := range raw {
			if v == nil {
				r[fields[i].Name] = nil
			} else {
				s := string(v)
				r[fields[i].Name] = &s
			}
		}
		if e = w.add(ctx, []Row{r}, lsn, time.Time{}); e != nil {
			rows.Close()
			return Ref{}, e
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return Ref{}, ErrUnknown
	}
	if e = tx.Commit(ctx); e != nil {
		return Ref{}, ErrUnknown
	}
	if e = w.flush(ctx); e != nil {
		return Ref{}, e
	}
	baseline, e := w.seal(ctx)
	if e != nil {
		return Ref{}, e
	}
	return put(ctx, store, rootKey(p.SourceID), Root{format, source, baseline, lsn}, time.Now().UTC().Add(time.Duration(p.RetentionDays)*24*time.Hour+retentionMargin), true)
}

func (p PostgreSQL) Initialize(ctx context.Context, store Store) (Ref, error) {
	if p.Root.valid() {
		return Ref{}, ErrConflict
	}
	return p.initialize(ctx, store)
}

// Collect runs until cancellation or an integrity/transport error. It never
// retries a write, recreates a slot, or modifies business data automatically.
func (p PostgreSQL) Collect(ctx context.Context, store Store, onCheckpoint func(*Chain)) error {
	loadCtx, loadCancel := context.WithTimeout(ctx, 2*time.Minute)
	chain, e := Load(loadCtx, store, p.Root, false, nil)
	loadCancel()
	if e != nil {
		return e
	}
	if chain.Source.ID != p.SourceID {
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
	w := newWriter(store, source, p.RetentionDays, chain.Active)
	if p.testPartLimit > 0 {
		w.partLimit = p.testPartLimit
	}
	baseCount := chain.Count
	update := func() {
		chain.Count = baseCount + w.confirmedCount
		chain.EndLSN = w.current.EndLSN
		chain.Last = w.current.Previous
		if len(w.current.Parts) > 0 {
			chain.Tail = w.current.Parts[len(w.current.Parts)-1].Ref
		} else {
			chain.Tail = chain.Last
		}
		chain.Segments = w.current.Sequence
		chain.ArchivedAt = w.now()
		chain.LastCommitAt = w.lastCommitAt
		if onCheckpoint != nil {
			onCheckpoint(chain)
		}
	}
	emit := func() error {
		before := w.confirmedCount
		if e := w.flush(ctx); e != nil {
			return e
		}
		if w.confirmedCount > before {
			update()
		}
		return nil
	}

	d := decoder{}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e = feedback(c, chain.EndLSN); e != nil {
			return ErrUnknown
		}
		timeout := 10 * time.Second
		if len(w.rows) > 0 {
			timeout = time.Until(w.lastFlush.Add(5 * time.Second))
			if timeout <= 0 {
				if e = emit(); e != nil {
					return e
				}
				continue
			}
		}
		readCtx, cancel := context.WithTimeout(ctx, timeout)
		m, e = c.ReceiveMessage(readCtx)
		cancel()
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if pgconn.Timeout(e) {
				if e = emit(); e != nil {
					return e
				}
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
				previous := w.confirmedCount
				if e = w.add(ctx, committed.Rows, committed.EndLSN, committed.At); e != nil {
					return e
				}
				if w.confirmedCount > previous {
					update()
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
