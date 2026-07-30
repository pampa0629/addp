package events

import "testing"

func TestBuildCleanupImpactDataIsDeterministic(t *testing.T) {
	left, err := BuildCleanupImpactData([]CleanupImpactItem{
		{StableRef: "task:2", Disposition: CleanupImpactWillDisable},
		{StableRef: "task:1", Disposition: CleanupImpactRebindable},
	}, "/develop/tasks")
	if err != nil {
		t.Fatalf("BuildCleanupImpactData() error = %v", err)
	}
	right, err := BuildCleanupImpactData([]CleanupImpactItem{
		{StableRef: "task:1", Disposition: CleanupImpactRebindable},
		{StableRef: "task:2", Disposition: CleanupImpactWillDisable},
	}, "/develop/tasks")
	if err != nil {
		t.Fatalf("BuildCleanupImpactData() error = %v", err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("digest differs by input order: %q != %q", left.Digest, right.Digest)
	}
	if left.Summary.Rebindable != 1 || left.Summary.WillDisable != 1 || left.Summary.Total() != 2 {
		t.Fatalf("summary = %#v", left.Summary)
	}
	if left.ManagementPath != "/develop/tasks" {
		t.Fatalf("management_path = %q", left.ManagementPath)
	}
}

func TestBuildCleanupImpactDataRejectsInvalidItem(t *testing.T) {
	if _, err := BuildCleanupImpactData([]CleanupImpactItem{{StableRef: "", Disposition: CleanupImpactRebindable}}, ""); err == nil {
		t.Fatal("expected empty stable_ref to fail")
	}
	if _, err := BuildCleanupImpactData([]CleanupImpactItem{{StableRef: "task:1", Disposition: "unknown"}}, ""); err == nil {
		t.Fatal("expected unsupported disposition to fail")
	}
}
