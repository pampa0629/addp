package dataprofile

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/spatial"
)

type valueFrequency struct {
	value interface{}
	count int64
}

// Build computes descriptive statistics from an independently collected sample.
func Build(rows []map[string]interface{}, fields []datatype.FieldInfo, opts BuildOptions) Profile {
	if opts.Mode == "" {
		opts.Mode = ModeSample
	}
	if opts.SampleMethod == "" {
		opts.SampleMethod = "bounded_reservoir"
	}
	if opts.TopN <= 0 {
		opts.TopN = 10
	}
	if opts.HistogramBins <= 0 {
		opts.HistogramBins = 10
	}
	if opts.ProfiledAt.IsZero() {
		opts.ProfiledAt = time.Now().UTC()
	}
	fields = normalizedFields(rows, fields)
	profile := Profile{
		SchemaVersion: SchemaVersionV1,
		Mode:          opts.Mode,
		SampleMethod:  opts.SampleMethod,
		SampleSize:    int64(len(rows)),
		RowsScanned:   opts.RowsScanned,
		RowCount:      cloneInt64(opts.RowCount),
		RowCountExact: opts.RowCountExact,
		FieldCount:    len(fields),
		Truncated:     opts.Truncated,
		Partial:       opts.Partial,
		ProfiledAt:    opts.ProfiledAt.UTC(),
		Fields:        make([]FieldProfile, 0, len(fields)),
	}
	for _, field := range fields {
		fp := buildField(rows, field, opts)
		profile.Fields = append(profile.Fields, fp)
		profile.Observations = append(profile.Observations, fp.Observations...)
	}
	return profile
}

func buildField(rows []map[string]interface{}, field datatype.FieldInfo, opts BuildOptions) FieldProfile {
	fp := FieldProfile{
		Name: field.Name, Type: field.Type, NativeType: field.NativeType,
		Nullable: field.Nullable, PrimaryKey: field.PrimaryKey, Status: MetricStatusComputed,
		DistinctApproximate: opts.Truncated || (opts.RowCount != nil && int64(len(rows)) < *opts.RowCount),
	}
	values := make([]interface{}, 0, len(rows))
	frequencies := make(map[string]*valueFrequency)
	for _, row := range rows {
		value, ok := row[field.Name]
		if !ok || value == nil {
			fp.NullCount++
			continue
		}
		fp.ValueCount++
		values = append(values, value)
		key := canonicalValue(value)
		if frequencies[key] == nil {
			frequencies[key] = &valueFrequency{value: value}
		}
		frequencies[key].count++
	}
	if len(rows) > 0 {
		fp.NullRate = float64(fp.NullCount) / float64(len(rows))
	}
	fp.DistinctCount = int64(len(frequencies))
	if fp.ValueCount > 0 {
		fp.UniqueRate = float64(fp.DistinctCount) / float64(fp.ValueCount)
	}
	if !datatype.IsSpatialFieldType(field.Type) {
		fp.TopValues = topValues(frequencies, fp.ValueCount, opts.TopN)
	}

	switch {
	case datatype.IsNumericFieldType(field.Type):
		fp.Numeric, fp.Distribution = numericProfile(values, opts.HistogramBins)
	case datatype.IsTemporalFieldType(field.Type):
		fp.Temporal, fp.Distribution = temporalProfile(values, opts.HistogramBins)
	case field.Type == datatype.FieldTypeBool:
		fp.Boolean, fp.Distribution = booleanProfile(values)
	case field.Type == datatype.FieldTypeString || field.Type == datatype.FieldTypeUUID:
		fp.Text, fp.Distribution = textProfile(values, opts.HistogramBins)
	case datatype.IsSpatialFieldType(field.Type):
		fp.Spatial, fp.Distribution = spatialProfile(values)
	default:
		fp.Status = MetricStatusUnsupported
	}
	fp.Observations = observationsForField(fp)
	return fp
}

