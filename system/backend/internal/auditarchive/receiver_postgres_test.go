package auditarchive

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestIndependentAuditReceiverAgainstPostgres(t *testing.T) {
	if os.Getenv("ADDP_AUDIT_ARCHIVE_INTEGRATION") != "1" {
		t.Skip("requires the owned Hosted audit protocol gate")
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" {
		t.Fatal("exclusive Hosted fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	makeStore := func(user, key string) *S3Store {
		c, e := minio.New(os.Getenv("ADDP_AUDIT_S3_TEST_ENDPOINT"), &minio.Options{Secure: false, Region: "us-east-1", MaxRetries: 1, Creds: credentials.NewStaticV4(user, key, "")})
		if e != nil {
			t.Fatal(e)
		}
		return &S3Store{Client: c, Bucket: "audit-access-disposable"}
	}
	admin := makeStore("owned-audit-root", "owned-root-test-secret")
	if e := admin.Client.MakeBucket(ctx, admin.Bucket, minio.MakeBucketOptions{ObjectLocking: true}); e != nil {
		t.Fatal(e)
	}
	writer := makeStore("access-writer", "owned-access-writer-test")
	reader := makeStore("access-reader", "owned-access-reader-test")
	root := receiverRoot(t, writer)
	h := fixtureReceiver(t, writer, root)
	var drop atomic.Bool
	drop.Store(true)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := httptest.NewRecorder()
		h.ServeHTTP(record, r)
		if record.Code == 204 && drop.Swap(false) {
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			_ = conn.Close()
			return
		}
		w.WriteHeader(record.Code)
	}))
	defer server.Close()
	post := func(body string) (int, error) {
		req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+ReceiverPath, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+receiverTestToken)
		response, e := server.Client().Do(req)
		if e != nil {
			return 0, e
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode, nil
	}
	body := accessFixture("lost-confirmation") + "\n" + accessFixture("whole-batch")
	if _, e := post(body); e == nil {
		t.Fatal("fixture did not lose the successful HTTP confirmation")
	}
	before, e := VerifyReceiver(ctx, reader, testSourceID, receiverTestDeployment, root, Ref{})
	if e != nil || before.Sequence != 1 || before.Events != 2 {
		t.Fatal("protected receipt not present before lost response", e)
	}
	// Restart handler with only the pinned root and remote protected evidence.
	server.Close()
	h = fixtureReceiver(t, writer, root)
	server = httptest.NewTLSServer(h)
	defer server.Close()
	if code, e := post(body); e != nil || code != 204 {
		t.Fatal("lost-confirmation retry", code, e)
	}
	after, e := VerifyReceiver(ctx, reader, testSourceID, receiverTestDeployment, root, before.Checkpoint)
	if e != nil || after != before {
		t.Fatal("restart changed the confirmed receipt", e)
	}
	if e = writer.Walk(ctx, accessPrefix(testSourceID), func(v Version) error {
		b, p, e := reader.Read(ctx, v)
		if e != nil {
			return e
		}
		if bytes.Contains(b, []byte("secret-credential-marker")) {
			t.Fatal("secret projection persisted")
		}
		if strings.Contains(v.Key, "/data/") {
			if !validAccessBody(v, p) {
				t.Fatal("body lacks 365-day protection")
			}
		} else if !validControl(v, p, time.Now()) {
			t.Fatal("control lacks 20-year protection")
		}
		extendedUntil := retentionUntil(p.Until.AddDate(1, 0, 0))
		if e = writer.Extend(ctx, v, extendedUntil); e != nil {
			t.Fatal("extension denied", e)
		}
		extendedBody, extendedProtection, readErr := reader.Read(ctx, v)
		if readErr != nil || !bytes.Equal(extendedBody, b) || extendedProtection.Until.Before(extendedUntil) {
			t.Fatal("extension was not confirmed on the original exact version", readErr)
		}
		// This pinned MinIO release rejects shortening via ObjectLocked before
		// evaluating the retention policy's date condition. Require its exact
		// WORM response, not an arbitrary error or only an AccessDenied code.
		shortenErr := writer.Extend(ctx, v, time.Now().Add(time.Hour))
		rejection := minio.ToErrorResponse(shortenErr)
		afterBody, afterProtection, afterReadErr := reader.Read(ctx, v)
		mode, until, retentionReadErr := reader.Client.GetObjectRetention(ctx, reader.Bucket, v.Key, v.ID)
		afterVersion, versionErr := exact(ctx, reader, v.Key)
		afterUntil := "unreadable"
		if until != nil {
			afterUntil = until.UTC().Format(time.RFC3339Nano)
		}
		t.Logf("COMPLIANCE shortening probe key=%q version=%q error_type=%T error_code=%q http_status=%d error_message=%q before=%s after=%s",
			v.Key, v.ID, shortenErr, rejection.Code, rejection.StatusCode, rejection.Message,
			extendedProtection.Until.UTC().Format(time.RFC3339Nano), afterUntil)
		if afterReadErr != nil || retentionReadErr != nil || versionErr != nil || afterVersion.ID != v.ID ||
			mode == nil || *mode != minio.Compliance || until == nil || !until.Equal(extendedProtection.Until) ||
			afterProtection != extendedProtection || !bytes.Equal(afterBody, b) {
			t.Fatal("COMPLIANCE evidence changed or could not be read after the shortening attempt")
		}
		if !fixedMinioWORMShorteningDenied(shortenErr) {
			if shortenErr == nil {
				t.Fatal("retention shortening operation unexpectedly succeeded")
			}
			t.Fatal("retention shortening did not return the pinned MinIO WORM rejection; see safe S3 error and version evidence above")
		}
		hold := minio.LegalHoldEnabled
		if e = writer.Client.PutObjectLegalHold(ctx, writer.Bucket, v.Key, minio.PutObjectLegalHoldOptions{VersionID: v.ID, Status: &hold}); e == nil || minio.ToErrorResponse(e).Code != "AccessDenied" {
			t.Fatal("receiver Legal Hold write was not denied")
		}
		if e = writer.Client.RemoveObject(ctx, writer.Bucket, v.Key, minio.RemoveObjectOptions{VersionID: v.ID}); e == nil || minio.ToErrorResponse(e).Code != "AccessDenied" {
			t.Fatal("writer delete not denied")
		}
		if _, e = reader.Create(ctx, v.Key+".forbidden", []byte(`{}`), controlUntil(time.Now())); e == nil || minio.ToErrorResponse(e).Code != "AccessDenied" {
			t.Fatal("reader wrote evidence")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = writer.Create(ctx, "access/foreign/data/x.json", []byte(`{}`), controlUntil(time.Now())); e == nil || minio.ToErrorResponse(e).Code != "AccessDenied" {
		t.Fatal("foreign source write allowed")
	}
	// A new object has no pre-existing WORM retention. Its too-short deadline
	// must be refused by IAM, separately from the existing-version WORM check.
	for _, key := range []string{accessPrefix(testSourceID) + "data/too-short.json", accessPrefix(testSourceID) + "receipts/too-short.json"} {
		_, denyErr := writer.Create(ctx, key, []byte(`{}`), time.Now().Add(time.Hour))
		denial := minio.ToErrorResponse(denyErr)
		if denyErr == nil || denial.Code != "AccessDenied" || denial.StatusCode != http.StatusForbidden {
			t.Fatalf("new short-retention object was not denied by IAM: code=%q http_status=%d", denial.Code, denial.StatusCode)
		}
		found := false
		if e = reader.Walk(ctx, key, func(v Version) error {
			if v.Key == key {
				found = true
			}
			return nil
		}); e != nil || found {
			t.Fatal("short-retention rejection left an object or its absence could not be confirmed", e)
		}
	}
	t.Log("all B exact versions retained their extended COMPLIANCE deadlines and bytes after the pinned MinIO 400 InvalidRequest WORM rejection; new short-retention body and control objects received IAM 403 AccessDenied and remained absent")
	// Fail the real S3 transport, then replay against the recovered endpoint.
	backend, _ := url.Parse("http://" + os.Getenv("ADDP_AUDIT_S3_TEST_ENDPOINT"))
	reverse := httputil.NewSingleHostReverseProxy(backend)
	var unavailable atomic.Bool
	unavailable.Store(true)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			w.WriteHeader(503)
			return
		}
		reverse.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	faultClient, err := minio.New(proxyURL.Host, &minio.Options{Region: "us-east-1", MaxRetries: 1, Secure: false, Creds: credentials.NewStaticV4("access-writer", "owned-access-writer-test", "")})
	if err != nil {
		t.Fatal(err)
	}
	h2 := fixtureReceiver(t, &S3Store{Client: faultClient, Bucket: writer.Bucket}, root)
	if c := receiveRequest(h2, accessFixture("outage"), nil); c != 503 {
		t.Fatal("unreadable B acknowledged", c)
	}
	unavailable.Store(false)
	if c := receiveRequest(h2, accessFixture("outage"), nil); c != 204 {
		t.Fatal("retry after recovery", c)
	}
	t.Log("B protected complete batches before HTTP confirmation; lost confirmation and receiver restart reused the same exact receipt without duplicate events; body 365 days and root/receipts 20 calendar years were read back, extension succeeded and deletion/shortening/foreign-source writes were denied")

	testNativeAuditQueue(t, ctx, writer, reader)

	// Privileged shadow creation must invalidate pinned evidence.
	v, e := exact(ctx, admin, accessReceiptKey(testSourceID, 1))
	if e != nil {
		t.Fatal(e)
	}
	raw, p, e := admin.Read(ctx, v)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Client.PutObject(ctx, admin.Bucket, v.Key, bytes.NewReader(raw), int64(len(raw)), minio.PutObjectOptions{Mode: minio.Compliance, RetainUntilDate: p.Until, DisableMultipart: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = VerifyReceiver(ctx, reader, testSourceID, receiverTestDeployment, root, before.Checkpoint); e == nil {
		t.Fatal("real shadow receipt accepted")
	}
	t.Log("real shadow receipt invalidated B verification; Loki was absent throughout and was not an acknowledgement dependency")
}

func testNativeAuditQueue(t *testing.T, ctx context.Context, writer, reader Store) {
	t.Helper()
	source := "aa1744e2-4fbe-45fb-aa91-2a65e26195a1"
	deployment := os.Getenv("ADDP_AUDIT_A_TEST_DEPLOYMENT")
	root, e := InitReceiver(ctx, writer, source, deployment)
	if e != nil {
		t.Fatal(e)
	}
	receiver, e := NewReceiver(writer, source, deployment, receiverTestToken, root, Ref{}, 365)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := tls.LoadX509KeyPair(os.Getenv("ADDP_AUDIT_RECEIVER_TEST_CERT"), os.Getenv("ADDP_AUDIT_RECEIVER_TEST_KEY"))
	if e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", os.Getenv("ADDP_AUDIT_RECEIVER_TEST_ADDRESS"))
	if e != nil {
		t.Fatal(e)
	}
	var blocked atomic.Bool
	blocked.Store(true)
	var rejected atomic.Int64
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if blocked.Load() {
			rejected.Add(1)
			w.WriteHeader(503)
			return
		}
		receiver.ServeHTTP(w, r)
	}), ReadHeaderTimeout: time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}}
	done := make(chan error, 1)
	go func() { done <- server.ServeTLS(listener, "", "") }()
	defer func() { _ = server.Close(); <-done }()
	a, e := minio.New(os.Getenv("ADDP_AUDIT_A_TEST_ENDPOINT"), &minio.Options{Region: "us-east-1", MaxRetries: 1, Secure: false, Creds: credentials.NewStaticV4("owned-source-root", "owned-source-test-secret", "")})
	if e != nil {
		t.Fatal(e)
	}
	if e = a.MakeBucket(ctx, "source-owned", minio.MakeBucketOptions{}); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"queued-first", "queued-second", "queued-third"} {
		if _, e = a.PutObject(ctx, "source-owned", name, strings.NewReader("owned fixture"), 13, minio.PutObjectOptions{DisableMultipart: true}); e != nil {
			t.Fatal(e)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for rejected.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if rejected.Load() == 0 {
		t.Fatal("native A queue did not actually encounter an unavailable B")
	}
	empty, e := VerifyReceiver(ctx, reader, source, deployment, root, Ref{})
	if e != nil || empty.Events != 0 {
		t.Fatal("unavailable B acknowledged source events")
	}
	blocked.Store(false)
	deadline = time.Now().Add(30 * time.Second)
	wanted := map[string]bool{"queued-first": false, "queued-second": false, "queued-third": false}
	for time.Now().Before(deadline) {
		// Access to the B source is serialized with delivery; inspect complete receipts.
		receiver.mu.Lock()
		out, e := VerifyReceiver(ctx, reader, source, deployment, root, Ref{})
		if e == nil {
			e = reader.Walk(ctx, accessPrefix(source)+"data/", func(v Version) error {
				raw, _, e := reader.Read(ctx, v)
				if e != nil {
					return e
				}
				var b accessBatch
				if decode(raw, &b) != nil {
					return ErrUnknown
				}
				for _, event := range b.Events {
					if event.API.Name == "PutObject" {
						if _, ok := wanted[event.API.Object]; ok {
							wanted[event.API.Object] = true
						}
					}
				}
				return nil
			})
		}
		receiver.mu.Unlock()
		if e == nil && wanted["queued-first"] && wanted["queued-second"] && wanted["queued-third"] && out.Events >= 3 {
			t.Log("unmodified A native disk webhook queue received actual 503 responses and replayed all three known source PutObject events after B recovered; only this bounded outage request set was reconciled, not the accepted crash/full-queue loss boundary")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("native audit queue did not reconcile the known source request set")
}

// This is a version-specific test oracle, not a runtime fallback. Malformed
// requests, transport failures and other InvalidRequest messages must fail.
func fixedMinioWORMShorteningDenied(err error) bool {
	if err == nil {
		return false
	}
	response := minio.ToErrorResponse(err)
	return response.StatusCode == http.StatusBadRequest && response.Code == "InvalidRequest" && response.Message == "Object is WORM protected and cannot be overwritten"
}
