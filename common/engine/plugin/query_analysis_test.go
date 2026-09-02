package plugin

import "testing"

func TestQueryAnalysisRejectsSchemaErrorWithoutCompleteCoverage(t *testing.T) {
	_, err := NewQueryAnalysis("sql", QuerySchemaCoverageSampled, QueryDiagnostic{
		Code: "field_not_found", Severity: QueryDiagnosticSeverityError, Phase: QueryDiagnosticPhaseSchema,
	})
	if err == nil {
		t.Fatal("sampled schema coverage accepted an absence error")
	}
}

func TestQueryAnalysisRejectsSchemaWarningWithoutCompleteCoverage(t *testing.T) {
	_, err := NewQueryAnalysis("mql", QuerySchemaCoverageSampled, QueryDiagnostic{
		Code: "field_not_found", Severity: QueryDiagnosticSeverityWarning, Phase: QueryDiagnosticPhaseSchema,
	})
	if err == nil {
		t.Fatal("sampled schema coverage accepted an absence warning")
	}
	if _, err := NewQueryAnalysis("mql", QuerySchemaCoverageSampled, QueryDiagnostic{
		Code: "schema_coverage_incomplete", Severity: QueryDiagnosticSeverityInfo, Phase: QueryDiagnosticPhaseSchema,
	}); err != nil {
		t.Fatalf("coverage information was rejected: %v", err)
	}
}

func TestQueryAnalysisClonesDiagnostics(t *testing.T) {
	start, end := 4, 9
	analysis, err := NewQueryAnalysis("MQL", QuerySchemaCoverageUnknown, QueryDiagnostic{
		Code: "parameter_missing", Severity: QueryDiagnosticSeverityError, Phase: QueryDiagnosticPhaseParameter,
		Parameters: map[string]string{"name": "status"}, Start: &start, End: &end,
	})
	if err != nil {
		t.Fatal(err)
	}
	clone := analysis.Clone()
	clone.Diagnostics[0].Parameters["name"] = "changed"
	*clone.Diagnostics[0].Start = 0
	if analysis.Language != "mql" || analysis.Diagnostics[0].Parameters["name"] != "status" || *analysis.Diagnostics[0].Start != 4 {
		t.Fatalf("analysis retained caller-owned diagnostic storage: %#v", analysis)
	}
}
