//go:build hdfs_formats

package hdfs_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	fileformat "github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/json"
	_ "github.com/addp/common/format/plugins/parquet"
)

// TestIntegrationHDFSFormats consumes provider bytes exported by the owned
// Linux cluster contract. Format plugins use the host's native CGO toolchain;
// the portable WebHDFS contract binary keeps only HDFS dependencies.
func TestIntegrationHDFSFormats(t *testing.T) {
	dir := os.Getenv("ADDP_HDFS_SAMPLE_DIR")
	if os.Getenv("ADDP_HDFS_INTEGRATION") != "1" || dir == "" {
		t.Fatal("owned HDFS sample directory required")
	}
	for _, kind := range []string{"csv", "json", "parquet"} {
		t.Run(kind, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(dir, "orders."+kind))
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := fileformat.GetTableSampleReader(fileformat.FormatType(kind))
			if err != nil {
				t.Fatal(err)
			}
			rows, err := consumer.SampleTable(context.Background(), bytes.NewReader(body), 0, 20, fileformat.DefaultParseOptions())
			if err != nil || len(rows) != 20 {
				t.Fatalf("shared parser rows=%d err=%v", len(rows), err)
			}
			var total int64
			for _, row := range rows {
				amount, err := strconv.ParseInt(fmt.Sprint(row["amount"]), 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				total += amount
			}
			if total != 2100 {
				t.Fatalf("shared parser aggregate=%d", total)
			}
		})
	}
}
