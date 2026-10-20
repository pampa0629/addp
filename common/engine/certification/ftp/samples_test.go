package ftp

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestBusinessParquetSample(t *testing.T) {
	type row struct {
		ID     int64   `parquet:"id"`
		Name   string  `parquet:"name"`
		Amount float64 `parquet:"amount"`
	}
	want := []row{{1, "长沙客户", 100.25}, {2, "株洲客户", 200.50}, {3, "湘潭客户", 300.75}}
	file := filepath.Join(t.TempDir(), "sample.parquet")
	if err := parquet.WriteFile(file, want); err != nil {
		t.Fatal(err)
	}
	if export := os.Getenv("ADDP_FTP_SAMPLE_EXPORT"); export != "" {
		// One-time fixture regeneration is restricted to OS temporary storage.
		if !filepath.IsAbs(export) || !bytes.HasPrefix([]byte(filepath.Clean(export)), []byte(filepath.Clean(os.TempDir())+string(os.PathSeparator))) {
			t.Fatal("sample export must be under OS temporary directory")
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(export, data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	actual, err := parquet.ReadFile[row]("../../../../business/ftp/samples/客户 示例.parquet")
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(want) {
		t.Fatal("Parquet sample is incomplete")
	}
	for i := range want {
		if actual[i] != want[i] {
			t.Fatal("Parquet sample changed")
		}
	}
}
