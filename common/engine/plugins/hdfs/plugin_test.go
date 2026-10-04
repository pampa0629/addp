package hdfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func fixture(t *testing.T) (plugin.ConnectionInfo, *httptest.Server) {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("user.name") != "reader" {
			t.Errorf("missing Simple identity")
			w.WriteHeader(403)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/webhdfs/v1/addp") {
			t.Errorf("outside root: %s", r.URL.Path)
		}
		if strings.Contains(r.URL.Path, "missing") {
			w.WriteHeader(404)
			fmt.Fprint(w, "private physical path")
			return
		}
		switch r.URL.Query().Get("op") {
		case "GETFILESTATUS":
			s := fileStatus{Type: "DIRECTORY"}
			if strings.HasSuffix(r.URL.Path, ".csv") {
				s.Type = "FILE"
				s.Length = 10
			}
			if strings.HasSuffix(r.URL.Path, "link") {
				s.Type = "SYMLINK"
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"FileStatus": s})
		case "LISTSTATUS_BATCH":
			if r.URL.Query().Get("startafter") == "" {
				fmt.Fprint(w, `{"DirectoryListing":{"partialListing":{"FileStatuses":{"FileStatus":[{"pathSuffix":"a.csv","type":"FILE","length":10}]}},"remainingEntries":1}}`)
			} else {
				fmt.Fprint(w, `{"DirectoryListing":{"partialListing":{"FileStatuses":{"FileStatus":[{"pathSuffix":" space%名.csv","type":"FILE","length":10},{"pathSuffix":"link","type":"SYMLINK"}]}},"remainingEntries":0}}`)
			}
		case "OPEN":
			if r.URL.Query().Get("noredirect") == "true" {
				q := r.URL.Query()
				q.Del("noredirect")
				json.NewEncoder(w).Encode(map[string]string{"Location": server.URL + r.URL.EscapedPath() + "?" + q.Encode()})
			} else {
				offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				fmt.Fprint(w, "0123456789"[offset:])
			}
		default:
			t.Errorf("unexpected operation")
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(server.Close)
	return plugin.ConnectionInfo{"webhdfs_endpoint": server.URL, "rpc_uri": "hdfs://namenode:8020", "root_path": "/addp", "user": "reader", "authentication": "simple"}, server
}

func TestHDFSCatalogAndRange(t *testing.T) {
	info, _ := fixture(t)
	p := &HDFSPlugin{}
	ctx := context.Background()
	if err := p.TestConnection(ctx, info); err != nil {
		t.Fatal(err)
	}
	children, err := p.ListChildren(ctx, info, plugin.FileRootPath(7), plugin.ListOptions{})
	if err != nil || len(children) != 2 {
		t.Fatalf("children=%#v err=%v", children, err)
	}
	if children[1].Storage.Path != " space%名.csv" || children[1].Path.StringPath() != " space%名.csv" {
		t.Fatalf("name was normalized: %#v", children[1])
	}
	f, err := p.DescribeEngineCatalogFacts(ctx, info, children[0].Path, plugin.EngineCatalogFactsOptions{})
	if err != nil || *f.Storage.SizeBytes != 10 {
		t.Fatalf("facts=%#v err=%v", f, err)
	}
	for _, tc := range []struct {
		offset, length int64
		want           string
	}{{0, 3, "012"}, {9, 10, "9"}, {10, 4, ""}, {100, 4, ""}} {
		r, err := p.OpenRange(ctx, info, children[0].Path, plugin.ReadOptions{Offset: tc.offset, Length: tc.length})
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil || string(data) != tc.want {
			t.Fatalf("range %#v: %q %v", tc, data, err)
		}
	}
	if _, err := p.ResolvePath(ctx, info, plugin.FileDirectoryPath(7, "missing")); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) || strings.Contains(err.Error(), "private") {
		t.Fatalf("missing/error disclosure: %v", err)
	}
	if _, err := p.ResolvePath(ctx, info, plugin.FileItemPath(7, "link/a.csv")); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorUnsupported) {
		t.Fatalf("ancestor link accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := p.TestConnection(ctx, info); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestHDFSConnectionAndPathBoundaries(t *testing.T) {
	info, _ := fixture(t)
	for key, values := range map[string][]string{
		"authentication": {"", "kerberos"}, "user": {"", "a b"}, "root_path": {"relative", "/addp/../secret", "/addp/", "/addp//x"},
		"rpc_uri":          {"hdfs://host", "http://host:8020", "hdfs://user@host:8020", "hdfs://host:8020/path", "hdfs://host:0", "hdfs://host:70000"},
		"webhdfs_endpoint": {"http://host/path", "http://user:password@host", "http://host?secret=x", "file:///tmp"},
	} {
		for _, value := range values {
			t.Run(key+"/"+value, func(t *testing.T) {
				clone := plugin.ConnectionInfo{}
				for k, v := range info {
					clone[k] = v
				}
				clone[key] = value
				if _, err := ParseConnectionInfo(clone); err == nil {
					t.Fatal("invalid configuration accepted")
				}
			})
		}
	}
	c, err := ParseConnectionInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := c.NativeURI(" space%名.csv")
	if err != nil || uri != "hdfs://namenode:8020/addp/%20space%25%E5%90%8D.csv" {
		t.Fatalf("native URI=%s %v", uri, err)
	}
	for _, raw := range []string{"../secret", "/secret", "x/../secret", "x//secret", "x/./secret", "\x00"} {
		if _, err := c.NativeURI(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, name := range []string{"..", ".", "x/y", "", "\x00"} {
		p := plugin.FileRootPath(7)
		p.Segments = append(p.Segments, plugin.EngineCatalogSegment{Term: "file", Kind: "file", Name: name})
		if _, err := (&HDFSPlugin{}).ResolvePath(context.Background(), info, p); err == nil {
			t.Fatalf("accepted raw segment %q", name)
		}
	}
}

func TestHDFSReadOnlyDescriptor(t *testing.T) {
	p := &HDFSPlugin{}
	d, err := plugin.DescribeEngineType(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Capabilities.EngineFamily != "file" || d.CatalogModel.RootTerm != "root" || d.Capabilities.Storage.Store.StreamWrite || d.Capabilities.Storage.Store.Delete {
		t.Fatalf("invalid descriptor: %#v", d)
	}
	if _, ok := interface{}(p).(plugin.ContentWritableProvider); ok {
		t.Fatal("HDFS must not implement writing")
	}
}

func TestHDFSRejectsUnsafeDataNodeLocation(t *testing.T) {
	for _, location := range []string{"http://other/webhdfs/v1/secret?op=OPEN", "file:///tmp", "http://user:password@other/webhdfs/v1/addp/a.csv", "http://other/webhdfs/v1/addp/a.csv?op=DELETE"} {
		t.Run(location, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]string{"Location": location})
			}))
			defer s.Close()
			c := &client{&Config{Endpoint: mustURL(t, s.URL), Root: "/addp", User: "reader"}}
			if _, err := c.open(context.Background(), "a.csv", 0, 3); err == nil {
				t.Fatal("unsafe location accepted")
			}
		})
	}
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	u, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
