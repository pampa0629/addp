package dataprofile

import (
	"time"

	"github.com/addp/common/datatype"
)

const (
	SchemaVersionV1 = "data.profile/v1"
	ModeSample      = "sample"

	MetricStatusComputed    = "computed"
	MetricStatusUnsupported = "unsupported"

	ObservationHighMissing        = "high_missing"
	ObservationConstant           = "constant"
	ObservationHighCardinality    = "high_cardinality"
	ObservationPossibleIdentifier = "possible_identifier"
	ObservationSkewed             = "skewed"
)

// Profile is the stable, cross-module data profiling result contract.
type Profile struct {
	SchemaVersion string         `json:"schema_version"`
	Mode          string         `json:"mode"`
	SampleMethod  string         `json:"sample_method"`
	SampleSize    int64          `json:"sample_size"`
	RowsScanned   int64          `json:"rows_scanned"`
	RowCount      *int64         `json:"row_count,omitempty"`
	RowCountExact bool           `json:"row_count_exact"`
	FieldCount    int            `json:"field_count"`
	Truncated     bool           `json:"truncated"`
	Partial       bool           `json:"partial"`
	ProfiledAt    time.Time      `json:"profiled_at"`
	Fields        []FieldProfile `json:"fields"`
	Observations  []Observation  `json:"observations,omitempty"`
}

type FieldProfile struct {
	Name                string               `json:"name"`
	Type                datatype.FieldType   `json:"type"`
	NativeType          string               `json:"native_type,omitempty"`
	Nullable            bool                 `json:"nullable"`
	PrimaryKey          bool                 `json:"primary_key,omitempty"`
	Status              string               `json:"status"`
	ValueCount          int64                `json:"value_count"`
	NullCount           int64                `json:"null_count"`
	NullRate            float64              `json:"null_rate"`
	DistinctCount       int64                `json:"distinct_count"`
	DistinctApproximate bool                 `json:"distinct_approximate"`
	UniqueRate          float64              `json:"unique_rate"`
	Numeric             *NumericMetrics      `json:"numeric,omitempty"`
	Text                *TextMetrics         `json:"text,omitempty"`
	Temporal            *TemporalMetrics     `json:"temporal,omitempty"`
	Boolean             *BooleanMetrics      `json:"boolean,omitempty"`
	Distribution        []DistributionBucket `json:"distribution,omitempty"`
	TopValues           []ValueCount         `json:"top_values,omitempty"`
	Observations        []Observation        `json:"observations,omitempty"`
}

type NumericMetrics struct {
	Min           float64 `json:"min"`
	Max           float64 `json:"max"`
	Mean          float64 `json:"mean"`
	Median        float64 `json:"median"`
	P25           float64 `json:"p25"`
	P75           float64 `json:"p75"`
	P95           float64 `json:"p95"`
	Stddev        float64 `json:"stddev"`
	ZeroCount     int64   `json:"zero_count"`
	NegativeCount int64   `json:"negative_count"`
}

type TextMetrics struct {
	EmptyCount int64   `json:"empty_count"`
	BlankCount int64   `json:"blank_count"`
	MinLength  int     `json:"min_length"`
	MaxLength  int     `json:"max_length"`
	AvgLength  float64 `json:"avg_length"`
}

type TemporalMetrics struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

type BooleanMetrics struct {
	TrueCount  int64 `json:"true_count"`
	FalseCount int64 `json:"false_count"`
}

type DistributionBucket struct {
	Label string      `json:"label"`
	Min   interface{} `json:"min,omitempty"`
	Max   interface{} `json:"max,omitempty"`
	Count int64       `json:"count"`
}

type ValueCount struct {
	Value interface{} `json:"value"`
	Count int64       `json:"count"`
	Rate  float64     `json:"rate"`
}

type Observation struct {
	Code     string  `json:"code"`
	Severity string  `json:"severity"`
	Field    string  `json:"field,omitempty"`
	Value    float64 `json:"value,omitempty"`
}

type BuildOptions struct {
	Mode          string
	SampleMethod  string
	RowsScanned   int64
	RowCount      *int64
	RowCountExact bool
	Truncated     bool
	Partial       bool
	TopN          int
	HistogramBins int
	ProfiledAt    time.Time
}
