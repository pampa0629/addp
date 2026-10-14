package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/clickhouse"
	"github.com/addp/common/engine/plugins/dameng"
	"github.com/addp/common/engine/plugins/doris"
	"github.com/addp/common/engine/plugins/kingbase"
	"github.com/addp/common/engine/plugins/mysql"
	"github.com/addp/common/engine/plugins/oceanbase"
	"github.com/addp/common/engine/plugins/opengauss"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/engine/plugins/tidb"
)

type forbiddenDefinitionDelete struct {
	*postgresql.PostgreSQLPlugin
	calls int
}

func TestClickHouseDefinitionRejectsBeforeReplaceEffects(t *testing.T) {
	testDefinitionRejectsBeforeReplaceEffects(t, &clickhouse.ClickHousePlugin{})
}

func TestDM8DefinitionRejectsBeforeReplaceEffects(t *testing.T) {
	testDefinitionRejectsBeforeReplaceEffects(t, &dameng.Plugin{})
}

func testDefinitionRejectsBeforeReplaceEffects(t *testing.T, p engineplugin.EnginePlugin) {
	fields := []datatype.FieldInfo{
		{Name: "payload", Type: datatype.FieldTypeArray},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3},
		{Name: "shape", Type: datatype.FieldTypeGeometry},
	}
	if p.Type() == "dameng" {
		fields = append(fields, datatype.FieldInfo{Name: "payload", Type: datatype.FieldTypeJSON, PrimaryKey: true}, datatype.FieldInfo{Name: "payload", Type: datatype.FieldTypeBytes, PrimaryKey: true})
	}
	for _, field := range fields {
		deleter := &forbiddenDefinitionDelete{}
		target := &nativeTableBatchTarget{definitionChecker: tableWriteDefinitionChecker(p), deleter: &engineTargetResourceDeleter{provider: deleter}, preparer: p.(engineplugin.TableWritePreparer)}
		_, err := target.Open(t.Context(), &datatype.TableInfo{Fields: []datatype.FieldInfo{field}}, nil)
		if err == nil || !strings.Contains(err.Error(), `column "`+field.Name+`"`) || deleter.calls != 0 {
			t.Fatalf("invalid definition reached replace: delete=%d error=%v", deleter.calls, err)
		}
	}
}

func TestDorisDefinitionRejectsBeforeReplaceEffects(t *testing.T) {
	p := &doris.DorisPlugin{}
	for _, fields := range [][]datatype.FieldInfo{
		{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "payload", Type: datatype.FieldTypeArray}},
		{{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3}},
		{{Name: "payload", Type: datatype.FieldTypeJSON}},
	} {
		deleter := &forbiddenDefinitionDelete{}
		target := &nativeTableBatchTarget{definitionChecker: tableWriteDefinitionChecker(p), deleter: &engineTargetResourceDeleter{provider: deleter}, preparer: p}
		_, err := target.Open(t.Context(), &datatype.TableInfo{Fields: fields}, nil)
		if err == nil || !strings.Contains(err.Error(), "target_definition") || deleter.calls != 0 {
			t.Fatalf("invalid definition reached replace: delete=%d error=%v", deleter.calls, err)
		}
	}
}

func TestMySQLMixedDefinitionRejectsBeforeReplaceEffects(t *testing.T) {
	p := &mysql.MySQLPlugin{}
	for _, field := range []datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeArray}, {Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3}} {
		deleter := &forbiddenDefinitionDelete{}
		target := &nativeTableBatchTarget{definitionChecker: tableWriteDefinitionChecker(p), deleter: &engineTargetResourceDeleter{provider: deleter}, preparer: p}
		_, err := target.Open(t.Context(), &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "shape", Type: datatype.FieldTypeGeometry}, field}}, datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4326, 2))
		if err == nil || !strings.Contains(err.Error(), `column "`+field.Name+`"`) || deleter.calls != 0 {
			t.Fatalf("invalid mixed definition reached replace: delete=%d error=%v", deleter.calls, err)
		}
	}
}

func TestProtocolWriteDefinitionRejectsBeforeReplaceEffects(t *testing.T) {
	for _, provider := range []engineplugin.EnginePlugin{&opengauss.Plugin{}, &kingbase.Plugin{}, &tidb.Plugin{}, &oceanbase.Plugin{}} {
		for _, field := range []datatype.FieldInfo{
			{Name: "members", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON},
			{Name: "shape", Type: datatype.FieldTypeGeometry},
			{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3},
		} {
			t.Run(provider.Type()+"/"+field.Name, func(t *testing.T) {
				deleter := &forbiddenDefinitionDelete{}
				target := &nativeTableBatchTarget{definitionChecker: tableWriteDefinitionChecker(provider), deleter: &engineTargetResourceDeleter{provider: deleter}, preparer: provider.(engineplugin.TableWritePreparer)}
				if _, err := target.Open(t.Context(), &datatype.TableInfo{Fields: []datatype.FieldInfo{field}}, nil); err == nil {
					t.Fatal("unsupported definition reached target")
				}
				if deleter.calls != 0 {
					t.Fatalf("replace delete called %d times", deleter.calls)
				}
			})
		}
	}
}

func (d *forbiddenDefinitionDelete) DeleteResource(context.Context, engineplugin.ConnectionInfo, engineplugin.EngineCatalogPath) error {
	d.calls++
	return nil
}

func TestNativeWriteDefinitionRejectsBeforeTargetEffects(t *testing.T) {
	pg := &postgresql.PostgreSQLPlugin{}
	deleter := &forbiddenDefinitionDelete{}
	target := &nativeTableBatchTarget{definitionChecker: tableWriteDefinitionChecker(pg), deleter: &engineTargetResourceDeleter{provider: deleter}, preparer: pg}
	_, err := target.Open(context.Background(), &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "members", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "array_element_type_unsupported") {
		t.Fatalf("got %v", err)
	}
	if deleter.calls != 0 {
		t.Fatalf("delete called %d times", deleter.calls)
	}
	// Invalid connection info also proves Prepare/Open were not invoked.
}
