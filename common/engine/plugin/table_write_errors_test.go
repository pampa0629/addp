package plugin_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestTableWriteErrorPreservesCauseAndActualColumnThroughWrapping(t *testing.T) {
	cause := errors.New("private driver evidence")
	err := plugin.WithTableWriteField(plugin.NewTableWriteError(plugin.TableWriteJSONInvalid, cause), 3, datatype.FieldTypeJSON)
	err = plugin.NewTableWriteError(plugin.TableWriteFailed, fmt.Errorf("write batch: %w", errors.Join(err, errors.New("cleanup failure"))))
	var typed *plugin.TableWriteError
	if !errors.Is(err, cause) || !errors.As(err, &typed) || typed.Code != plugin.TableWriteJSONInvalid || typed.Field == nil || typed.Field.ColumnIndex != 3 {
		t.Fatalf("lost typed cause or column: %v", err)
	}
	for _, field := range []*plugin.TableWriteField{nil, {ColumnIndex: 0, DeclaredType: datatype.FieldTypeJSON}, {ColumnIndex: 3, DeclaredType: datatype.FieldTypeBytes}, {ColumnIndex: 65536, DeclaredType: datatype.FieldTypeJSON}} {
		if field.Valid(plugin.TableWriteJSONInvalid) {
			t.Fatalf("accepted invalid field: %#v", field)
		}
	}
	if plugin.NewTableWriteError(plugin.TableWriteFailed, nil) != nil {
		t.Fatal("success became an error")
	}
}
