package scanflow

import "testing"

func TestNormalizeScanDepth(t *testing.T) {
	t.Parallel()

	got, err := NormalizeScanDepth("", ScanDepthBasic)
	if err != nil || got != ScanDepthBasic {
		t.Fatalf("default depth = %q, err=%v", got, err)
	}

	got, err = NormalizeScanDepth("DEEP", ScanDepthBasic)
	if err != nil || got != ScanDepthDeep {
		t.Fatalf("normalized depth = %q, err=%v", got, err)
	}

	if _, err = NormalizeScanDepth("shallow", ScanDepthBasic); err == nil {
		t.Fatal("shallow should be rejected")
	}
}