func spatialProfile(values []interface{}) (*SpatialMetrics, []DistributionBucket) {
	metrics := &SpatialMetrics{}
	typeCounts := make(map[string]int64)
	for _, value := range values {
		geometry, err := spatial.ParseGeometryValue(value)
		if err != nil || geometry == nil {
			metrics.InvalidGeometryCount++
			continue
		}
		if geometry.Empty() {
			metrics.EmptyGeometryCount++
			continue
		}
		metrics.ValidGeometryCount++
		typeCounts[spatial.GeometryTypeName(geometry)]++
	}

	labels := make([]string, 0, len(typeCounts))
	for label := range typeCounts {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	distribution := make([]DistributionBucket, 0, len(labels))
	for _, label := range labels {
		distribution = append(distribution, DistributionBucket{Label: label, Count: typeCounts[label]})
	}
	return metrics, distribution
}

func normalizedFields(rows []map[string]interface{}, fields []datatype.FieldInfo) []datatype.FieldInfo {
	if len(fields) > 0 {
		out := append([]datatype.FieldInfo(nil), fields...)
		for i := range out {
			if out[i].Type == "" {
				out[i].Type = datatype.FieldTypeUnknown
			}
		}
		return out
	}
	keys := make(map[string]struct{})
	for _, row := range rows {
		for key := range row {
			keys[key] = struct{}{}
		}
	}
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]datatype.FieldInfo, 0, len(names))
	for _, name := range names {
		out = append(out, datatype.FieldInfo{Name: name, Type: inferFieldType(rows, name), Nullable: true})
	}
	return out
}

func inferFieldType(rows []map[string]interface{}, field string) datatype.FieldType {
	for _, row := range rows {
		value := row[field]
		switch value.(type) {
		case bool:
			return datatype.FieldTypeBool
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return datatype.FieldTypeBigInt
		case float32, float64, json.Number:
			return datatype.FieldTypeDouble
		case time.Time:
			return datatype.FieldTypeTimestamp
		case string:
			return datatype.FieldTypeString
		}
	}
	return datatype.FieldTypeUnknown
}

func numericProfile(values []interface{}, bins int) (*NumericMetrics, []DistributionBucket) {
	numbers := make([]float64, 0, len(values))
	for _, value := range values {
		if number, ok := floatValue(value); ok && !math.IsNaN(number) && !math.IsInf(number, 0) {
			numbers = append(numbers, number)
		}
	}
	if len(numbers) == 0 {
		return nil, nil
	}
	sort.Float64s(numbers)
	metrics := &NumericMetrics{Min: numbers[0], Max: numbers[len(numbers)-1]}
	var sum float64
	for _, number := range numbers {
		sum += number
		if number == 0 {
			metrics.ZeroCount++
		}
		if number < 0 {
			metrics.NegativeCount++
		}
	}
	metrics.Mean = sum / float64(len(numbers))
	metrics.Median = quantile(numbers, 0.5)
	metrics.P25 = quantile(numbers, 0.25)
	metrics.P75 = quantile(numbers, 0.75)
	metrics.P95 = quantile(numbers, 0.95)
	var variance float64
	for _, number := range numbers {
		delta := number - metrics.Mean
		variance += delta * delta
	}
	metrics.Stddev = math.Sqrt(variance / float64(len(numbers)))
	return metrics, numericHistogram(numbers, bins)
}

func textProfile(values []interface{}, bins int) (*TextMetrics, []DistributionBucket) {
	lengths := make([]float64, 0, len(values))
	metrics := &TextMetrics{MinLength: -1}
	var sum int
	for _, value := range values {
		text := fmt.Sprint(value)
		length := len([]rune(text))
		lengths = append(lengths, float64(length))
		sum += length
		if length == 0 {
			metrics.EmptyCount++
		}
		if strings.TrimSpace(text) == "" {
			metrics.BlankCount++
		}
		if metrics.MinLength < 0 || length < metrics.MinLength {
			metrics.MinLength = length
		}
		if length > metrics.MaxLength {
			metrics.MaxLength = length
		}
	}
	if len(values) == 0 {
		metrics.MinLength = 0
		return nil, nil
	}
	metrics.AvgLength = float64(sum) / float64(len(values))
	sort.Float64s(lengths)
	return metrics, numericHistogram(lengths, bins)
}

