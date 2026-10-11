package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
)

type forbiddenDefinitionDelete struct {
	*postgresql.PostgreSQLPlugin
	calls int
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
