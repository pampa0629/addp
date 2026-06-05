package scanflow

import (
	"context"
	"testing"
)

type catalogPathReporter struct {
	total   int
	message string
}

func (r *catalogPathReporter) SetTotal(total int) {
	r.total = total
}

func (r *catalogPathReporter) Advance(string, int, int, map[string]interface{}) {}

func (r *catalogPathReporter) Message(message string) {
	r.message = message
}

func TestResolveCatalogScanPathsUsesFallbackLister(t *testing.T) {
	reporter := &catalogPathReporter{}

	got, err := ResolveCatalogScanPaths(context.Background(), "empty", nil, nil, func(context.Context) ([]string, error) {
		return []string{"a", "b"}, nil
	}, reporter)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("paths = %#v", got)
	}
	if reporter.total != 2 {
		t.Fatalf("total = %d", reporter.total)
	}
}

func TestResolveCatalogScanPathsReportsEmpty(t *testing.T) {
	reporter := &catalogPathReporter{}

	got, err := ResolveCatalogScanPaths(context.Background(), "empty", nil, nil, nil, reporter)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	if got != nil {
		t.Fatalf("paths = %#v", got)
	}
	if reporter.message != "empty" || reporter.total != 0 {
		t.Fatalf("reporter = %#v", reporter)
	}
}
