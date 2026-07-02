package ifc

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestIFCDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatIFC {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatIFC)
	}
	if descriptor.DataType != datatype.Model3D {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Model3D)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribeModel3DReadsIFCSummary(t *testing.T) {
	content := []byte(`ISO-10303-21;
HEADER;
FILE_DESCRIPTION(('ViewDefinition [CoordinationView]'),'2;1');
FILE_NAME('sample.ifc','2026-07-01T00:00:00',('ADDP'),('ADDP'),'','', '');
FILE_SCHEMA(('IFC4X3_ADD2'));
ENDSEC;
DATA;
#1=IFCPROJECT('0',$,'Project',$,$,$,$,$,$);
#2=IFCSITE('1',$,'Site',$,$,$,$,$,$,$,$,$,$,$);
#3=IFCBUILDING('2',$,'Building',$,$,$,$,$,$,$,$);
#4=IFCWALL('3',$,'Wall',$,$,$,$,$);
#5=IFCWALL('4',$,'Wall 2',$,$,$,$,$);
ENDSEC;
END-ISO-10303-21;`)

	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(content), nil)
	if err != nil {
		t.Fatalf("DescribeModel3D() error = %v", err)
	}
	if result == nil || result.Model3D == nil {
		t.Fatalf("DescribeModel3D() = %#v, want model info", result)
	}
	if result.Model3D.ModelKind != datatype.Model3DKindBIMModel {
		t.Fatalf("ModelKind = %q, want bim_model", result.Model3D.ModelKind)
	}
	if result.FormatInfo["schema_version"] != "IFC4X3" {
		t.Fatalf("schema_version = %#v, want IFC4X3", result.FormatInfo["schema_version"])
	}
	if result.FormatInfo["entity_count"] != int64(5) {
		t.Fatalf("entity_count = %#v, want 5", result.FormatInfo["entity_count"])
	}
	entityCounts, ok := result.FormatInfo["entity_type_counts"].(map[string]int64)
	if !ok {
		t.Fatalf("entity_type_counts = %#v, want map[string]int64", result.FormatInfo["entity_type_counts"])
	}
	if entityCounts["IFCWALL"] != 2 {
		t.Fatalf("IFCWALL count = %d, want 2", entityCounts["IFCWALL"])
	}
}

func TestSniffFormatRecognizesIFC(t *testing.T) {
	peek := []byte("ISO-10303-21;\nHEADER;\nFILE_SCHEMA(('IFC2X3'));\n")
	if !NewPlugin().SniffFormat(peek) {
		t.Fatal("SniffFormat(IFC) = false, want true")
	}
}

func TestDescribeModel3DReadsExternalIFCSamples(t *testing.T) {
	sampleDir := strings.TrimSpace(os.Getenv("ADDP_IFC_SAMPLE_DIR"))
	if sampleDir == "" {
		t.Skip("set ADDP_IFC_SAMPLE_DIR to validate external IFC samples")
	}
	entries, err := os.ReadDir(sampleDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", sampleDir, err)
	}
	plugin := NewPlugin()
	seen := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ifc") {
			continue
		}
		seen++
		path := filepath.Join(sampleDir, entry.Name())
		t.Run(entry.Name(), func(t *testing.T) {
			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("Open(%q) error = %v", path, err)
			}
			defer file.Close()

			result, err := plugin.DescribeModel3D(context.Background(), file, nil)
			if err != nil {
				t.Fatalf("DescribeModel3D(%q) error = %v", path, err)
			}
			if result == nil || result.Model3D == nil {
				t.Fatalf("DescribeModel3D(%q) = %#v, want model info", path, result)
			}
			if result.Model3D.ModelKind != datatype.Model3DKindBIMModel {
				t.Fatalf("ModelKind = %q, want bim_model", result.Model3D.ModelKind)
			}
			schemas, ok := result.FormatInfo["schema_identifiers"].([]string)
			if !ok || len(schemas) == 0 {
				t.Fatalf("schema_identifiers = %#v, want non-empty []string", result.FormatInfo["schema_identifiers"])
			}
			entityCount, ok := result.FormatInfo["entity_count"].(int64)
			if !ok || entityCount == 0 {
				t.Fatalf("entity_count = %#v, want positive int64", result.FormatInfo["entity_count"])
			}
			if result.FormatInfo["scan_complete"] != true {
				t.Fatalf("scan_complete = %#v, want true", result.FormatInfo["scan_complete"])
			}
		})
	}
	if seen == 0 {
		t.Fatalf("no .ifc sample found in %q", sampleDir)
	}
}
