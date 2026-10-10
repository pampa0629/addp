//go:build !linux && !darwin

package processmetrics

func sampleSelf() (Sample, error) { return Sample{}, ErrSampleUnavailable }
