package highgo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/lib/pq"
)

func TestHighGoSM3ConnectionAndGORMPool(t *testing.T) {
	for _, pool := range []bool{false, true} {
		t.Run(fmt.Sprintf("pool=%t", pool), func(t *testing.T) {
			info := newSM3Server(t, "瀚高数据库管理系统V4.5, x86_64平台, 创建日期:20250227")
			p := &Plugin{}
			if !pool {
				if err := p.TestConnection(t.Context(), info); err != nil {
					t.Fatalf("SM3 connection: %v", err)
				}
				return
			}
			db, err := p.CreateConnectionPool(info, &plugin.PoolConfig{MaxOpenConns: 1, MaxIdleConns: 1})
			if err != nil {
				t.Fatalf("SM3 GORM pool must use the highgo driver: %v", err)
			}
			native, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			defer native.Close()
			var version string
			if err := db.Raw("SELECT version()").Scan(&version).Error; err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(version, "V4.5") {
				t.Fatalf("unexpected version: %s", version)
			}
		})
	}
}

func TestHighGoRejectsWrongPassword(t *testing.T) {
	info := newSM3Server(t, "瀚高数据库管理系统V4.5")
	info["password"] = "wrong-password"
	err := (&Plugin{}).TestConnection(t.Context(), info)
	var databaseError *pq.Error
	if !errors.As(err, &databaseError) || databaseError.SQLState() != "28P01" {
		t.Fatalf("wrong SM3 password must retain the common typed SQLSTATE contract: %v", err)
	}
}

func TestHighGoDriverDoesNotReplacePostgreSQL(t *testing.T) {
	info := newSM3Server(t, "瀚高数据库管理系统V4.5")
	err := (&postgresql.PostgreSQLPlugin{}).TestConnection(t.Context(), info)
	if err == nil || !strings.Contains(err.Error(), "unknown authentication response: 13") {
		t.Fatalf("ordinary PostgreSQL must retain its original driver: %v", err)
	}
}

func TestHighGoRejectsUnverifiedServerVersion(t *testing.T) {
	for _, version := range []string{
		"PostgreSQL 12.7",
		"瀚高数据库管理系统V4.50",
		"瀚高数据库管理系统V4.5evil",
		"HighGo Database Management System 4.50 on aarch64",
		"HighGo Database Management System 4.6 on aarch64",
		"Other Database 瀚高数据库管理系统V4.5",
	} {
		t.Run(version, func(t *testing.T) {
			info := newSM3Server(t, version)
			if err := (&Plugin{}).TestConnection(t.Context(), info); !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("unexpected server identity accepted: %v", err)
			}
		})
	}
}

func TestHighGoNativeVersionLanguages(t *testing.T) {
	for _, version := range []string{
		"瀚高数据库管理系统V4.5, x86_64平台, 创建日期:20250227",
		"HighGo Database Management System 4.5 on aarch64,build on 20250227",
		"HighGo Database Management System 4.5.10.3 on aarch64",
	} {
		t.Run(version, func(t *testing.T) {
			info := newSM3Server(t, version)
			if err := (&Plugin{}).TestConnection(t.Context(), info); err != nil {
				t.Fatalf("native HighGo 4.5 rejected: %v", err)
			}
		})
	}
}

