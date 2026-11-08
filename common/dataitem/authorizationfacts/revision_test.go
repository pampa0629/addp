package authorizationfacts

import "testing"

func TestRevisionRequiresCanonicalGenerationAndVersion(t *testing.T) {
	valid := Revision{RevisionSchemaVersion, "03c40e08-fcbf-468c-932e-d54da85ca8e7", "0"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"", "-1", "01", "1.0", "18446744073709551616"} {
		invalid := valid
		invalid.Version = version
		if invalid.Validate() == nil {
			t.Fatalf("accepted version %q", version)
		}
	}
	for _, generation := range []string{"", "00000000-0000-0000-0000-000000000000", "03C40E08-FCBF-468C-932E-D54DA85CA8E7"} {
		invalid := valid
		invalid.Generation = generation
		if invalid.Validate() == nil {
			t.Fatalf("accepted generation %q", generation)
		}
	}
}
