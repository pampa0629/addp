package plugin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/query/plan"
)

const AnalyticalPlanSchemaVersion = "addp.analytical_plan/v1"

var (
	ErrAnalyticalInvalid     = errors.New("invalid analytical plan")
	ErrAnalyticalUnsupported = errors.New("analytical plan is unsupported")
	ErrAnalyticalPlanChanged = errors.New("analytical plan dependency changed")
)

// AnalyticalCompilerProvider adds compilation, never a second execution route.
// Production capability declarations require separate engine certification.
type AnalyticalCompilerProvider interface {
	QueryRuntimeProvider
	AnalyticalCompiler() AnalyticalCompiler
}

// AnalyticalSQLExecutionValidator is a native SQL compiler integration hook,
// not an owner API or a second executor. The caller owns the read-only
// transaction; implementations retain source schema locks until it finishes.
type AnalyticalSQLExecutionValidator interface {
	ValidateAnalyticalExecution(context.Context, *sql.Tx, []SourceBinding) error
}

// AnalyticalCompiler is deterministic and connection-free. Check must validate
// the complete native catalog/type conditions. Compile must repeat validation;
// it must not assume Check was called. Neither method accepts request values.
type AnalyticalCompiler interface {
	Identity() CompilerIdentity
	Check(CompileRequest) (SupportReport, error)
	Compile(CompileRequest) (CompiledQuery, error)
}

type CompilerIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// AnalyticalCapability is the credential-free projection of certified instance
// capabilities. Defining this contract does not enable any production engine.
type AnalyticalCapability struct {
	Supported        bool     `json:"supported"`
	PlanVersions     []string `json:"plan_versions"`
	SemanticProfiles []string `json:"semantic_profiles"`
}
type AnalyticalInstance struct {
	EngineID   uint                 `json:"engine_id"`
	Capability AnalyticalCapability `json:"capability"`
}
type CompileRequest struct {
	Plan     plan.Plan
	Sources  []SourceBinding
	Instance AnalyticalInstance
}

type SourceBinding struct {
	Source  plan.SourceID     `json:"source"`
	Path    EngineCatalogPath `json:"path"`
	Columns []ColumnBinding   `json:"columns"`
}

// Field contains physical Name/Path, Type/NativeType, Nullable and size/precision.
// Path is complete within the source and ends in Name. Presentation, defaults,
// generated expressions and owner IDs are forbidden. NativeType is a fact to
// check, never an expression to splice into a query.
type ColumnBinding struct {
	Column string             `json:"column"`
	Field  datatype.FieldInfo `json:"field"`
}
type SupportDiagnostic struct {
	Code   string      `json:"code"`
	NodeID plan.NodeID `json:"node_id,omitempty"`
}
type SupportReport struct {
	Supported   bool                `json:"supported"`
	Diagnostics []SupportDiagnostic `json:"diagnostics,omitempty"`
}

func (r CompileRequest) Validate() error {
	if err := plan.Validate(r.Plan); err != nil {
		return fmt.Errorf("%w: %v", ErrAnalyticalInvalid, err)
	}
	if err := validateAnalyticalBindings(r.Plan, r.Instance.EngineID, r.Sources); err != nil {
		return err
	}
	c := r.Instance.Capability
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrAnalyticalInvalid, err)
	}
	if !c.Supported || !slices.Contains(c.PlanVersions, r.Plan.SchemaVersion) || !slices.Contains(c.SemanticProfiles, r.Plan.SemanticProfile) {
		return ErrAnalyticalUnsupported
	}
	return nil
}

var compilerIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

func (c CompilerIdentity) valid() bool {
	return compilerIdentityPattern.MatchString(c.ID) && compilerIdentityPattern.MatchString(c.Version)
}
func analyticalName(s string) bool {
	return len(s) > 0 && len(s) <= 1024 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// This validates engine-neutral binding structure. The compiler must additionally
// check each complete path against its own EngineCatalogModelSpec and native
// column semantics; the shared contract imposes no fixed namespace depth.
func validateAnalyticalBindings(p plan.Plan, engineID uint, sources []SourceBinding) error {
	invalid := func() error { return ErrAnalyticalInvalid }
	if engineID == 0 || len(sources) > plan.MaxNodes {
		return invalid()
	}
	scans := map[plan.SourceID][]datatype.FieldInfo{}
	for _, n := range p.Nodes {
		if n.Scan != nil {
			scans[n.Scan.Source] = n.Scan.Fields
		}
	}
	if len(scans) != len(sources) {
		return invalid()
	}
	seen := map[plan.SourceID]bool{}
	size := 0
	for _, s := range sources {
		fields, exists := scans[s.Source]
		if !exists || seen[s.Source] {
			return invalid()
		}
		seen[s.Source] = true
		path := s.Path
		if path.EngineID != engineID || path.Version != EngineCatalogPathVersion || len(path.Segments) < 2 || len(path.Segments) > plan.MaxDepth {
			return invalid()
		}
		root := path.Segments[0]
		if !IsEngineCatalogRootSegment(root) || root.Kind != root.Term || root.Name != "" {
			return invalid()
		}
		for _, part := range path.Segments[1:] {
			if !plan.Symbol(part.Term) || !plan.Symbol(part.Kind) || !analyticalName(part.Name) || IsEngineCatalogRootSegment(part) {
				return invalid()
			}
			size += len(part.Term) + len(part.Kind) + len(part.Name)
		}
		if len(s.Columns) != len(fields) {
			return invalid()
		}
		columns := map[string]datatype.FieldInfo{}
		for _, f := range fields {
			columns[f.Name] = f
		}
		for _, b := range s.Columns {
			logical, ok := columns[b.Column]
			if !ok {
				return invalid()
			}
			delete(columns, b.Column)
			f := b.Field
			if len(f.Path) == 0 || len(f.Path) > plan.MaxDepth || f.Name != f.Path[len(f.Path)-1] || f.Type != logical.Type || (f.Nullable && !logical.Nullable) {
				return invalid()
			}
			for _, part := range f.Path {
				if !analyticalName(part) {
					return invalid()
				}
				size += len(part)
			}
			if !analyticalName(f.NativeType) || f.Size < 0 || f.Precision < 0 || f.Scale < 0 || f.Scale > f.Precision {
				return invalid()
			}
			expected := datatype.FieldInfo{Name: f.Name, Path: f.Path, Type: f.Type, NativeType: f.NativeType, Nullable: f.Nullable, Size: f.Size, Precision: f.Precision, Scale: f.Scale}
			if !reflect.DeepEqual(f, expected) {
				return invalid()
			}
			size += len(f.Name) + len(f.NativeType) + len(b.Column)
		}
		if size > plan.MaxBytes {
			return invalid()
		}
	}
	return nil
}

func checkAnalyticalRequest(r CompileRequest, c AnalyticalCompiler) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if c == nil || !c.Identity().valid() {
		return ErrAnalyticalUnsupported
	}
	report, err := c.Check(r)
	if err != nil {
		return err
	}
	if report.Supported && len(report.Diagnostics) > 0 {
		return ErrAnalyticalInvalid
	}
	nodes := map[plan.NodeID]bool{}
	for _, n := range r.Plan.Nodes {
		nodes[n.ID] = true
	}
	for _, d := range report.Diagnostics {
		if !plan.Symbol(d.Code) || (d.NodeID != "" && !nodes[d.NodeID]) {
			return ErrAnalyticalInvalid
		}
	}
	if !report.Supported {
		return ErrAnalyticalUnsupported
	}
	return nil
}