func TestHighGoOnlyDeclaresCertifiedSurface(t *testing.T) {
	p := &Plugin{}
	caps := p.Capabilities()
	store := caps.Storage.Store
	if caps.EngineType != "highgo" || !store.BatchRead || !store.TableReadSession || !caps.Compute.Query.ReadSession {
		t.Fatal("read capabilities missing or identity lost")
	}
	if !store.BatchWrite || !store.TableWriteSession || !store.TableWritePrepare || !store.Delete {
		t.Fatal("certified non-spatial write capabilities missing")
	}
	if store.TableUpsert != nil || store.PartitionedTableChangeApply != nil || store.TableSpatialEncoding != nil || caps.Storage.Facts.SpatialFacts {
		t.Fatal("uncertified capability declared")
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatalf("capability/interface contract: %v", err)
	}
	if _, ok := any(p).(plugin.ResourceDeleteProvider); !ok {
		t.Fatal("certified delete provider missing")
	}
	info := plugin.ConnectionInfo{"host": "localhost", "database": "test", "user": "user", "password": "fixture-password"}
	dsn, err := p.protocol().BuildDSN(info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "port=5866") || !strings.Contains(dsn, "sslmode=require") {
		t.Fatalf("owning connection defaults lost: %s", dsn)
	}
	if info["sslmode"] != nil {
		t.Fatal("connection input mutated while applying defaults")
	}
	info["sslmode"] = "disable"
	dsn, err = p.BuildDSN(info)
	if err != nil || !strings.Contains(dsn, "sslmode=disable") {
		t.Fatalf("explicit TLS setting lost: %s, %v", dsn, err)
	}
	if p.ConnectionSpec().DefaultPortValue() != 5866 {
		t.Fatal("descriptor default port does not match provider")
	}
	_, err = p.PrepareQuery(t.Context(), nil, plugin.QueryRequest{Language: "sql", Query: "DELETE FROM public.items"})
	if !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("writable query was not rejected before connecting: %v", err)
	}
	_, err = p.ExecuteSQL(t.Context(), nil, "DELETE FROM public.items", plugin.QueryOptions{})
	if !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("writable SQL was not rejected before connecting: %v", err)
	}
}

func TestHighGoAuditTableIsOutsideBusinessCatalog(t *testing.T) {
	p := &Plugin{}
	for _, name := range []string{"hg_t_audit_log", "pg_buffercache", "pg_stat_statements"} {
		t.Run(name, func(t *testing.T) {
			path := plugin.TabularItemPath(29, plugin.EngineCatalogTermSchema, "public", name)
			if _, err := p.ResolvePath(t.Context(), nil, path); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
				t.Fatalf("system object must be hidden before connecting: %v", err)
			}
			if _, err := p.DescribeEngineCatalogFacts(t.Context(), nil, path, plugin.EngineCatalogFactsOptions{}); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
				t.Fatalf("cached system object facts must be hidden before connecting: %v", err)
			}
		})
	}
}

func TestHighGoRejectsUnsupportedWriteDefinitionsBeforeConnecting(t *testing.T) {
	p := &Plugin{}
	for _, field := range []datatype.FieldInfo{
		{Name: "shape", Type: datatype.FieldTypeGeometry},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3},
		{Name: "objects", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON},
	} {
		t.Run(field.Name, func(t *testing.T) {
			fields := []datatype.FieldInfo{field}
			if err := p.PrepareTableWrite(t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteOptions{Fields: fields}); err == nil {
				t.Fatal("unsupported definition reached prepare")
			}
			if _, err := p.OpenTableWriteSession(t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteSessionOptions{Fields: fields}); err == nil {
				t.Fatal("unsupported definition reached COPY")
			}
			if err := p.WriteBatch(t.Context(), nil, plugin.EngineCatalogPath{}, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{field.Name: nil}}}, plugin.BatchWriteOptions{}); err == nil {
				t.Fatal("unsupported definition reached batch write")
			}
		})
	}
	// Runtime write paths must retain field metadata, rather than infer a type
	// from untrusted values and bypass the non-spatial definition check.
	if err := p.WriteBatch(t.Context(), nil, plugin.EngineCatalogPath{}, &plugin.BatchData{Rows: []map[string]interface{}{{"value": 1}}}, plugin.BatchWriteOptions{}); err == nil {
		t.Fatal("untyped batch was accepted")
	}
}

