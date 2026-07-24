package iam

import "testing"

func TestNormalizeUsernameUsesTrimNFKCAndCaseFolding(t *testing.T) {
	tests := map[string]string{
		"  Alice  ": "alice",
		"ＡＬＩＣＥ":     "alice",
		"Straße":    "strasse",
	}
	for input, want := range tests {
		got, err := NormalizeUsername(input)
		if err != nil {
			t.Fatalf("NormalizeUsername(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeUsername(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeUsernameRejectsEmptyValue(t *testing.T) {
	if _, err := NormalizeUsername("　 "); err == nil {
		t.Fatal("NormalizeUsername() accepted an empty username")
	}
}

func TestNormalizeTenantCode(t *testing.T) {
	got, err := NormalizeTenantCode("  Research-Lab ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "research-lab" {
		t.Fatalf("NormalizeTenantCode() = %q", got)
	}
}
