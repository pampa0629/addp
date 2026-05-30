package pluginmanifest

import "testing"

func TestValidateTopLevelFieldsRejectsLegacyDefaults(t *testing.T) {
	cases := []struct {
		name    string
		raw     []byte
		allowed []string
	}{
		{
			name:    "preview",
			raw:     []byte(`{"default_providers":[]}`),
			allowed: []string{"version", "description", "providers", "notes"},
		},
		{
			name:    "content",
			raw:     []byte(`{"default_content_plugins":[]}`),
			allowed: []string{"version", "description", "content_plugins", "notes"},
		},
	}
	for _, tt := range cases {
		if err := ValidateTopLevelFields(tt.raw, tt.allowed...); err == nil {
			t.Fatalf("%s ValidateTopLevelFields(%s) error = nil, want unsupported field", tt.name, tt.raw)
		}
	}
}

func TestValidateTopLevelFieldsAcceptsAllowedFields(t *testing.T) {
	cases := []struct {
		name    string
		raw     []byte
		allowed []string
	}{
		{
			name:    "preview",
			raw:     []byte(`{"version":"1.0.0","description":"x","providers":[],"notes":[]}`),
			allowed: []string{"version", "description", "providers", "notes"},
		},
		{
			name:    "content",
			raw:     []byte(`{"version":"1.0.0","description":"x","content_plugins":[],"notes":[]}`),
			allowed: []string{"version", "description", "content_plugins", "notes"},
		},
	}
	for _, tt := range cases {
		if err := ValidateTopLevelFields(tt.raw, tt.allowed...); err != nil {
			t.Fatalf("%s ValidateTopLevelFields() error = %v", tt.name, err)
		}
	}
}

func TestValidateTopLevelFieldsRejectsCrossFileFields(t *testing.T) {
	if err := ValidateTopLevelFields([]byte(`{"providers":[]}`), "version", "description", "content_plugins", "notes"); err == nil {
		t.Fatal("content config should reject providers")
	}
	if err := ValidateTopLevelFields([]byte(`{"content_plugins":[]}`), "version", "description", "providers", "notes"); err == nil {
		t.Fatal("preview config should reject content_plugins")
	}
}
