package tidb

import "testing"

func TestTiDBAnalyticalVersionAndComment(t *testing.T) {
	for _, tc := range []struct {
		version, comment string
		want             bool
	}{
		{"8.0.11-TiDB-v8.5.8", "TiDB Server (Apache License 2.0) Community Edition, MySQL 8.0 compatible", true},
		{"8.0.11-TiDB-v8.4.0", "TiDB Server (Apache License 2.0) Community Edition", false},
		{"8.0.11", "MySQL Community Server - GPL", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			if got := tidbAnalyticalVersionSupported(tc.version, tc.comment); got != tc.want {
				t.Fatalf("tidbAnalyticalVersionSupported(%q, %q) = %t, want %t", tc.version, tc.comment, got, tc.want)
			}
		})
	}
}

func TestTiDBAnalyticalInstanceRejectsUncertifiedFacts(t *testing.T) {
	valid := tidbAnalyticalInstanceFacts{
		Version: "8.0.11-TiDB-v8.5.8", Comment: "TiDB Server (Apache License 2.0) Community Edition",
		ClientCharset: "utf8mb4", ConnectionCharset: "utf8mb4", ResultsCharset: "utf8mb4", Isolation: "REPEATABLE-READ",
	}
	if report := checkTiDBAnalyticalInstanceFacts(valid); !report.Supported {
		t.Fatal(report)
	}
	for _, tc := range []struct {
		name, code string
		change     func(*tidbAnalyticalInstanceFacts)
	}{
		{"server", "analytical_server_version", func(f *tidbAnalyticalInstanceFacts) { f.Version = "8.0.11" }},
		{"client charset", "analytical_encoding", func(f *tidbAnalyticalInstanceFacts) { f.ClientCharset = "latin1" }},
		{"connection charset", "analytical_encoding", func(f *tidbAnalyticalInstanceFacts) { f.ConnectionCharset = "latin1" }},
		{"results charset", "analytical_encoding", func(f *tidbAnalyticalInstanceFacts) { f.ResultsCharset = "" }},
		{"isolation", "analytical_isolation", func(f *tidbAnalyticalInstanceFacts) { f.Isolation = "READ-UNCOMMITTED" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := valid
			tc.change(&facts)
			report := checkTiDBAnalyticalInstanceFacts(facts)
			if report.Supported || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != tc.code {
				t.Fatal(report)
			}
		})
	}
}
