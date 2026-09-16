package postgresql

import "testing"

func TestAnalyticalInstanceFacts(t *testing.T) {
	base := analyticalInstanceFacts{VersionNumber: 150015, Banner: "PostgreSQL 15.15 on test", ServerEncoding: "UTF8", ClientEncoding: "UTF8"}
	for _, tc := range []struct {
		name, code string
		change     func(*analyticalInstanceFacts)
	}{
		{"certified major", "", func(*analyticalInstanceFacts) {}},
		{"older major", "analytical_server_version", func(f *analyticalInstanceFacts) { f.VersionNumber = 140019; f.Banner = "PostgreSQL 14.19" }},
		{"newer major", "analytical_server_version", func(f *analyticalInstanceFacts) { f.VersionNumber = 160011; f.Banner = "PostgreSQL 16.11" }},
		{"inconsistent product", "analytical_server_version", func(f *analyticalInstanceFacts) { f.Banner = "Compatible PostgreSQL 15.15" }},
		{"unknown version", "analytical_server_version", func(f *analyticalInstanceFacts) { f.VersionNumber = 0 }},
		{"SQL ASCII database", "analytical_encoding", func(f *analyticalInstanceFacts) { f.ServerEncoding = "SQL_ASCII" }},
		{"LATIN1 client", "analytical_encoding", func(f *analyticalInstanceFacts) { f.ClientEncoding = "LATIN1" }},
		{"missing encoding", "analytical_encoding", func(f *analyticalInstanceFacts) { f.ClientEncoding = "" }},
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
