package auditarchive

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestIndependentAuditArchiveAgainstPostgres(t *testing.T) {
	if os.Getenv("ADDP_AUDIT_ARCHIVE_INTEGRATION") != "1" {
		t.Skip("requires the owned Hosted PostgreSQL/WORM protocol gate")
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" {
		t.Fatal("requires an exclusive GitHub Hosted fixture")
	}
	dsn := os.Getenv("ADDP_AUDIT_POSTGRES_TEST_DSN")
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close(context.Background())
	if e = migration.NewRunner(dsn).Run(ctx); e != nil {
		t.Fatal(e)
	}
	_, e = admin.Exec(ctx, `CREATE PUBLICATION audit_publication FOR TABLE system.audit_logs WITH (publish='insert');
 CREATE ROLE archive_collector LOGIN REPLICATION PASSWORD 'owned-collector-test';
 GRANT USAGE ON SCHEMA system TO archive_collector;
 GRANT SELECT ON system.audit_logs TO archive_collector;`)
	if e != nil {
		t.Fatal(e)
	}
	pgURL, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	pgURL.User = url.UserPassword("archive_collector", "owned-collector-test")
	p := PostgreSQL{DSN: pgURL.String(), SourceID: testSourceID, Slot: "audit_slot", Publication: "audit_publication", RetentionDays: 365, testPartLimit: 1}
	newStore := func(user, secret string) *S3Store {
		client, e := minio.New(os.Getenv("ADDP_AUDIT_S3_TEST_ENDPOINT"), &minio.Options{Creds: credentials.NewStaticV4(user, secret, ""), Secure: false, Region: "us-east-1", MaxRetries: 1})
		if e != nil {
			t.Fatal(e)
		}
		return &S3Store{Client: client, Bucket: "audit-disposable"}
	}
	duringSnapshot, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer duringSnapshot.Close(context.Background())
	var writeDuringSnapshot, snapshotCommitted atomic.Bool
	writeDuringSnapshot.Store(true)
	var loseResponse, unavailable, droppedSuccessfulPUT atomic.Bool
	backendURL, e := url.Parse("http://" + os.Getenv("ADDP_AUDIT_S3_TEST_ENDPOINT"))
	if e != nil {
		t.Fatal(e)
	}
	proxy := httputil.NewSingleHostReverseProxy(backendURL)
	proxy.ErrorLog = nil
	closeHTTP := func(w http.ResponseWriter) {
		conn, _, e := w.(http.Hijacker).Hijack()
		if e != nil {
			t.Error("fault fixture could not close HTTP connection")
			return
		}
		conn.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/data/") && writeDuringSnapshot.Swap(false) {
			if _, e := duringSnapshot.Exec(ctx, `INSERT INTO system.audit_logs(event_name,result,risk_level,module_name,details) VALUES('iam.archive_during_snapshot','succeeded','high','system','{}')`); e != nil {
				t.Error("could not commit concurrent snapshot fixture")
				closeHTTP(w)
				return
			}
			snapshotCommitted.Store(true)
		}
		if r.Method == http.MethodPut && unavailable.Load() {
			closeHTTP(w)
			return
		}
		if r.Method == http.MethodPut && loseResponse.Swap(false) {
			// The real S3 service has completed the PUT before its successful
			// response is withheld from the actual SDK's HTTP connection.
			recorder := httptest.NewRecorder()
			proxy.ServeHTTP(recorder, r)
			if recorder.Code == http.StatusOK {
				droppedSuccessfulPUT.Store(true)
			}
			closeHTTP(w)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	proxyURL, _ := url.Parse(server.URL)
	client, e := minio.New(proxyURL.Host, &minio.Options{Creds: credentials.NewStaticV4("audit-writer", "owned-writer-test", ""), Secure: false, Region: "us-east-1", MaxRetries: 1})
	if e != nil {
		t.Fatal(e)
	}
	writer := &S3Store{Client: client, Bucket: "audit-disposable"}
	reader := newStore("audit-reader", "owned-reader-test")
	root := newStore("owned-audit-root", "owned-root-test-secret")
	if e = root.Client.MakeBucket(ctx, root.Bucket, minio.MakeBucketOptions{ObjectLocking: true}); e != nil {
		t.Fatal(e)
	}
	insert := `INSERT INTO system.audit_logs(event_name,result,risk_level,module_name,details) VALUES($1,'succeeded','high','system','{}')`
	if _, e = admin.Exec(ctx, insert, "iam.archive_baseline"); e != nil {
		t.Fatal(e)
	}
	// Published migrations can legitimately append catalog governance audit
	// events. Archive and preserve them; never assume a freshly migrated audit
	// table is empty or delete those facts to make a fixture count pass.
	if _, e = admin.Exec(ctx, `INSERT INTO system.audit_logs(event_name,result,risk_level,module_name,details,user_agent) SELECT 'iam.archive_baseline_bulk','succeeded','high','system','{}',repeat('x',2048) FROM generate_series(1,6000)`); e != nil {
		t.Fatal(e)
	}
	var initialCount int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM system.audit_logs`).Scan(&initialCount); e != nil {
		t.Fatal(e)
	}
	rootRef, initErr := p.Initialize(ctx, writer)
	if initErr != nil {
		e = initErr
		t.Fatal("consistent snapshot initialization", e)
	}
	p.Root = rootRef
	if _, e = p.Initialize(ctx, writer); e == nil {
		t.Fatal("silently replaced an existing baseline")
	}
	baseline, e := Load(ctx, reader, rootRef, true, nil)
	if e != nil {
		t.Fatal("baseline not verified", e)
	}
	if baseline.Count != initialCount {
		t.Fatalf("baseline count = %d, source snapshot count = %d", baseline.Count, initialCount)
	}
	t.Logf("consistent baseline preserved all %d source audit records, including migration events", initialCount)
	if !snapshotCommitted.Load() {
		t.Fatal("did not actually commit during the exported snapshot")
	}
	if _, e = Load(ctx, reader, rootRef, true, func(r Row, _ bool) error {
		if *r["event_name"] == "iam.archive_during_snapshot" {
			return ErrConflict
		}
		return nil
	}); e != nil {
		t.Fatal("baseline leaked a post-snapshot commit", e)
	}
	// REPLICATION is not SUPERUSER, and the collector must not mutate source facts.
	collector, e := pgx.Connect(ctx, p.DSN)
	if e != nil {
		t.Fatal(e)
	}
	var isSuper bool
	if e = collector.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname=current_user`).Scan(&isSuper); e != nil || isSuper {
		t.Fatal("collector must not be superuser", e)
	}
	if _, e = collector.Exec(ctx, `UPDATE system.audit_logs SET event_name='iam.forged'`); e == nil {
		t.Fatal("collector modified source audit")
	}
	collector.Close(ctx)

	loseResponse.Store(true)
	var stop context.CancelFunc
	var finished chan error
	start := func(store Store) {
		streamCtx, streamStop := context.WithCancel(ctx)
		stop = streamStop
		finished = make(chan error, 1)
		go func() { finished <- p.Collect(streamCtx, store, nil) }()
		t.Cleanup(streamStop)
	}
	wait := func(count int) *Chain {
		t.Helper()
		deadline := time.NewTimer(20 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			c, e := Load(ctx, reader, rootRef, true, nil)
			if e == nil && c.Count == count {
				return c
			}
			if e == nil && c.Count > count {
				t.Fatalf("archive passed the expected transaction checkpoint: got %d records, expected %d", c.Count, count)
			}
			select {
			case e := <-finished:
				t.Fatalf("collector exited before checkpoint: %v", e)
			case <-deadline.C:
				t.Fatalf("archive checkpoint not reached, expected %d", count)
			case <-tick.C:
			}
		}
	}
	shutdown := func() {
		stop()
		select {
		case e := <-finished:
			if !errors.Is(e, context.Canceled) {
				t.Fatal("collector shutdown", e)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("collector did not stop")
		}
	}
	start(writer)
	wait(initialCount + 1)
	initialCount++
	t.Log("a real independent commit during segmented snapshot upload was absent from the baseline and recovered exactly once by the same logical slot")
	// Conn.Begin does not create an independent transaction on an already
	// active connection. Keep both transactions on different real backends so
	// committing b cannot also commit a's earlier row.
	other, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close(context.Background())
	if other.PgConn().PID() == admin.PgConn().PID() {
		t.Fatal("commit-order fixture requires two independent PostgreSQL backends")
	}
	a, e := admin.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Rollback(context.Background())
	b, e := other.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Rollback(context.Background())
	var earlierID, laterID int64
	if e = a.QueryRow(ctx, insert+" RETURNING id", "iam.archive_earlier_id_later_commit").Scan(&earlierID); e != nil {
		t.Fatal(e)
	}
	if e = b.QueryRow(ctx, insert+" RETURNING id", "iam.archive_later_id_earlier_commit").Scan(&laterID); e != nil {
		t.Fatal(e)
	}
	if earlierID >= laterID {
		t.Fatal("commit-order fixture did not allocate the earlier ID first")
	}
	if e = b.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	first := wait(initialCount + 1)
	readLatest := func(c *Chain) Part {
		t.Helper()
		if len(c.Active.Parts) == 0 {
			t.Fatal("missing active part")
		}
		b, _, e := readRef(ctx, reader, c.Active.Parts[len(c.Active.Parts)-1].Ref, false)
		if e != nil {
			t.Fatal(e)
		}
		var part Part
		if e = decode(b, &part); e != nil {
			t.Fatal(e)
		}
		return part
	}
	last := readLatest(first)
	if len(last.Rows) != 1 || *last.Rows[0]["event_name"] != "iam.archive_later_id_earlier_commit" {
		t.Fatal("first committed batch did not contain only the later ID while the earlier transaction remained open")
	}
	if e = p.Collect(ctx, writer, nil); e == nil {
		t.Fatal("a second collector acquired the active replication slot")
	}
	if e = a.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	second := wait(initialCount + 2)
	last = readLatest(second)
	if len(last.Rows) != 1 || *last.Rows[0]["event_name"] != "iam.archive_earlier_id_later_commit" {
		t.Fatal("later commit lost the earlier ID or failed to preserve transaction order")
	}
	t.Log("two independent PostgreSQL backends committed the higher ID first while the lower ID stayed uncommitted; both archive checkpoints preserved actual commit order")
	rolled, e := admin.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = rolled.Exec(ctx, insert, "iam.archive_rolled_back"); e != nil {
		t.Fatal(e)
	}
	if e = rolled.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(ctx, insert, "iam.archive_after_rollback"); e != nil {
		t.Fatal(e)
	}
	c := wait(initialCount + 3)
	if _, e = Load(ctx, reader, rootRef, true, func(r Row, _ bool) error {
		if *r["event_name"] == "iam.archive_rolled_back" {
			return ErrConflict
		}
		return nil
	}); e != nil {
		t.Fatal("archived a rollback", e)
	}
	_ = c
	shutdown()
	if !droppedSuccessfulPUT.Load() {
		t.Fatal("did not observe and withhold a real successful S3 PUT response")
	}
	t.Log("consistent exported snapshot and actual PostgreSQL commit order retained late lower IDs; rollback absent; lost object PUT confirmation resolved by immutable version readback")

	// A failed archive upload must not acknowledge the corresponding commit.
	unavailable.Store(true)
	start(writer)
	if _, e = admin.Exec(ctx, insert, "iam.archive_outage"); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-finished:
		if e == nil {
			t.Fatal("unavailable archive was accepted")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("outage did not stop collector")
	}
	stop()
	before, e := Load(ctx, reader, rootRef, true, nil)
	if e != nil || before.Count != initialCount+3 {
		t.Fatal("failed upload advanced archive", e)
	}
	var confirmed string
	if e = admin.QueryRow(ctx, `SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name='audit_slot'`).Scan(&confirmed); e != nil {
		t.Fatal(e)
	}
	confirmedLSN, e := parseLSN(confirmed)
	if e != nil || confirmedLSN > before.EndLSN {
		t.Fatal("acknowledged unarchived commit")
	}
	unavailable.Store(false)
	start(writer)
	wait(initialCount + 4)
	shutdown()
	t.Log("archive outage acknowledged no unarchived WAL; restarting the same slot recovered exactly once without resetting the source history")
	if _, e = admin.Exec(ctx, `ALTER TABLE system.audit_logs DISABLE TRIGGER trg_audit_logs_prevent_update_delete;
 UPDATE system.audit_logs SET event_name='iam.archive_source_changed' WHERE event_name='iam.archive_baseline';
 ALTER TABLE system.audit_logs ENABLE TRIGGER trg_audit_logs_prevent_update_delete;`); e != nil {
		t.Fatal(e)
	}
	if _, rows, e := QueryArchive(ctx, reader, reader, rootRef, testActor, 365, Query{EventName: "iam.archive_baseline"}, false); e != nil || len(rows) != 1 {
		t.Fatal("source DBA changed independently archived facts", e)
	}
	t.Log("business database owner bypassed the local trigger and changed the source row; independent versioned evidence retained the original event")

	if _, rows, e := QueryArchive(ctx, reader, reader, rootRef, testActor, 365, Query{EventName: "iam.archive_outage"}, false); e != nil || len(rows) != 1 {
		t.Fatal("independent query/access audit", e)
	}
	if _, rows, e := QueryArchive(ctx, reader, reader, rootRef, "948d2bb7-8071-4a85-83b8-873f5c8e1fd7", 365, Query{}, false); e == nil || rows != nil {
		t.Fatal("reader impersonated another query-log identity")
	}
	key := partKey(testSourceID, 0, 0)
	if _, e = reader.Create(ctx, key, []byte(`{}`), time.Now().Add(366*24*time.Hour), false); e == nil {
		t.Fatal("audit reader wrote original facts")
	}
	if _, e = writer.Create(ctx, "queries/"+testActor+"/forged.json", []byte(`{}`), time.Now().Add(366*24*time.Hour), false); e == nil {
		t.Fatal("collector wrote query history")
	}
	v, e := exact(ctx, root, key)
	if e != nil {
		t.Fatal(e)
	}
	if e = root.Client.RemoveObject(ctx, root.Bucket, key, minio.RemoveObjectOptions{VersionID: v.ID, GovernanceBypass: true}); e == nil {
		t.Fatal("root deleted a COMPLIANCE version")
	}
	mode := minio.Compliance
	short := time.Now().Add(time.Hour)
	if e = root.Client.PutObjectRetention(ctx, root.Bucket, key, minio.PutObjectRetentionOptions{VersionID: v.ID, Mode: &mode, RetainUntilDate: &short}); e == nil {
		t.Fatal("root shortened COMPLIANCE retention")
	}
	t.Log("actual object store denied reader writes, collector query-log forgery, identity-prefix forgery, version deletion and retention shortening; this proves API retention, not protection from host root")

	if _, e = admin.Exec(ctx, `SELECT pg_drop_replication_slot('audit_slot')`); e != nil {
		t.Fatal(e)
	}
	if e = p.Collect(ctx, writer, nil); e == nil {
		t.Fatal("missing slot silently recreated")
	}
	plan, e := PlanExpiry(ctx, reader, rootRef)
	if e != nil || len(plan.Objects) != 0 || plan.Authorization {
		t.Fatal("read-only plan accepted current protected data", e)
	}
	off := minio.LegalHoldDisabled
	if e = writer.Client.PutObjectLegalHold(ctx, writer.Bucket, rootRef.Key, minio.PutObjectLegalHoldOptions{VersionID: rootRef.Version, Status: &off}); e == nil {
		t.Fatal("collector released the root hold")
	}
	t.Log("segmented baseline exceeded 8 MiB in one exported snapshot; pinned root and sealed checkpoints retained legal holds; read-only expiry plan selected no protected objects and collector could not release the root hold")
	// A privileged storage administrator can create a new version; verification
	// must detect the shadow rather than trusting an unversioned latest object.
	data, until, e := root.Read(ctx, v)
	if e != nil {
		t.Fatal(e)
	}
	opts := minio.PutObjectOptions{Mode: minio.Compliance, RetainUntilDate: until.Until, DisableMultipart: true}
	if _, e = root.Client.PutObject(ctx, root.Bucket, key, bytes.NewReader(data), int64(len(data)), opts); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(ctx, reader, rootRef, true, nil); e == nil {
		t.Fatal("shadow version accepted as original evidence")
	}
	t.Log("missing replication slot remained unknown without reinitialization; a real shadow object version invalidated the chain")
}
