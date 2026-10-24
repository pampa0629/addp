//go:build !linux

package processmetrics

// Unsupported platforms keep the independent process metrics source intact.
func sampleSelfCgroup() *CgroupSample { return nil }
