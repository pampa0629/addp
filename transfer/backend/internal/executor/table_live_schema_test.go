package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestNativeProtectionChecksLiveFactsBeforeOpeningReader(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString, NativeType: "text", Nullable: true}}
	for _, tc := range []struct {
		name      string
		catalog   plugin.EngineCatalogFactsProvider
		mismatch  bool
		allowed   bool
		unmanaged bool
	}{
		{name: "missing catalog"},
		{name: "unmanaged source without catalog", allowed: true, unmanaged: true},
		{name: "catalog failure", catalog: &liveSchemaCatalog{err: errors.New("catalog unavailable")}},
		{name: "missing structure", catalog: &liveSchemaCatalog{}, mismatch: true},
		{name: "added field", catalog: &liveSchemaCatalog{fields: append(append([]datatype.FieldInfo(nil), fields...), datatype.FieldInfo{Name: "extra", Type: datatype.FieldTypeInt})}, mismatch: true},
		{name: "changed type", catalog: &liveSchemaCatalog{fields: []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeBigInt, Nullable: true}}}, mismatch: true},
		{name: "same structure with richer display facts", catalog: &liveSchemaCatalog{fields: []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString, NativeType: "varchar(32)", Comment: "contact", Nullable: true}}}, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opener := &liveSchemaSessionOpener{fakeBatchReader: &fakeBatchReader{}}
			protector := &liveSchemaProtector{unmanaged: tc.unmanaged}
			export := &TableTransferExecutor{SourceCatalogFacts: tc.catalog, SourceTableSessionProvider: opener, SourceProtector: protector}
			source, err := export.openSource(TableSourcePlan{Kind: TableEndpointNative, TableInfo: &datatype.TableInfo{Fields: fields}})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := source.Open(t.Context())
			if tc.allowed {
				if err != nil || reader == nil || opener.opens != 1 {
					t.Fatalf("matching structure was not admitted: opens=%d error=%v", opener.opens, err)
				}
				if err := reader.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else if reader != nil || !errors.Is(err, ErrProtectionPreparation) || opener.opens != 0 || tc.mismatch && !errors.Is(err, dataprotection.ErrSchemaMismatch) {
				t.Fatalf("unsafe native source admission: opens=%d error=%v", opener.opens, err)
			}
			if protector.calls != 1 {
				t.Fatal("live schema check must preserve a single prepared protection view")
			}
		})
	}
}

func TestGuardedArchiveSourceChecksLockedSchemaAndCurrentGrant(t *testing.T) {
	path := plugin.TabularItemPath(1, "database", "business", "roads")
	exact, _ := plugin.NewQueryReadSet(path)
	sibling, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(1, "database", "business", "sibling"))
	fields := []datatype.FieldInfo{{Name: "name", Type: datatype.FieldTypeString, Nullable: true}}
	changed := append(datatype.CloneFieldInfos(fields), datatype.FieldInfo{Name: "private_field", Type: datatype.FieldTypeString})
	revoked := errors.New("current source Grant revoked")
	for _, tc := range []struct {
		name   string
		proof  *plugin.QueryReadSet
		fields []datatype.FieldInfo
		grant  error
		calls  int
		want   error
	}{
		{"exact", exact, fields, nil, 1, nil},
		{"schema changed", exact, changed, nil, 0, dataprotection.ErrSchemaMismatch},
		{"sibling table", sibling, fields, nil, 0, plugin.ErrQueryReadSetUnresolved},
		{"revoked", exact, fields, revoked, 1, revoked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			opener := &guardedSchemaSessionOpener{liveSchemaSessionOpener: liveSchemaSessionOpener{fakeBatchReader: &fakeBatchReader{}}, proof: tc.proof, fields: tc.fields}
			transfer := &TableTransferExecutor{
				SourceTableSessionProvider: opener, SourceProtector: &liveSchemaProtector{},
				AuthorizeNativeSource: func(context.Context, *plugin.QueryReadSet, []datatype.FieldInfo) error {
					calls++
					return tc.grant
				},
			}
			source, err := transfer.openSource(TableSourcePlan{Kind: TableEndpointNative, Path: path, TableInfo: &datatype.TableInfo{Fields: fields}})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := source.Open(t.Context())
			if !errors.Is(err, tc.want) || calls != tc.calls || opener.opens != 0 || opener.opened != (tc.want == nil) {
				t.Fatalf("unexpected admission: err=%v grant calls=%d ordinary opens=%d guarded opened=%v", err, calls, opener.opens, opener.opened)
			}
			if reader != nil {
				if err := reader.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type guardedSchemaSessionOpener struct {
	liveSchemaSessionOpener
	proof  *plugin.QueryReadSet
	fields []datatype.FieldInfo
	opened bool
}

func (p *guardedSchemaSessionOpener) OpenGuardedTableReadSession(ctx context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath,
	_ plugin.TableReadSessionOptions, authorize plugin.GuardedTableReadAuthorizer) (plugin.TableReadSession, error) {
	if err := authorize(ctx, p.proof, p.fields); err != nil {
		return nil, err
	}
	p.opened = true
	return &guardedSchemaSession{fakeTableReadSession: fakeTableReadSession{reader: p.fakeBatchReader}, proof: p.proof}, nil
}

type guardedSchemaSession struct {
	fakeTableReadSession
	proof *plugin.QueryReadSet
}

func (s *guardedSchemaSession) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return s.proof, nil
}

type liveSchemaCatalog struct {
	plugin.EnginePlugin
	fields []datatype.FieldInfo
	err    error
}

func (p *liveSchemaCatalog) DescribeEngineCatalogFacts(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return &plugin.EngineCatalogFacts{Table: &datatype.TableInfo{Fields: p.fields}}, p.err
}

type liveSchemaSessionOpener struct {
	*fakeBatchReader
	opens int
}

func (p *liveSchemaSessionOpener) OpenTableReadSession(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	p.opens++
	return p.fakeBatchReader.OpenTableReadSession(ctx, conn, path, opts)
}

type liveSchemaProtector struct {
	calls     int
	unmanaged bool
}

func (p *liveSchemaProtector) PrepareCatalogTableProtection(_ context.Context, _ plugin.EngineCatalogPath, fields []datatype.FieldInfo) (*dataprotection.PreparedTableProtection, error) {
	p.calls++
	prepared := &dataprotection.PreparedTableProtection{Apply: func(*plugin.QueryResult) error { return nil }}
	if !p.unmanaged {
		expected, _ := dataprotection.TableSchemaSnapshotHash(fields)
		prepared.ValidateSourceSchema = func(current []datatype.FieldInfo) error {
			actual, err := dataprotection.TableSchemaSnapshotHash(current)
			if err != nil || expected != actual {
				return dataprotection.ErrSchemaMismatch
			}
			return nil
		}
	}
	return prepared, nil
}
func (*liveSchemaProtector) PrepareQueryProtection(context.Context, plugin.PreparedQuery) (*dataprotection.PreparedTableProtection, error) {
	return nil, errors.New("unexpected query source")
}
