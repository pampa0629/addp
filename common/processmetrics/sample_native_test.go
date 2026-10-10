//go:build darwin || linux

package processmetrics

import "testing"

func TestNativeSelfSample(t *testing.T) {
	a, err := sampleSelf()
	if err != nil || a.ResidentBytes == 0 || a.CPUSeconds < 0 {
		t.Fatalf("self sample: %v %v", a, err)
	}
	b, err := sampleSelf()
	if err != nil || b.CPUSeconds < a.CPUSeconds || b.ResidentBytes == 0 {
		t.Fatalf("counter regression: %v %v", b, err)
	}
}
