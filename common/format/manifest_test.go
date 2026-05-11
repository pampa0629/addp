package format

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterFormatPluginManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	content := `{
  "descriptor": {
    "id": "plugin-manifest-doc",
    "version": "v1",
    "format": "manifest_doc",
    "data_type": "document",
    "layouts": ["single"],
    "identification": {
      "extensions": [".mfd"],
      "mime_types": ["text/x-manifest-doc"]
    },
    "content_readers": ["document_text"],
    "transfer_read": true
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	descriptor, err := RegisterFormatPluginManifest(path)
	if err != nil {
		t.Fatalf("RegisterFormatPluginManifest() error = %v", err)
	}
	if descriptor.Format != FormatType("manifest_doc") {
		t.Fatalf("descriptor format = %q, want manifest_doc", descriptor.Format)
	}

	capability, ok := GetFormatCapability(FormatType("manifest_doc"))
	if !ok {
		t.Fatal("manifest descriptor did not update capability registry")
	}
	if capability.DataType != FormatDataTypeDocument {
		t.Fatalf("capability data type = %q, want document", capability.DataType)
	}
	if got := MIMEToFormat("text/x-manifest-doc"); got != FormatType("manifest_doc") {
		t.Fatalf("MIMEToFormat() = %q, want manifest_doc", got)
	}
}

func TestRegisterFormatPluginManifestsFromDir(t *testing.T) {
	dir := t.TempDir()
	content := `{
  "descriptor": {
    "id": "plugin-dir-doc",
    "format": "manifest_dir_doc",
    "data_type": "document",
    "layouts": ["single"],
    "identification": {"extensions": [".mdd"]},
    "content_readers": ["document_text"]
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignored"), 0o600); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	descriptors, err := RegisterFormatPluginManifestsFromDir(dir)
	if err != nil {
		t.Fatalf("RegisterFormatPluginManifestsFromDir() error = %v", err)
	}
	if len(descriptors) != 1 {
		t.Fatalf("registered descriptors = %#v, want one", descriptors)
	}
	if descriptors[0].Format != FormatType("manifest_dir_doc") {
		t.Fatalf("descriptor format = %q, want manifest_dir_doc", descriptors[0].Format)
	}
}
