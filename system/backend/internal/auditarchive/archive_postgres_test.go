package auditarchive

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
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
	"github.com/minio/minio-go/v7/pkg/signer"
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
		return &S3Store{Client: client, Bucket: "platform-disposable"}
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
	writer := &S3Store{Client: client, Bucket: "platform-disposable"}
	reader := newStore("audit-reader", "owned-reader-test")
	root := newStore("owned-audit-root", "owned-root-test-secret")
	if e = root.Client.MakeBucket(ctx, root.Bucket, minio.MakeBucketOptions{ObjectLocking: true}); e != nil {
		t.Fatal(e)
	}
	t.Run("shared_platform_bucket_capabilities_do_not_change_other_objects", func(t *testing.T) {
		if err := writer.CheckBucket(ctx); err != nil {
			t.Fatal("dedicated writer cannot verify the shared bucket", err)
		}
		if err := reader.CheckBucket(ctx); err != nil {
			t.Fatal("dedicated auditor cannot verify the shared bucket", err)
		}
		lock, mode, days, unit, err := root.Client.GetObjectLockConfig(ctx, root.Bucket)
		if err != nil || lock != "Enabled" || mode != nil || days != nil || unit != nil {
			t.Fatal("archive installed global retention on the platform bucket", err)
		}
		// A versioned bucket lacking Object Lock must be rejected, not upgraded.
		const ordinaryBucket = "platform-without-object-lock"
		if err := root.Client.MakeBucket(ctx, ordinaryBucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := root.Client.EnableVersioning(ctx, ordinaryBucket); err != nil {
			t.Fatal(err)
		}
		ordinary := &S3Store{Client: root.Client, Bucket: ordinaryBucket}
		if err := ordinary.CheckBucket(ctx); err == nil {
			t.Fatal("accepted a versioned bucket without Object Lock")
		}
		if enabled, _, _, _, err := root.Client.GetObjectLockConfig(ctx, ordinaryBucket); err == nil && enabled == "Enabled" {
			t.Fatal("read-only capability check enabled Object Lock")
		}
		// Non-audit content shares the actual bucket, not a second fixture bucket.
		key := "settings/ordinary-platform-file.json"
		info, err := root.Client.PutObject(ctx, root.Bucket, key, strings.NewReader("ordinary"), 8, minio.PutObjectOptions{DisableMultipart: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := root.Client.RemoveObject(ctx, root.Bucket, key, minio.RemoveObjectOptions{VersionID: info.VersionID}); err != nil {
			t.Fatal("audit retention leaked into ordinary platform files", err)
		}
		t.Log("shared platform bucket has versioning and Object Lock without a global default; ordinary platform objects remain writable and deletable; capability check did not upgrade an incompatible bucket")
	})
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
	if _, err := Load(ctx, reader, rootRef, true, nil); err != nil {
		t.Fatal("auditor cannot verify source evidence before query", err)
	}
	if err := reader.Walk(ctx, "audit/queries/"+testActor+"/", func(Version) error { return nil }); err != nil {
		t.Fatal("auditor cannot confirm its own query-log versions", err)
	}
	if err := reader.Walk(ctx, "audit/queries/948d2bb7-8071-4a85-83b8-873f5c8e1fd7/", func(Version) error { return nil }); err == nil {
		t.Fatal("auditor listed another query-log identity")
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
	if _, e = reader.Create(ctx, key, []byte(`{}`), time.Now().Add(366*24*time.Hour)); e == nil {
		t.Fatal("audit reader wrote original facts")
	}
	if _, e = writer.Create(ctx, "audit/queries/"+testActor+"/forged.json", []byte(`{}`), time.Now().Add(366*24*time.Hour)); e == nil {
		t.Fatal("collector wrote query history")
	}
	v, e := exact(ctx, root, key)
	if e != nil {
		t.Fatal(e)
	}
	t.Run("tenant_and_archive_identities_are_isolated_inside_one_bucket", func(t *testing.T) {
		tenant := newStore("tenant-user", "owned-tenant-test")
		tenantKey := "tenant_17/ordinary.txt"
		ordinary, err := tenant.Client.PutObject(ctx, tenant.Bucket, tenantKey, strings.NewReader("tenant"), 6, minio.PutObjectOptions{DisableMultipart: true})
		if err != nil {
			t.Fatal("tenant cannot write its own ordinary prefix", err)
		}
		obj, err := tenant.Client.GetObject(ctx, tenant.Bucket, tenantKey, minio.GetObjectOptions{VersionID: ordinary.VersionID})
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(obj)
		obj.Close()
		if err != nil || string(body) != "tenant" {
			t.Fatal("tenant ordinary read was changed", err)
		}
		denied := func(err error) {
			t.Helper()
			if minio.ToErrorResponse(err).Code != "AccessDenied" {
				t.Fatal("isolation did not return an authorization denial", err)
			}
		}
		_, err = tenant.Client.StatObject(ctx, tenant.Bucket, key, minio.StatObjectOptions{VersionID: v.ID})
		denied(err)
		_, err = tenant.Create(ctx, "audit/sources/"+testSourceID+"/data/tenant-forgery.json", []byte(`{}`), time.Now().Add(366*24*time.Hour))
		denied(err)
		denied(tenant.Client.RemoveObject(ctx, tenant.Bucket, key, minio.RemoveObjectOptions{VersionID: v.ID}))
		for _, prefix := range []string{"", archiveNamespace} {
			observedDenial := false
			for object := range tenant.Client.ListObjects(ctx, tenant.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: true}) {
				denied(object.Err)
				observedDenial = true
			}
			if !observedDenial {
				t.Fatal("tenant listed the bucket root or archive prefix")
			}
		}
		for _, archiveIdentity := range []*S3Store{writer, reader} {
			_, err = archiveIdentity.Client.StatObject(ctx, archiveIdentity.Bucket, tenantKey, minio.StatObjectOptions{VersionID: ordinary.VersionID})
			denied(err)
			_, err = archiveIdentity.Create(ctx, "settings/forged.json", []byte(`{}`), time.Now().Add(366*24*time.Hour))
			denied(err)
		}
		if err := tenant.Client.RemoveObject(ctx, tenant.Bucket, tenantKey, minio.RemoveObjectOptions{VersionID: ordinary.VersionID}); err != nil {
			t.Fatal("audit retention changed tenant ordinary deletion", err)
		}
		t.Log("real tenant identity was denied archive read/write/delete and root/audit listing; archive identities were denied tenant reads and other platform writes; ordinary tenant CRUD remained available in the same bucket")
	})
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
	// Real storage metadata, not a synthetic clock or a successful PUT alone.
	controls := 0
	for _, prefix := range []string{rootRef.Key, "audit/sources/" + testSourceID + "/checkpoints/"} {
		if e = writer.Walk(ctx, prefix, func(cv Version) error {
			_, pr, err := writer.Read(ctx, cv)
			if err != nil || !validControl(cv, pr, time.Now().UTC()) || pr.Hold {
				return ErrUnknown
			}
			controls++
			if err = root.Client.RemoveObject(ctx, root.Bucket, cv.Key, minio.RemoveObjectOptions{VersionID: cv.ID}); err == nil {
				t.Fatal("root deleted a protected control version")
			}
			shorter := pr.Until.Add(-24 * time.Hour)
			if err = root.Extend(ctx, cv, shorter); err == nil {
				t.Fatal("root shortened control protection")
			}
			longer := retentionUntil(pr.Until.Add(24 * time.Hour))
			if err = writer.Extend(ctx, cv, longer); err != nil {
				return err
			}
			_, after, err := writer.Read(ctx, cv)
			if err != nil || after.Until.Before(longer) {
				return ErrUnknown
			}
			return nil
		}); e != nil {
			t.Fatal("control protection readback/extension", e)
		}
	}
	if controls < 2 {
		t.Fatal("missing root or sealed checkpoints")
	}
	_, bodyProtection, e := writer.Read(ctx, v)
	if e != nil || bodyProtection.Until.Before(v.CreatedAt.Add(365*24*time.Hour)) || bodyProtection.Until.After(v.CreatedAt.AddDate(2, 0, 0)) {
		t.Fatal("control retention leaked into body retention", e)
	}
	queryKey := "audit/queries/" + testActor + "/hold-denial-fixture.json"
	queryVersion, e := reader.Create(ctx, queryKey, []byte(`{}`), time.Now().Add(366*24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	for _, identity := range []struct {
		store                      *S3Store
		user, secret, key, version string
	}{
		{writer, "audit-writer", "owned-writer-test", rootRef.Key, rootRef.Version},
		{reader, "audit-reader", "owned-reader-test", queryKey, queryVersion},
	} {
		for _, status := range []minio.LegalHoldStatus{minio.LegalHoldEnabled, minio.LegalHoldDisabled} {
			err := identity.store.Client.PutObjectLegalHold(ctx, identity.store.Bucket, identity.key, minio.PutObjectLegalHoldOptions{VersionID: identity.version, Status: &status})
			if minio.ToErrorResponse(err).Code != "AccessDenied" {
				t.Fatal("runtime modified Legal Hold", err)
			}
		}
		// Sign a real contradictory request: header ON, XML body OFF. Denial
		// must be authorization, never just a broken signature or malformed XML.
		body := `<LegalHold xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>OFF</Status></LegalHold>`
		u := *backendURL
		u.Path = "/" + identity.store.Bucket + "/" + identity.key
		q := url.Values{"legal-hold": {""}, "versionId": {identity.version}}
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		md := md5.Sum([]byte(body))
		sh := sha256.Sum256([]byte(body))
		req.Header.Set("Content-Md5", base64.StdEncoding.EncodeToString(md[:]))
		req.Header.Set("X-Amz-Content-Sha256", hex.EncodeToString(sh[:]))
		req.Header.Set("X-Amz-Object-Lock-Legal-Hold", "ON")
		req = signer.SignV4(*req, identity.user, identity.secret, "", "us-east-1")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var rejected struct{ Code string }
		err = xml.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&rejected)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusForbidden || rejected.Code != "AccessDenied" {
			t.Fatal("contradictory Legal Hold request did not receive an authorization denial")
		}
	}
	if _, err := Load(ctx, reader, rootRef, true, nil); err != nil {
		t.Fatal("extension changed evidence identity", err)
	}
	t.Log("root and all sealed checkpoints have at least 20 calendar years COMPLIANCE protection without Legal Hold; extensions succeeded and root deletion/shortening were denied; body protection remains at least 365 days")
	t.Log("collector and auditor were denied Legal Hold ON, OFF and signed header-ON/body-OFF requests; read-only expiry plan selected no protected objects")
	t.Log("segmented baseline exceeded 8 MiB in one exported snapshot with a pinned root and immutable sealed checkpoints")
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
	// A trusted writer key can create a short-retained new object; that is not
	// accepted evidence or an IAM promise of relative calendar-year enforcement.
	// Do this only after the healthy-chain assertions and intentional shadow.
	shortKey := "audit/sources/" + testSourceID + "/data/unaccepted-short.json"
	if ref, err := put(ctx, writer, shortKey, map[string]bool{"fixture": true}, time.Now().Add(time.Hour), false); err == nil || ref != (Ref{}) {
		t.Fatal("archive acknowledged a short-retained new body under the production policy")
	}
	shortVersion, err := exact(ctx, root, shortKey)
	if err != nil {
		t.Fatal("fixture did not really store the short-retained object", err)
	}
	_, shortProtection, err := root.Read(ctx, shortVersion)
	if err != nil || !shortProtection.Until.Before(shortVersion.CreatedAt.Add(365*24*time.Hour)) {
		t.Fatal("fixture did not exercise actual short storage protection", err)
	}
	t.Log("production policy uses scoped COMPLIANCE actions without dated retention floors; real short-retained storage was rejected with no archive acknowledgement; existing version protection remains enforced by storage")
}
