package metacleanup

import "testing"

func TestMinIOGarbageStatsAddBucket(t *testing.T) {
	t.Parallel()

	stats := &MinIOGarbageStats{ByBucket: map[string]int{}}
	stats.addBucket("manager", &MinIOGarbageStats{
		TotalCount:     2,
		TotalSizeBytes: 1024,
	})

	if stats.TotalCount != 2 || stats.TotalSizeBytes != 1024 || stats.ByBucket["manager"] != 2 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestStringSet(t *testing.T) {
	t.Parallel()

	set := stringSet([]string{"a", "b"})
	if !set["a"] || set["c"] {
		t.Fatalf("set = %#v", set)
	}
}
