// Package plan defines database-independent, serializable analytical semantics.
// It has no dependency on an engine, physical catalog, driver or business owner.
package plan

import "github.com/addp/common/datatype"

const (
	SchemaVersion   = "addp.query_plan/v1"
	SemanticProfile = "relational_analytics_v1"
	MaxNodes        = 512
	MaxExpressions  = 16384
	MaxParameters   = 128
	MaxDepth        = 128
	MaxBytes        = 1 << 20
	// MaxLimit is a compilation resource bound; service policies may be stricter.
	MaxLimit = 100001
)

type NodeID string
type SourceID string

// Names in schemas are plan-local column identities, never physical names.
type Plan struct {
	SchemaVersion   string         `json:"schema_version"`
	SemanticProfile string         `json:"semantic_profile"`
	Nodes           []Node         `json:"nodes"`
	Root            NodeID         `json:"root"`
	Parameters      []Parameter    `json:"parameters"`
	Output          OutputContract `json:"output"`
	Assertions      []Assertion    `json:"assertions"`
}

// StableKey declares owner-proven uniqueness. Validation checks membership and
// non-nullability; the owner must build assertions if source uniqueness is not guaranteed.
type OutputContract struct {
	Fields    []datatype.FieldInfo `json:"fields"`
	StableKey []string             `json:"stable_key"`
}

type Parameter struct {
	Name     string             `json:"name"`
	Type     datatype.FieldType `json:"type"`
	Required bool               `json:"required"`
	Allowed  []Literal          `json:"allowed,omitempty"`
}

// Literal uses exact text encodings for integers/decimals and ISO dates.
// Null values must have empty Text. No float64 round-trip is permitted.
type Literal struct {
	Type datatype.FieldType `json:"type"`
	Text string             `json:"text"`
	Null bool               `json:"null,omitempty"`
}

type ColumnRef struct {
	Input NodeID `json:"input"`
	Name  string `json:"name"`
}

type Expr struct {
	Op        string     `json:"op"`
	Column    *ColumnRef `json:"column,omitempty"`
	Parameter string     `json:"parameter,omitempty"`
	Literal   *Literal   `json:"literal,omitempty"`
	Args      []Expr     `json:"args,omitempty"`
}

type Projection struct {
	Name string `json:"name"`
	Expr Expr   `json:"expr"`
}

type Assertion struct {
	Violation NodeID `json:"violation"`
	Code      string `json:"code"`
}

// Node is a closed tagged union: exactly the payload named by Op is allowed.
type Node struct {
	ID           NodeID        `json:"id"`
	Op           string        `json:"op"`
	Scan         *Scan         `json:"scan,omitempty"`
	Filter       *Filter       `json:"filter,omitempty"`
	Project      *Project      `json:"project,omitempty"`
	Join         *Join         `json:"join,omitempty"`
	Distinct     *Unary        `json:"distinct,omitempty"`
	Aggregate    *Aggregate    `json:"aggregate,omitempty"`
	UnionAll     *UnionAll     `json:"union_all,omitempty"`
	ConstantRows *ConstantRows `json:"constant_rows,omitempty"`
	DateBuckets  *DateBuckets  `json:"date_buckets,omitempty"`
	Sort         *Sort         `json:"sort,omitempty"`
	Limit        *Limit        `json:"limit,omitempty"`
}
type Scan struct {
	Source SourceID             `json:"source"`
	Fields []datatype.FieldInfo `json:"fields"`
}
type Unary struct {
	Input NodeID `json:"input"`
}
type Filter struct {
	Input     NodeID `json:"input"`
	Predicate Expr   `json:"predicate"`
}
type Project struct {
	Input   NodeID       `json:"input"`
	Columns []Projection `json:"columns"`
}
type Join struct {
	Left  NodeID `json:"left"`
	Right NodeID `json:"right"`
	Kind  string `json:"kind"`
	On    *Expr  `json:"on,omitempty"`
}
type Aggregate struct {
	Input    NodeID       `json:"input"`
	Groups   []Projection `json:"groups"`
	Measures []Measure    `json:"measures"`
}
type Measure struct {
	Name  string `json:"name"`
	Op    string `json:"op"`
	Value *Expr  `json:"value,omitempty"`
}
type UnionAll struct {
	Inputs []NodeID `json:"inputs"`
}
type ConstantRows struct {
	Fields []datatype.FieldInfo `json:"fields"`
	Rows   [][]Literal          `json:"rows"`
}
type DateBuckets struct {
	Start     Expr   `json:"start"`
	End       Expr   `json:"end"`
	Name      string `json:"name"`
	MaxMonths int    `json:"max_months"`
}
type Sort struct {
	Input NodeID    `json:"input"`
	Keys  []SortKey `json:"keys"`
}
type SortKey struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Nulls     string `json:"nulls"`
}
type Limit struct {
	Input NodeID `json:"input"`
	Count int    `json:"count"`
}

func (n Node) Inputs() []NodeID {
	switch {
	case n.Filter != nil:
		return []NodeID{n.Filter.Input}
	case n.Project != nil:
		return []NodeID{n.Project.Input}
	case n.Join != nil:
		return []NodeID{n.Join.Left, n.Join.Right}
	case n.Distinct != nil:
		return []NodeID{n.Distinct.Input}
	case n.Aggregate != nil:
		return []NodeID{n.Aggregate.Input}
	case n.UnionAll != nil:
		return append([]NodeID(nil), n.UnionAll.Inputs...)
	case n.Sort != nil:
		return []NodeID{n.Sort.Input}
	case n.Limit != nil:
		return []NodeID{n.Limit.Input}
	default:
		return nil
	}
}
