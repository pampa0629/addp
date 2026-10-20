package parquet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
)

// The same source is exercised through scan, pagination and continuous reads.
// Full Open calls are forbidden when the source exposes usable byte ranges.
func TestParquetScopeReadPathsUseRanges(t *testing.T) {
	for _, operation := range []string{"describe", "sample", "continuous"} {
		t.Run(operation, func(t *testing.T) {
			path := "dataset/part-000.parquet"
			reader := parquetMemoryContentReader{
				data: map[string][]byte{path: buildParquetRows(t,
					testParquetRow{ID: 1, Name: "Alice"},
					testParquetRow{ID: 2, Name: "Bob"})},
				openCounts: map[string]int{}, rangeOpenCounts: map[string]int{},
			}
			scope := contentio.NewRef("dataset", contentio.RoleScope)
			plugin := NewPlugin()
			ctx := context.Background()
			switch operation {
			case "describe":
				result, err := plugin.DescribeTableScope(ctx, reader, scope, nil)
				if err != nil {
					t.Fatal(err)
				}
				if result.Table.RowCount == nil || *result.Table.RowCount != 2 {
					t.Fatalf("row count = %v", result.Table.RowCount)
				}
			case "sample":
				rows, err := plugin.SampleTableScope(ctx, reader, scope, 1, 1, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 1 || rows[0]["name"] != "Bob" {
					t.Fatalf("rows = %#v", rows)
				}
			case "continuous":
				r, err := plugin.OpenTableScopeReader(ctx, reader, scope, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close(ctx)
				rows, err := r.ReadRows(ctx, 2)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 2 || rows[1]["name"] != "Bob" {
					t.Fatalf("rows = %#v", rows)
				}
			}
			if reader.openCounts[path] != 0 || reader.rangeOpenCounts[path] == 0 {
				t.Fatalf("full opens = %d, range opens = %d; require only ranges", reader.openCounts[path], reader.rangeOpenCounts[path])
			}
		})
	}
}

// Count consumed bytes, not just requests: metadata scanning must not load data
// pages, and a deep page must not read all the preceding row groups.
func TestParquetScopeRangesDoNotDownloadWholeFile(t *testing.T) {
	sourceRows := make([]testParquetRow, 2000)
	for i := range sourceRows {
		sourceRows[i] = testParquetRow{ID: int64(i + 1), Name: strings.Repeat(fmt.Sprintf("%064x", i), 4)}
	}
	data := buildParquetRowsWithMaxRowsPerRowGroup(t, 100, sourceRows...)
	for _, sample := range []bool{false, true} {
		t.Run(fmt.Sprintf("sample=%t", sample), func(t *testing.T) {
			reader := newParquetRangeAuditReader(data)
			scope := contentio.NewRef("dataset", contentio.RoleScope)
			if sample {
				rows, err := NewPlugin().SampleTableScope(context.Background(), reader, scope, 1500, 2, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 2 || rows[0]["id"] != int64(1501) || rows[0]["name"] != sourceRows[1500].Name {
					t.Fatalf("rows = %#v", rows)
				}
			} else {
				result, err := NewPlugin().DescribeTableScope(context.Background(), reader, scope, nil)
				if err != nil {
					t.Fatal(err)
				}
				if result.Table.RowCount == nil || *result.Table.RowCount != 2000 {
					t.Fatalf("row count = %v", result.Table.RowCount)
				}
			}
			if reader.bytesRead <= 0 || reader.bytesRead >= len(data)/2 {
				t.Fatalf("consumed %d of %d bytes; expected footer/selected groups only", reader.bytesRead, len(data))
			}
			if reader.openCounts["dataset/part.parquet"] != 0 || reader.closes != reader.rangeOpenCounts["dataset/part.parquet"] {
				t.Fatalf("reader not range-only or leaked a range: %#v", reader)
			}
			t.Logf("consumed %d of %d bytes", reader.bytesRead, len(data))
		})
	}
}

func TestParquetScopeRangeFailureDoesNotReopenWholeFile(t *testing.T) {
	sentinel := errors.New("range unavailable")
	for _, sample := range []bool{false, true} {
		reader := newParquetRangeAuditReader(buildParquetRows(t, testParquetRow{ID: 1}))
		reader.rangeErr = sentinel
		scope := contentio.NewRef("dataset", contentio.RoleScope)
		var err error
		if sample {
			_, err = NewPlugin().SampleTableScope(context.Background(), reader, scope, 0, 1, nil)
		} else {
			_, err = NewPlugin().DescribeTableScope(context.Background(), reader, scope, nil)
		}
		if !errors.Is(err, sentinel) || reader.openCounts["dataset/part.parquet"] != 0 {
			t.Fatalf("error = %v, full opens = %#v", err, reader.openCounts)
		}
	}
}

func TestParquetScopeStreamInputsAreClosed(t *testing.T) {
	for _, sample := range []bool{false, true} {
		for _, corrupt := range []bool{false, true} {
			for _, noRange := range []bool{false, true} {
				t.Run(fmt.Sprintf("sample=%t/corrupt=%t/noRange=%t", sample, corrupt, noRange), func(t *testing.T) {
					data := buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"})
					if corrupt {
						data = []byte("not parquet")
					}
					reader := newParquetRangeAuditReader(data)
					reader.unknownSize = !noRange
					var source contentio.Reader = reader
					if noRange {
						source = parquetStreamOnlyContentReader{Reader: reader, Lister: reader}
					}
					scope := contentio.NewRef("dataset", contentio.RoleScope)
					var err error
					if sample {
						rows, readErr := NewPlugin().SampleTableScope(context.Background(), source, scope, 0, 1, nil)
						err = readErr
						if !corrupt && (len(rows) != 1 || rows[0]["name"] != "Alice") {
							t.Fatalf("rows = %#v", rows)
						}
					} else {
						_, err = NewPlugin().DescribeTableScope(context.Background(), source, scope, nil)
					}
					if (err != nil) != corrupt {
						t.Fatalf("error = %v, corrupt = %t", err, corrupt)
					}
					if reader.closes != reader.openCounts["dataset/part.parquet"] || reader.closes == 0 || reader.rangeOpenCounts["dataset/part.parquet"] != 0 {
						t.Fatalf("stream leak or unexpected range: %#v", reader)
					}
				})
			}
		}
	}
}

type parquetRangeAuditReader struct {
	parquetMemoryContentReader
	bytesRead, closes int
	rangeErr          error
}

func newParquetRangeAuditReader(data []byte) *parquetRangeAuditReader {
	return &parquetRangeAuditReader{parquetMemoryContentReader: parquetMemoryContentReader{
		data:       map[string][]byte{"dataset/part.parquet": data},
		openCounts: map[string]int{}, rangeOpenCounts: map[string]int{},
	}}
}

func (r *parquetRangeAuditReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	input, err := r.parquetMemoryContentReader.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &parquetCountedInput{ReadCloser: input, owner: r}, nil
}

func (r *parquetRangeAuditReader) OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	if r.rangeErr != nil {
		return nil, r.rangeErr
	}
	input, err := r.parquetMemoryContentReader.OpenRange(ctx, ref, offset, length)
	if err != nil {
		return nil, err
	}
	return &parquetCountedInput{ReadCloser: input, owner: r}, nil
}

type parquetCountedInput struct {
	io.ReadCloser
	owner *parquetRangeAuditReader
}

func (r *parquetCountedInput) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.owner.bytesRead += n
	return n, err
}

func (r *parquetCountedInput) Close() error {
	r.owner.closes++
	return r.ReadCloser.Close()
}

type parquetStreamOnlyContentReader struct {
	contentio.Reader
	contentio.Lister
}
