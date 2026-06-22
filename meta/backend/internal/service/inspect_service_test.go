package service

import "testing"

func TestParseInfraLocatorParsesMinioObject(t *testing.T) {
	t.Parallel()

	loc, err := parseInfraLocator("addp-infra://minio/manager/tenant_7/import/20260621/u1/roads.shp?type=object")
	if err != nil {
		t.Fatalf("parseInfraLocator() error = %v", err)
	}
	if loc.Kind != "minio" || loc.Namespace != "manager" {
		t.Fatalf("locator kind/namespace = %q/%q", loc.Kind, loc.Namespace)
	}
	if got := loc.Path; len(got) != 5 || got[0] != "tenant_7" || got[4] != "roads.shp" {
		t.Fatalf("locator path = %#v", got)
	}
}

func TestShapefileExtensionRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ext      string
		role     string
		required bool
		primary  bool
	}{
		{ext: ".shp", role: "main", required: true, primary: true},
		{ext: ".shx", role: "index", required: true},
		{ext: ".dbf", role: "attributes", required: true},
		{ext: ".prj", role: "projection"},
		{ext: ".qpj", role: "projection"},
		{ext: ".cpg", role: "encoding"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.ext, func(t *testing.T) {
			t.Parallel()
			if got := roleFromExtension(tt.ext); got != tt.role {
				t.Fatalf("roleFromExtension(%q) = %q, want %q", tt.ext, got, tt.role)
			}
			if got := requiredShapefileExtension(tt.ext); got != tt.required {
				t.Fatalf("requiredShapefileExtension(%q) = %v, want %v", tt.ext, got, tt.required)
			}
			if got := isPrimaryShapefileExtension(tt.ext); got != tt.primary {
				t.Fatalf("isPrimaryShapefileExtension(%q) = %v, want %v", tt.ext, got, tt.primary)
			}
		})
	}
}
