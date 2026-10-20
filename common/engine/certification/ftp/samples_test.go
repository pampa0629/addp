package ftp

import (
	"os"
	"os/exec"
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
	directory := t.TempDir()
	if output, err := exec.Command("python3", "../../../../business/ftp/seed.py", directory).CombinedOutput(); err != nil {
		t.Fatalf("generate Business samples: %v %s", err, output)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "examples" || !entries[0].IsDir() {
		t.Fatal("Business seed must only create the examples directory at the account root")
	}
	actual, err := parquet.ReadFile[row](filepath.Join(directory, "examples", "客户 示例.parquet"))
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
