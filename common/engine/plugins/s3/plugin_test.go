package s3

import (
	"context"
	"github.com/addp/common/engine/plugin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestS3ConnectionDefaults(t *testing.T) {
	p := &S3Plugin{}
	for _, field := range p.ConnectionSpec().Fields {
		if field.Key == "use_ssl" {
			if field.Default != true {
				t.Fatal("Amazon S3 must default to TLS")
			}
			return
		}
	}
	t.Fatal("missing TLS connection field")
}

func TestS3SharedProtocolHonorsExplicitHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/" {
			t.Errorf("unexpected S3 request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<ListAllMyBucketsResult><Buckets></Buckets></ListAllMyBucketsResult>`))
	}))
	defer server.Close()
	p := &S3Plugin{}
	conn := plugin.ConnectionInfo{"endpoint": strings.TrimPrefix(server.URL, "http://"), "access_key": "ak", "secret_key": "sk", "use_ssl": false}
	if err := p.TestConnection(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
}
