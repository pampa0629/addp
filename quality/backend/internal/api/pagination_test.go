package api

import "testing"

func TestPageParamsNormalizesBounds(t *testing.T) {
	if page, size := pageParams("0", "1000"); page != 1 || size != 100 {
		t.Fatalf("pageParams() = %d/%d, want 1/100", page, size)
	}
	if page, size := pageParams("", ""); page != 1 || size != 20 {
		t.Fatalf("pageParams() defaults = %d/%d, want 1/20", page, size)
	}
}

func TestTotalPagesKeepsEmptyListAtOnePage(t *testing.T) {
	if got := totalPages(0, 20); got != 1 {
		t.Fatalf("totalPages(0, 20) = %d, want 1", got)
	}
	if got := totalPages(21, 20); got != 2 {
		t.Fatalf("totalPages(21, 20) = %d, want 2", got)
	}
}

func TestOptionalPositiveID(t *testing.T) {
	if got, err := optionalPositiveID(""); err != nil || got != 0 {
		t.Fatalf("optionalPositiveID(empty) = %d/%v, want 0/nil", got, err)
	}
	if got, err := optionalPositiveID("12"); err != nil || got != 12 {
		t.Fatalf("optionalPositiveID(12) = %d/%v, want 12/nil", got, err)
	}
	if _, err := optionalPositiveID("0"); err == nil {
		t.Fatal("optionalPositiveID(0) error = nil")
	}
}

func TestRequiredPositiveID(t *testing.T) {
	if got, err := requiredPositiveID("12"); err != nil || got != 12 {
		t.Fatalf("requiredPositiveID(12) = %d/%v, want 12/nil", got, err)
	}
	for _, value := range []string{"", "0", "-1", "not-an-id"} {
		if _, err := requiredPositiveID(value); err == nil {
			t.Fatalf("requiredPositiveID(%q) error = nil", value)
		}
	}
}