func temporalProfile(values []interface{}, bins int) (*TemporalMetrics, []DistributionBucket) {
	times := make([]time.Time, 0, len(values))
	for _, value := range values {
		if parsed, ok := timeValue(value); ok {
			times = append(times, parsed.UTC())
		}
	}
	if len(times) == 0 {
		return nil, nil
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	metrics := &TemporalMetrics{Min: times[0].Format(time.RFC3339Nano), Max: times[len(times)-1].Format(time.RFC3339Nano)}
	numbers := make([]float64, len(times))
	for i, value := range times {
		numbers[i] = float64(value.UnixNano())
	}
	histogram := numericHistogram(numbers, bins)
	for i := range histogram {
		if min, ok := histogram[i].Min.(float64); ok {
			histogram[i].Min = time.Unix(0, int64(min)).UTC().Format(time.RFC3339Nano)
		}
		if max, ok := histogram[i].Max.(float64); ok {
			histogram[i].Max = time.Unix(0, int64(max)).UTC().Format(time.RFC3339Nano)
		}
		histogram[i].Label = fmt.Sprintf("%v - %v", histogram[i].Min, histogram[i].Max)
	}
	return metrics, histogram
}

func booleanProfile(values []interface{}) (*BooleanMetrics, []DistributionBucket) {
	metrics := &BooleanMetrics{}
	for _, value := range values {
		if typed, ok := value.(bool); ok {
			if typed {
				metrics.TrueCount++
			} else {
				metrics.FalseCount++
			}
		}
	}
	return metrics, []DistributionBucket{
		{Label: "false", Min: false, Max: false, Count: metrics.FalseCount},
		{Label: "true", Min: true, Max: true, Count: metrics.TrueCount},
	}
}

func numericHistogram(numbers []float64, bins int) []DistributionBucket {
	if len(numbers) == 0 {
		return nil
	}
	if bins <= 0 {
		bins = 10
	}
	if bins > len(numbers) {
		bins = len(numbers)
	}
	minValue, maxValue := numbers[0], numbers[len(numbers)-1]
	if minValue == maxValue {
		return []DistributionBucket{{Label: formatRange(minValue, maxValue), Min: minValue, Max: maxValue, Count: int64(len(numbers))}}
	}
	width := (maxValue - minValue) / float64(bins)
	result := make([]DistributionBucket, bins)
	for i := range result {
		lower := minValue + float64(i)*width
		upper := lower + width
		if i == bins-1 {
			upper = maxValue
		}
		result[i] = DistributionBucket{Label: formatRange(lower, upper), Min: lower, Max: upper}
	}
	for _, number := range numbers {
		index := int((number - minValue) / width)
		if index >= bins {
			index = bins - 1
		}
		result[index].Count++
	}
	return result
}

func topValues(frequencies map[string]*valueFrequency, total int64, limit int) []ValueCount {
	values := make([]*valueFrequency, 0, len(frequencies))
	for _, frequency := range frequencies {
		values = append(values, frequency)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].count == values[j].count {
			return canonicalValue(values[i].value) < canonicalValue(values[j].value)
		}
		return values[i].count > values[j].count
	})
	if len(values) > limit {
		values = values[:limit]
	}
	result := make([]ValueCount, 0, len(values))
	for _, value := range values {
		rate := float64(0)
		if total > 0 {
			rate = float64(value.count) / float64(total)
		}
		result = append(result, ValueCount{Value: value.value, Count: value.count, Rate: rate})
	}
	return result
}

func observationsForField(field FieldProfile) []Observation {
	result := make([]Observation, 0, 4)
	appendObservation := func(code, severity string, value float64) {
		result = append(result, Observation{Code: code, Severity: severity, Field: field.Name, Value: value})
	}
	if field.NullRate >= 0.5 {
		appendObservation(ObservationHighMissing, "warning", field.NullRate)
	}
	if field.ValueCount > 0 && field.DistinctCount == 1 {
		appendObservation(ObservationConstant, "info", 1)
	}
	if field.ValueCount >= 20 && field.UniqueRate >= 0.9 {
		appendObservation(ObservationHighCardinality, "info", field.UniqueRate)
		if field.Type == datatype.FieldTypeString || datatype.IsNumericFieldType(field.Type) || field.Type == datatype.FieldTypeUUID {
			appendObservation(ObservationPossibleIdentifier, "info", field.UniqueRate)
		}
	}
	if field.ValueCount >= 20 && distributionPeakRate(field.Distribution, field.ValueCount) >= 0.7 {
		appendObservation(ObservationSkewed, "info", distributionPeakRate(field.Distribution, field.ValueCount))
	}
	return result
}

func distributionPeakRate(buckets []DistributionBucket, total int64) float64 {
	if total <= 0 {
		return 0
	}
	var maxCount int64
	for _, bucket := range buckets {
		if bucket.Count > maxCount {
			maxCount = bucket.Count
		}
	}
	return float64(maxCount) / float64(total)
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	position := q * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return sorted[lower]
	}
	weight := position - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

func floatValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func timeValue(value interface{}) (time.Time, bool) {
	if typed, ok := value.(time.Time); ok {
		return typed, true
	}
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05", "2006-01-02", "15:04:05"} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(text)); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func canonicalValue(value interface{}) string {
	encoded, err := json.Marshal(value)
	if err == nil {
		return string(encoded)
	}
	return fmt.Sprintf("%T:%v", value, value)
}

func formatRange(minValue, maxValue float64) string {
	return fmt.Sprintf("%s - %s", strconv.FormatFloat(minValue, 'g', 6, 64), strconv.FormatFloat(maxValue, 'g', 6, 64))
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
