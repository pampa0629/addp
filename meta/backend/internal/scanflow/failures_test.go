package scanflow

import (
	"errors"
	"fmt"
	"testing"
)

func TestFailedTargetCollectorAggregatesAndBoundsSamples(t *testing.T) {
	t.Parallel()

	collector := &FailedTargetCollector{}
	for i := 0; i < MaxFailedTargetSamples+3; i++ {
		collector.Add(fmt.Sprintf("target-%d", i), errors.New("boom"))
	}

	err := collector.Err()
	count, samples := FailedTargetDetails(err)
	if count != MaxFailedTargetSamples+3 {
		t.Fatalf("failed target count = %d", count)
	}
	if len(samples) != MaxFailedTargetSamples {
		t.Fatalf("failed target samples = %d", len(samples))
	}
	if got := err.Error(); got != "23 个扫描目标失败，首个失败目标 target-0: boom" {
		t.Fatalf("error = %q", got)
	}
}

func TestFailedTargetCollectorMergesNestedFailures(t *testing.T) {
	t.Parallel()

	inner := &FailedTargetCollector{}
	inner.Add("a", errors.New("first"))
	inner.Add("b", errors.New("second"))

	outer := &FailedTargetCollector{}
	outer.Add("ignored-wrapper", fmt.Errorf("scan directory: %w", inner.Err()))
	outer.Add("c", errors.New("third"))

	count, samples := FailedTargetDetails(outer.Err())
	if count != 3 || len(samples) != 3 {
		t.Fatalf("count=%d samples=%#v", count, samples)
	}
	if samples[0].Target != "a" || samples[2].Target != "c" {
		t.Fatalf("samples=%#v", samples)
	}
}

func TestFailedTargetCollectorCountsEachTargetOnce(t *testing.T) {
	t.Parallel()

	collector := &FailedTargetCollector{}
	collector.Add("same", errors.New("first"))
	collector.Add("same", errors.New("second"))

	count, samples := FailedTargetDetails(collector.Err())
	if count != 1 || len(samples) != 1 {
		t.Fatalf("count=%d samples=%#v", count, samples)
	}
}