// Meta asks for optional enrichments even when an engine only exposes fields.
// Exercise the real HighGo pool and shared catalog queries, retaining ordinary
// facts without issuing PostGIS or uncertified relational enrichment queries.
func TestHighGoOrdinaryFactsWithOptionalEnrichments(t *testing.T) {
	info := newSM3Server(t, "瀚高数据库管理系统V4.5")
	const engineID = 987654
	engine := &plugin.Engine{ID: engineID, EngineType: "highgo", ConnectionInfo: info}
	db, err := plugin.GetOrCreatePoolFromFactory(engine, plugin.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	native, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { native.Close(); plugin.ClosePool(engineID) })
	path := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermSchema, "public", "items")
	facts, err := (&Plugin{}).DescribeEngineCatalogFacts(t.Context(), info, path, plugin.EngineCatalogFactsOptions{
		IncludeSpatialFacts: true, IncludeIndexes: true, IncludeConstraints: true, IncludePartitioning: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	table := plugin.EngineCatalogFactsTableInfo(facts)
	if table == nil || table.Name != "items" || len(table.Fields) != 1 || table.Fields[0].Name != "label" || facts.Spatial != nil {
		t.Fatalf("ordinary facts missing or unsupported spatial facts returned: %#v", facts)
	}
}

// newSM3Server is an in-process PostgreSQL protocol fixture. It authenticates
// against an independent fixed SM3 response and supports only diagnostic reads.
func newSM3Server(t *testing.T, version string) plugin.ConnectionInfo {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var connections sync.Map
	var handlers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(c, struct{}{})
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer connections.Delete(c)
				defer c.Close()
				if err := serveSM3(c, version); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					t.Errorf("SM3 protocol fixture: %v", err)
				}
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
		connections.Range(func(key, value any) bool { key.(net.Conn).Close(); return true })
		handlers.Wait()
	})
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return plugin.ConnectionInfo{"host": host, "port": port, "database": "test", "user": "user", "password": "fixture-password", "sslmode": "disable"}
}

