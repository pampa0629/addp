package mysql

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

func TestMySQLCatalogFieldTypeMapsNativeTypes(t *testing.T) {
	if mysqlCatalogFactsDialect.MapFieldType == nil {
		t.Fatal("MySQL CatalogFacts dialect must declare its field type mapper")
	}
	tests := map[string]datatype.FieldType{
		"int":           datatype.FieldTypeInt,
		"bigint":        datatype.FieldTypeBigInt,
		"decimal(18,2)": datatype.FieldTypeDecimal,
		"varchar(255)":  datatype.FieldTypeString,
		"tinyint(1)":    datatype.FieldTypeBool,
		"geometry":      datatype.FieldTypeGeometry,
	}
	for nativeType, want := range tests {
		if got := mysqlCatalogFactsDialect.MapFieldType(nativeType); got != want {
			t.Fatalf("mysqlCatalogFieldType(%q) = %q, want %q", nativeType, got, want)
		}
	}
}

func TestMySQLCapabilitiesDeclareEWKBTableWriteEncoding(t *testing.T) {
	capabilities := (&MySQLPlugin{}).Capabilities()
	if capabilities.Storage == nil || capabilities.Storage.Store == nil || capabilities.Storage.Store.TableSpatialEncoding == nil {
		t.Fatal("MySQL capabilities do not declare table spatial encoding")
	}
	encodings := capabilities.Storage.Store.TableSpatialEncoding.GeometryWriteEncodings
	if len(encodings) != 1 || encodings[0] != string(format.GeometryEncodingEWKB) {
		t.Fatalf("geometry write encodings = %#v, want [ewkb]", encodings)
	}
}

func TestMySQLCapabilitiesDeclareIdempotentTableUpsert(t *testing.T) {
	capabilities := (&MySQLPlugin{}).Capabilities()
	if capabilities.Storage == nil || capabilities.Storage.Store == nil || capabilities.Storage.Store.TableUpsert == nil {
		t.Fatal("MySQL capabilities do not declare table upsert")
	}
	if !capabilities.Storage.Store.TableUpsert.Supported || !capabilities.Storage.Store.TableUpsert.Idempotent {
		t.Fatalf("table upsert capability = %#v, want supported idempotent", capabilities.Storage.Store.TableUpsert)
	}
}

func TestMySQLCapabilitiesDeclareAtomicPartitionedTableChangeApply(t *testing.T) {
	capabilities := (&MySQLPlugin{}).Capabilities()
	apply := capabilities.Storage.Store.PartitionedTableChangeApply
	if apply == nil || !apply.Supported || !apply.AtomicPositionCommit || !apply.Monotonic {
		t.Fatalf("partitioned table change apply capability = %#v", apply)
	}
	for _, operation := range []string{plugin.TableChangeOperationUpsert, plugin.TableChangeOperationDelete, plugin.TableChangeOperationSkip} {
		found := false
		for _, declared := range apply.Operations {
			if declared == operation {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("partitioned table change apply operations = %#v, missing %q", apply.Operations, operation)
		}
	}
}
