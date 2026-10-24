package auditarchive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type archiveTransport func(*http.Request) (*http.Response, error)

func (f archiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestArchiveBucketCheckIsReadOnlyAndRejectsUnknownCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name, versioning, lock string
		status                 int
		wantError              bool
	}{
		{"enabled_without_global_default", "Enabled", "Enabled", 200, false},
		{"unversioned", "", "Enabled", 200, true},
		{"suspended", "Suspended", "Enabled", 200, true},
		{"lock_disabled", "Enabled", "", 200, true},
		{"configuration_access_denied", "Enabled", "Enabled", 403, true},
		{"store_unavailable", "Enabled", "Enabled", 503, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			client, err := minio.New("archive-fixture.invalid", &minio.Options{Secure: true, Region: "us-east-1", MaxRetries: 1, Creds: credentials.NewStaticV4("fixture", "fixture-secret", ""), Transport: archiveTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/system/" {
					t.Fatal("bucket check tried to write, probe an object or change buckets")
				}
				body := ""
				switch {
				case r.URL.Query().Has("versioning"):
					body = "<VersioningConfiguration><Status>" + tc.versioning + "</Status></VersioningConfiguration>"
				case r.URL.Query().Has("object-lock"):
					body = "<ObjectLockConfiguration><ObjectLockEnabled>" + tc.lock + "</ObjectLockEnabled></ObjectLockConfiguration>"
				default:
					t.Fatal("bucket check exceeded versioning/lock reads")
				}
				if tc.status != 200 {
					body = "<Error><Code>AccessDenied</Code></Error>"
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			err = (&S3Store{Client: client, Bucket: "system"}).CheckBucket(context.Background())
			if (err != nil) != tc.wantError || requests == 0 || requests > 2 {
				t.Fatal("capability result or read budget incorrect", err, requests)
			}
		})
	}
}

func TestArchiveS3DistinguishesAbsentHoldFromUnknownProtection(t *testing.T) {
	for _, tc := range []struct {
		name, code, status  string
		wantHold, wantError bool
	}{
		{"explicit_absence", "NoSuchObjectLockConfiguration", "", false, false},
		{"off", "", "OFF", false, false}, {"on", "", "ON", true, false},
		{"denied", "AccessDenied", "", false, true}, {"missing_version", "NoSuchKey", "", false, true},
		{"unknown_status", "", "unexpected", false, true}, {"empty_status", "", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, e := minio.New("archive-fixture.invalid", &minio.Options{Secure: true, Region: "us-east-1", MaxRetries: 1, Creds: credentials.NewStaticV4("fixture", "fixture-secret", ""), Transport: archiveTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Query().Get("versionId") != "fixture-version" {
					t.Fatal("unexpected operation or unversioned request")
				}
				status := http.StatusOK
				body := "{}"
				typ := "application/json"
				if _, ok := r.URL.Query()["retention"]; ok {
					typ = "application/xml"
					body = fmt.Sprintf(`<Retention><Mode>COMPLIANCE</Mode><RetainUntilDate>%s</RetainUntilDate></Retention>`, time.Now().UTC().Add(366*24*time.Hour).Format(time.RFC3339))
				}
				if _, ok := r.URL.Query()["legal-hold"]; ok {
					typ = "application/xml"
					if tc.code != "" {
						status = http.StatusBadRequest
						body = `<Error><Code>` + tc.code + `</Code></Error>`
					} else {
						body = `<LegalHold><Status>` + tc.status + `</Status></LegalHold>`
					}
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {typ}, "Content-Length": {fmt.Sprint(len(body))}, "Last-Modified": {time.Now().UTC().Format(http.TimeFormat)}}, ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})})
			if e != nil {
				t.Fatal(e)
			}
			s := &S3Store{Client: client, Bucket: "audit-fixture"}
			b, p, e := s.Read(context.Background(), Version{Key: "data.json", ID: "fixture-version"})
			if tc.wantError {
				if e == nil || b != nil {
					t.Fatal("treated an unknown protection result as absent hold")
				}
			} else if e != nil || string(b) != "{}" || p.Hold != tc.wantHold {
				t.Fatal("misclassified protection", p, e)
			}
		})
	}
}

func TestArchiveS3CreateNeverRequestsLegalHold(t *testing.T) {
	until := controlUntil(time.Now().UTC())
	client, e := minio.New("archive-fixture.invalid", &minio.Options{Secure: true, Region: "us-east-1", MaxRetries: 1, Creds: credentials.NewStaticV4("fixture", "fixture-secret", ""), Transport: archiveTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPut || r.Header.Get("X-Amz-Object-Lock-Legal-Hold") != "" || r.Header.Get("X-Amz-Object-Lock-Mode") != "COMPLIANCE" || r.Header.Get("X-Amz-Object-Lock-Retain-Until-Date") != until.Format(time.RFC3339) || r.Header.Get("If-None-Match") != "*" {
			t.Fatal("create lost conditional COMPLIANCE retention or requested Legal Hold")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Amz-Version-Id": {"fixture-version"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	s := &S3Store{Client: client, Bucket: "audit-fixture"}
	if version, err := s.Create(context.Background(), "root.json", []byte(`{}`), until); err != nil || version != "fixture-version" {
		t.Fatal("conditional create failed", err)
	}
}
