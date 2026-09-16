package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/addp/common/query/plan"
)

// AnalyticalPlanPackage freezes semantics and bindings, not a native query.
// Owner revisions, tenant checks and authorization remain owner responsibilities.
// PackageHash is an integrity fingerprint, not a signature or authorization grant.
type AnalyticalPlanPackage struct {
	SchemaVersion string           `json:"schema_version"`
	Plan          plan.Plan        `json:"plan"`
	Sources       []SourceBinding  `json:"sources"`
	EngineID      uint             `json:"engine_id"`
	Compiler      CompilerIdentity `json:"compiler"`
	PackageHash   string           `json:"package_hash"`
}

func NewAnalyticalPlanPackage(r CompileRequest, c AnalyticalCompiler) (AnalyticalPlanPackage, error) {
	if err := checkAnalyticalRequest(r, c); err != nil {
		return AnalyticalPlanPackage{}, err
	}
	p := AnalyticalPlanPackage{SchemaVersion: AnalyticalPlanSchemaVersion, Plan: r.Plan, Sources: r.Sources, EngineID: r.Instance.EngineID, Compiler: c.Identity()}
	normalized, err := p.canonical()
	if err != nil {
		return AnalyticalPlanPackage{}, err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return AnalyticalPlanPackage{}, err
	}
	normalized.PackageHash = analyticalHash(data)
	return normalized, nil
}

// Verify rejects version/hash drift before asking the compiler to check current
// instance support. A new compiler version requires explicit owner republication.
func (p AnalyticalPlanPackage) Verify(instance AnalyticalInstance, c AnalyticalCompiler) error {
	if c == nil || p.Compiler != c.Identity() || p.EngineID != instance.EngineID {
		return ErrAnalyticalPlanChanged
	}
	normalized, err := p.canonical()
	if err != nil {
		return err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return ErrAnalyticalInvalid
	}
	if p.PackageHash != analyticalHash(data) {
		return ErrAnalyticalPlanChanged
	}
	return checkAnalyticalRequest(CompileRequest{Plan: p.Plan, Sources: p.Sources, Instance: instance}, c)
}

func (p AnalyticalPlanPackage) canonical() (AnalyticalPlanPackage, error) {
	if p.SchemaVersion != AnalyticalPlanSchemaVersion || !p.Compiler.valid() {
		return AnalyticalPlanPackage{}, ErrAnalyticalInvalid
	}
	raw, err := plan.CanonicalJSON(p.Plan)
	if err != nil {
		return AnalyticalPlanPackage{}, ErrAnalyticalInvalid
	}
	if err = validateAnalyticalBindings(p.Plan, p.EngineID, p.Sources); err != nil {
		return AnalyticalPlanPackage{}, err
	}
	p.PackageHash = ""
	p.Plan = plan.Plan{}
	if err = json.Unmarshal(raw, &p.Plan); err != nil {
		return AnalyticalPlanPackage{}, ErrAnalyticalInvalid
	}
	// Deep-copy paths and fields: freezing a package must never mutate the owner.
	bindings, err := json.Marshal(p.Sources)
	if err != nil || len(bindings) > plan.MaxBytes {
		return AnalyticalPlanPackage{}, ErrAnalyticalInvalid
	}
	p.Sources = nil
	if err = json.Unmarshal(bindings, &p.Sources); err != nil {
		return AnalyticalPlanPackage{}, ErrAnalyticalInvalid
	}
	if p.Sources == nil {
		p.Sources = []SourceBinding{}
	}
	sort.Slice(p.Sources, func(i, j int) bool { return p.Sources[i].Source < p.Sources[j].Source })
	for i := range p.Sources {
		sort.Slice(p.Sources[i].Columns, func(a, b int) bool { return p.Sources[i].Columns[a].Column < p.Sources[i].Columns[b].Column })
	}
	return p, nil
}
func analyticalHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// CompiledQuery is an immutable derived value. It does not execute or prove the
// provider's ReadSet/OutputLineage. The execution bridge still must PrepareQuery
// and pass the existing authorization/data-protection gates.
type CompiledQuery struct {
	language    string
	template    string
	output      plan.OutputContract
	compiler    CompilerIdentity
	fingerprint string
	engineID    uint
	parameters  []plan.Parameter
	layout      AnalyticalResultLayout
}

func NewCompiledQuery(r CompileRequest, identity CompilerIdentity, language, template string, evaluations []EvaluationCheck) (CompiledQuery, error) {
	if err := r.Validate(); err != nil {
		return CompiledQuery{}, err
	}
	if !identity.valid() || !plan.Symbol(language) || len(template) == 0 || len(template) > plan.MaxBytes {
		return CompiledQuery{}, ErrAnalyticalInvalid
	}
	p := AnalyticalPlanPackage{SchemaVersion: AnalyticalPlanSchemaVersion, Plan: r.Plan, Sources: r.Sources, EngineID: r.Instance.EngineID, Compiler: identity}
	normalized, err := p.canonical()
	if err != nil {
		return CompiledQuery{}, err
	}
	layout, err := NewAnalyticalResultLayout(normalized.Plan, evaluations)
	if err != nil {
		return CompiledQuery{}, err
	}
	data, err := json.Marshal(struct {
		Package            AnalyticalPlanPackage
		Language, Template string
		Evaluations        []EvaluationCheck
	}{normalized, language, template, layout.Evaluations})
	if err != nil {
		return CompiledQuery{}, err
	}
	return CompiledQuery{language: language, template: template, output: normalized.Plan.Output, compiler: identity, fingerprint: analyticalHash(data), engineID: r.Instance.EngineID, parameters: normalized.Plan.Parameters, layout: layout}, nil
}
func (q CompiledQuery) Language() string           { return q.language }
func (q CompiledQuery) Template() string           { return q.template }
func (q CompiledQuery) Compiler() CompilerIdentity { return q.compiler }
func (q CompiledQuery) Fingerprint() string        { return q.fingerprint }
func (q CompiledQuery) Output() plan.OutputContract {
	// Logical fields contain no reference-valued native annotations.
	out := q.output
	out.Fields = append(out.Fields[:0:0], out.Fields...)
	out.StableKey = append([]string(nil), out.StableKey...)
	return out
}
