package minio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestMinIOInstanceOverviewUsesAuthenticatedBucketListOnly(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint(denied), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/" || r.URL.RawQuery != "" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
					t.Errorf("unexpected native operation %s %s", r.Method, r.URL.String())
				}
				w.Header().Set("Content-Type", "application/xml")
				if denied {
					w.WriteHeader(403)
					fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>private-error-sentinel</Message></Error>`)
					return
				}
				fmt.Fprint(w, `<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Buckets><Bucket><Name>one</Name></Bucket><Bucket><Name>two</Name></Bucket></Buckets></ListAllMyBucketsResult>`)
			}))
			defer server.Close()
			result, err := (&MinIOPlugin{}).ReadInstanceOverview(t.Context(), plugin.ConnectionInfo{"endpoint": strings.TrimPrefix(server.URL, "http://"), "access_key": "fixture-access", "secret_key": "fixture-secret", "bucket": "one"})
			if err != nil || result == nil || len(result.Metrics) != 1 {
				t.Fatalf("overview=%v error=%v", result, err)
			}
			m := result.Metrics[0]
			if m.Scope != "account" || m.Key != "account_buckets" || calls != 1 {
				t.Fatal(m, calls)
			}
			if denied {
				if m.Status != "permission_denied" || m.Value != nil {
					t.Fatal(m)
				}
			} else if m.Status != "available" || m.Value == nil || *m.Value != 2 {
				t.Fatal(m)
			}
			b, _ := json.Marshal(result)
			if strings.Contains(string(b), "private-error-sentinel") || strings.Contains(string(b), "fixture-secret") {
				t.Fatal("private native response leaked")
			}
		})
	}
}

func TestMinIOInstanceOverviewCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&MinIOPlugin{}).ReadInstanceOverview(ctx, plugin.ConnectionInfo{"endpoint": "127.0.0.1:1", "access_key": "fixture-access", "secret_key": "fixture-secret"})
	if err == nil {
		t.Fatal("canceled read succeeded")
	}
}

func TestMinIOContentReadSessionBindsActualObjectAndKeepsEvidenceAfterClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("location") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<LocationConstraint>us-east-1</LocationConstraint>`)
			return
		}
		if r.Method != "GET" || r.URL.Path != "/bucket/input.geojson" {
			t.Errorf("unexpected content read %s %s", r.Method, r.URL)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Length", "2")
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, "{}")
	}))
	defer server.Close()
	target, err := plugin.ObjectItemPathFromRefPath(11, "bucket/input.geojson")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := (&MinIOPlugin{}).OpenContent(t.Context(), plugin.ConnectionInfo{"endpoint": server.URL, "access_key": "fixture-access", "secret_key": "fixture-secret"}, target, plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "{}" {
		t.Fatal("actual object", string(data), err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	evidence, ok := reader.(plugin.ReadSessionEvidence)
	if !ok {
		t.Fatal("read session evidence missing")
	}
	set, err := evidence.ReadSet(t.Context())
	if err != nil || set == nil || len(set.Paths) != 1 || !reflect.DeepEqual(set.Paths[0], target) {
		t.Fatal("wrong object read closure", err)
	}
	set.Paths[0].EngineID = 999
	again, err := evidence.ReadSet(t.Context())
	if err != nil || !reflect.DeepEqual(again.Paths[0], target) {
		t.Fatal("mutable evidence")
	}
}
