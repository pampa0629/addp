package mysql

import "testing"

func TestAnalyticalInstanceFacts(t *testing.T) {
	base := analyticalInstanceFacts{Version: "8.0.43", Comment: "MySQL Community Server - GPL", ClientCharset: "utf8mb4", ConnectionCharset: "utf8mb4", ResultsCharset: "utf8mb4", Isolation: "REPEATABLE-READ"}
	for _, tc := range []struct {
		name, code string
		change     func(*analyticalInstanceFacts)
	}{
		{"certified version", "", func(*analyticalInstanceFacts) {}},
		{"native commercial", "", func(f *analyticalInstanceFacts) {
			f.Version = "8.0.43-commercial"
			f.Comment = "MySQL Enterprise Server - Commercial"
		}},
		{"read committed", "", func(f *analyticalInstanceFacts) { f.Isolation = "READ-COMMITTED" }},
		{"serializable", "", func(f *analyticalInstanceFacts) { f.Isolation = "SERIALIZABLE" }},
		{"older version", "analytical_server_version", func(f *analyticalInstanceFacts) { f.Version = "5.7.44" }},
		{"newer version", "analytical_server_version", func(f *analyticalInstanceFacts) { f.Version = "8.4.7" }},
		{"unknown build", "analytical_server_version", func(f *analyticalInstanceFacts) { f.Version = "8.0.43-compatible" }},
		{"compatible product", "analytical_server_version", func(f *analyticalInstanceFacts) { f.Comment = "Compatible database" }},
		{"missing version", "analytical_server_version", func(f *analyticalInstanceFacts) { f.Version = "" }},
		{"client encoding", "analytical_encoding", func(f *analyticalInstanceFacts) { f.ClientCharset = "latin1" }},
		{"connection encoding", "analytical_encoding", func(f *analyticalInstanceFacts) { f.ConnectionCharset = "utf8mb3" }},
		{"result encoding", "analytical_encoding", func(f *analyticalInstanceFacts) { f.ResultsCharset = "" }},
		{"dirty reads", "analytical_isolation", func(f *analyticalInstanceFacts) { f.Isolation = "READ-UNCOMMITTED" }},
		{"unknown isolation", "analytical_isolation", func(f *analyticalInstanceFacts) { f.Isolation = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			tc.change(&f)
			r := checkAnalyticalInstanceFacts(f)
			if tc.code == "" {
				if !r.Supported || len(r.Diagnostics) != 0 {
					t.Fatalf("report=%#v", r)
				}
			} else if r.Supported || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != tc.code {
				t.Fatalf("report=%#v want=%s", r, tc.code)
			}
		})
	}
}
