package service

import (
	"fmt"
	"math"

	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
)

var filesystemPressureKeys = []string{"node.filesystem.total_bytes", "node.filesystem.used_bytes", "node.filesystem.available_bytes"}

func sameResourceDimensions(a, b resourcequery.Dimensions) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
func resourceRuleGroup(r models.ResourceAlertRule) string { return r.NodeID + "/" + r.MountKey }
func pressureMetricDimensions(s resourcequery.Series) bool {
	if s.MetricKey == models.FilesystemCapacityMetric {
		return len(s.Dimensions) == 3 && s.Dimensions.Validate() == nil
	}
	return len(s.Dimensions) == 0
}

// All witnesses describe the exact mount and original scrape of the ratio.
func filesystemPressureEvidence(metric resourcequery.Series, companions []resourcequery.Series, index int) (*models.FilesystemCapacityEvidence, bool) {
	if len(companions) != len(filesystemPressureKeys) || !pressureMetricDimensions(metric) || index < 0 || index >= len(metric.Points) {
		return nil, false
	}
	point := metric.Points[index]
	if !validPressurePoint(point) {
		return nil, false
	}
	values := make([]float64, len(companions))
	for i, series := range companions {
		if series.MetricKey != filesystemPressureKeys[i] || series.Unit != "bytes" || series.WindowSeconds != 0 || !sameResourceDimensions(series.Dimensions, metric.Dimensions) || len(series.Points) != len(metric.Points) {
			return nil, false
		}
		witness := series.Points[index]
		if !validPressurePoint(witness) || !witness.EvaluatedAt.Equal(point.EvaluatedAt) || !witness.SampledAt.Equal(*point.SampledAt) || *witness.Value < 0 {
			return nil, false
		}
		values[i] = *witness.Value
	}
	total, used, available := values[0], values[1], values[2]
	if !filesystemCapacityValuesValid(total, used, available, *point.Value) {
		return nil, false
	}
	return &models.FilesystemCapacityEvidence{Dimensions: metric.Dimensions.Copy(), TotalBytes: total, UsedBytes: used, AvailableBytes: available}, true
}

func validFilesystemAlertEvidence(signal string, e *models.ResourceAlertEvidence) bool {
	if e == nil || e.Filesystem == nil || !resourceRuleUUID(e.RuleID) || !e.Policy.Valid() {
		return false
	}
	f := e.Filesystem
	if len(f.Dimensions) != 3 || f.Dimensions.Validate() != nil || signal != models.ResourceAlertSignal(models.FilesystemCapacityMetric, f.Dimensions) {
		return false
	}
	return filesystemCapacityValuesValid(f.TotalBytes, f.UsedBytes, f.AvailableBytes, e.Latest)
}

func filesystemCapacityIEC(value float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	index := 0
	for value >= 1024 && index < len(units)-1 {
		value /= 1024
		index++
	}
	return fmt.Sprintf("%.2f %s", value, units[index])
}

func filesystemCapacityValuesValid(total, used, available, percent float64) bool {
	for _, v := range []float64{total, used, available, percent} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return false
		}
	}
	// Subtracting the exported used value may round differently from the raw
	// free gauge. Permit only a few representational ULPs, not missing capacity.
	tolerance := 4 * (math.Nextafter(total, math.Inf(1)) - total)
	return total > 0 && used <= total && available <= total-used+tolerance && used+available > 0 && percent <= 100 && math.Abs(percent-100*used/(used+available)) <= 0.000001
}

func validResourceRuleDimensions(metric string, d resourcequery.Dimensions) bool {
	if !resourcePressureMetric(metric) || d.Validate() != nil {
		return false
	}
	if metric == models.FilesystemCapacityMetric {
		return len(d) == 3
	}
	return len(d) == 0
}
