package parquet

import "testing"

func TestIsTableFileExt(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{".parquet", ".orc", ".avro", ".PARQUET"} {
		if !IsTableFileExt(ext) {
			t.Fatalf("IsTableFileExt(%q) = false, want true", ext)
		}
	}
	if IsTableFileExt(".csv") {
		t.Fatal("IsTableFileExt(.csv) = true, want false")
	}
}

func TestIsTableFileType(t *testing.T) {
	t.Parallel()

	for _, fileType := range []string{"parquet", "orc", "avro", " Parquet "} {
		if !IsTableFileType(fileType) {
			t.Fatalf("IsTableFileType(%q) = false, want true", fileType)
		}
	}
	if IsTableFileType("json") {
		t.Fatal("IsTableFileType(json) = true, want false")
	}
}

func TestLogicalTableName(t *testing.T) {
	t.Parallel()

	if got := LogicalTableName("orders.parquet"); got != "orders" {
		t.Fatalf("LogicalTableName() = %q, want orders", got)
	}
	if got := LogicalTableName(".parquet"); got != ".parquet" {
		t.Fatalf("LogicalTableName() empty base = %q, want .parquet", got)
	}
	if got := LogicalTableName("orders"); got != "orders" {
		t.Fatalf("LogicalTableName() no ext = %q, want orders", got)
	}
}
