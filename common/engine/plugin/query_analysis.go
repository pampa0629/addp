package plugin

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	QuerySchemaCoverageComplete = "complete"
	QuerySchemaCoverageSampled  = "sampled"
	QuerySchemaCoverageUnknown  = "unknown"

	QueryDiagnosticSeverityError   = "error"
	QueryDiagnosticSeverityWarning = "warning"
	QueryDiagnosticSeverityInfo    = "info"

	QueryDiagnosticPhaseSyntax    = "syntax"
	QueryDiagnosticPhaseParameter = "parameter"
	QueryDiagnosticPhaseScope     = "scope"
	QueryDiagnosticPhaseSchema    = "schema"
	QueryDiagnosticPhasePolicy    = "policy"
)

var queryDiagnosticCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// QueryAnalysis is the provider-owned diagnostic fact for one PreparedQuery.
// Schema diagnostics may assert absence only when SchemaCoverage is complete.
type QueryAnalysis struct {
	Language       string            `json:"language"`
	SchemaCoverage string            `json:"schema_coverage"`
	Diagnostics    []QueryDiagnostic `json:"diagnostics"`
}

// QueryDiagnostic carries stable machine-readable facts. User-visible text is
// localized by the consuming owner from Code and Parameters.
type QueryDiagnostic struct {
	Code       string            `json:"code"`
	Severity   string            `json:"severity"`
	Phase      string            `json:"phase"`
	Parameters map[string]string `json:"parameters,omitempty"`
	Start      *int              `json:"start,omitempty"`
	End        *int              `json:"end,omitempty"`
}

// NewQueryAnalysis validates and clones a provider analysis before it crosses
// the engine boundary.
func NewQueryAnalysis(language, schemaCoverage string, diagnostics ...QueryDiagnostic) (*QueryAnalysis, error) {
	analysis := &QueryAnalysis{
		Language:       strings.ToLower(strings.TrimSpace(language)),
		SchemaCoverage: strings.ToLower(strings.TrimSpace(schemaCoverage)),
		Diagnostics:    append(make([]QueryDiagnostic, 0, len(diagnostics)), diagnostics...),
	}
	if err := analysis.Validate(); err != nil {
		return nil, err
	}
	return analysis.Clone(), nil
}

func (a *QueryAnalysis) Validate() error {
	if a == nil || a.Language == "" {
		return fmt.Errorf("query analysis language is required")
	}
	switch a.SchemaCoverage {
	case QuerySchemaCoverageComplete, QuerySchemaCoverageSampled, QuerySchemaCoverageUnknown:
	default:
		return fmt.Errorf("unsupported query schema coverage %q", a.SchemaCoverage)
	}
	for index, diagnostic := range a.Diagnostics {
		if err := diagnostic.validate(a.SchemaCoverage); err != nil {
			return fmt.Errorf("query diagnostic %d: %w", index, err)
		}
	}
	return nil
}

func (d QueryDiagnostic) validate(schemaCoverage string) error {
	if !queryDiagnosticCodePattern.MatchString(d.Code) {
		return fmt.Errorf("invalid code %q", d.Code)
	}
	switch d.Severity {
	case QueryDiagnosticSeverityError, QueryDiagnosticSeverityWarning, QueryDiagnosticSeverityInfo:
	default:
		return fmt.Errorf("unsupported severity %q", d.Severity)
	}
	switch d.Phase {
	case QueryDiagnosticPhaseSyntax, QueryDiagnosticPhaseParameter, QueryDiagnosticPhaseScope,
		QueryDiagnosticPhaseSchema, QueryDiagnosticPhasePolicy:
	default:
		return fmt.Errorf("unsupported phase %q", d.Phase)
	}
	if d.Phase == QueryDiagnosticPhaseSchema && schemaCoverage != QuerySchemaCoverageComplete &&
		d.Code != "schema_coverage_incomplete" {
		return fmt.Errorf("schema absence diagnostics require complete schema coverage")
	}
	if (d.Start == nil) != (d.End == nil) {
		return fmt.Errorf("diagnostic range requires both start and end")
	}
	if d.Start != nil && (*d.Start < 0 || *d.End < *d.Start) {
		return fmt.Errorf("invalid diagnostic range")
	}
	return nil
}

func (a *QueryAnalysis) Clone() *QueryAnalysis {
	if a == nil {
		return nil
	}
	cloned := &QueryAnalysis{
		Language:       a.Language,
		SchemaCoverage: a.SchemaCoverage,
		Diagnostics:    make([]QueryDiagnostic, len(a.Diagnostics)),
	}
	for index, diagnostic := range a.Diagnostics {
		cloned.Diagnostics[index] = diagnostic
		if diagnostic.Parameters != nil {
			cloned.Diagnostics[index].Parameters = make(map[string]string, len(diagnostic.Parameters))
			for key, value := range diagnostic.Parameters {
				cloned.Diagnostics[index].Parameters[key] = value
			}
		}
		if diagnostic.Start != nil {
			start := *diagnostic.Start
			end := *diagnostic.End
			cloned.Diagnostics[index].Start = &start
			cloned.Diagnostics[index].End = &end
		}
	}
	return cloned
}