func serveSM3(c net.Conn, version string) error {
	c.SetDeadline(time.Now().Add(10 * time.Second))
	var length uint32
	if err := binary.Read(c, binary.BigEndian, &length); err != nil {
		return err
	}
	if length < 8 || length > 65536 {
		return fmt.Errorf("invalid startup length")
	}
	startup := make([]byte, length-4)
	if _, err := io.ReadFull(c, startup); err != nil {
		return err
	}
	if binary.BigEndian.Uint32(startup) != 196608 {
		return fmt.Errorf("expected protocol 3 startup")
	}
	if err := sendMessage(c, 'R', []byte{0, 0, 0, 13, 1, 2, 3, 4}); err != nil {
		return err
	}
	kind, payload, err := readMessage(c)
	if err != nil {
		return err
	}
	const expected = "sm3b60121e46a4ec353a07a6f1cf0981a25ae532b6e7f8162d0cacab209424b113e\x00"
	if kind != 'p' || string(payload) != expected {
		return sendMessage(c, 'E', []byte("SFATAL\x00C28P01\x00Mpassword authentication failed\x00\x00"))
	}
	if err := sendMessage(c, 'R', []byte{0, 0, 0, 0}); err != nil {
		return err
	}
	for _, status := range []string{"server_version\x0012.7\x00", "client_encoding\x00UTF8\x00", "standard_conforming_strings\x00on\x00", "TimeZone\x00UTC\x00"} {
		if err := sendMessage(c, 'S', []byte(status)); err != nil {
			return err
		}
	}
	if err := sendMessage(c, 'Z', []byte{'I'}); err != nil {
		return err
	}

	var query string
	for {
		kind, payload, err = readMessage(c)
		if err != nil {
			return err
		}
		switch kind {
		case 'X':
			return nil
		case 'P':
			parts := bytes.SplitN(payload, []byte{0}, 3)
			if len(parts) != 3 {
				return fmt.Errorf("invalid Parse")
			}
			query = string(parts[1])
			if err := sendMessage(c, '1', nil); err != nil {
				return err
			}
		case 'B':
			if err := sendMessage(c, '2', nil); err != nil {
				return err
			}
		case 'D':
			if payload[0] == 'S' {
				var parameters bytes.Buffer
				count := int16(0)
				for _, placeholder := range []string{"$1", "$2"} {
					if strings.Contains(query, placeholder) {
						count++
					}
				}
				binary.Write(&parameters, binary.BigEndian, count)
				for i := int16(0); i < count; i++ {
					binary.Write(&parameters, binary.BigEndian, int32(25))
				}
				if err := sendMessage(c, 't', parameters.Bytes()); err != nil {
					return err
				}
			}
			names, _, err := fixtureQuery(query, version)
			if err != nil {
				return err
			}
			if err := sendDescription(c, names); err != nil {
				return err
			}
		case 'E':
			_, values, err := fixtureQuery(query, version)
			if err != nil {
				return err
			}
			if err := sendRow(c, values); err != nil {
				return err
			}
		case 'S':
			if err := sendMessage(c, 'Z', []byte{'I'}); err != nil {
				return err
			}
		case 'Q':
			query = strings.TrimSpace(strings.TrimSuffix(string(payload), "\x00"))
			if query == ";" {
				if err := sendMessage(c, 'I', nil); err != nil {
					return err
				}
			} else {
				names, values, err := fixtureQuery(query, version)
				if err != nil {
					return err
				}
				if err := sendDescription(c, names); err != nil {
					return err
				}
				if err := sendRow(c, values); err != nil {
					return err
				}
			}
			if err := sendMessage(c, 'Z', []byte{'I'}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported fixture message %q", kind)
		}
	}
}

func fixtureQuery(query, version string) ([]string, []string, error) {
	switch {
	case query == "SELECT version()":
		return []string{"version"}, []string{version}, nil
	case strings.Contains(query, "FROM information_schema.tables t"):
		return []string{"name", "table_type", "relkind", "kind"}, []string{"items", "BASE TABLE", "r", "table"}, nil
	case strings.Contains(query, "FROM information_schema.columns c"):
		return []string{"name", "data_type", "udt_name", "native_type", "nullable", "primary_key", "comment"}, []string{"label", "text", "text", "text", "true", "false", ""}, nil
	default:
		return nil, nil, fmt.Errorf("fixture refuses non-diagnostic or unsupported enrichment query: %s", query)
	}
}
func sendDescription(w io.Writer, names []string) error {
	var description bytes.Buffer
	binary.Write(&description, binary.BigEndian, int16(len(names)))
	for _, name := range names {
		description.WriteString(name + "\x00")
		for _, v := range []any{int32(0), int16(0), int32(25), int16(-1), int32(-1), int16(0)} {
			binary.Write(&description, binary.BigEndian, v)
		}
	}
	return sendMessage(w, 'T', description.Bytes())
}
func sendRow(w io.Writer, values []string) error {
	var row bytes.Buffer
	binary.Write(&row, binary.BigEndian, int16(len(values)))
	for _, value := range values {
		binary.Write(&row, binary.BigEndian, int32(len([]byte(value))))
		row.WriteString(value)
	}
	if err := sendMessage(w, 'D', row.Bytes()); err != nil {
		return err
	}
	return sendMessage(w, 'C', []byte("SELECT 1\x00"))
}

func sendMessage(w io.Writer, kind byte, payload []byte) error {
	var frame bytes.Buffer
	frame.WriteByte(kind)
	binary.Write(&frame, binary.BigEndian, uint32(len(payload)+4))
	frame.Write(payload)
	_, err := w.Write(frame.Bytes())
	return err
}
func readMessage(r io.Reader) (byte, []byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(head[1:])
	if length < 4 || length > 65536 {
		return 0, nil, fmt.Errorf("invalid message length")
	}
	payload := make([]byte, length-4)
	_, err := io.ReadFull(r, payload)
	return head[0], payload, err
}

func TestHighGoDeleteProviderPreservesCatalogBoundary(t *testing.T) {
	p := &Plugin{}
	for _, name := range []string{"hg_t_audit_log", "pg_buffercache", "pg_stat_statements"} {
		path := plugin.TabularItemPath(29, plugin.EngineCatalogTermSchema, "public", name)
		if err := p.DeleteResource(t.Context(), nil, path); err != nil {
			t.Fatalf("hidden object must be a no-op before connecting: %v", err)
		}
	}
	for _, path := range []plugin.EngineCatalogPath{
		plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 29),
		plugin.TabularNamespacePath(29, plugin.EngineCatalogTermSchema, "public"),
	} {
		if err := p.DeleteResource(t.Context(), nil, path); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("non-table delete not rejected: %v", err)
		}
	}
}
