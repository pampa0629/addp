package format

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestListFormatDescriptorsReturnsSortedCopies(t *testing.T) {
	registry := newDescriptorRegistry()
	for _, descriptor := range []FormatDescriptor{
		{
			ID:       "builtin-markdown",
			Format:   FormatMarkdown,
			DataType: datatype.Document,
			Layouts:  []string{LayoutSingle},
		},
		formatMediaDescriptorForTest("builtin-webp", FormatWebP),
		formatMediaDescriptorForTest("builtin-svg", FormatSVG),
		formatMediaDescriptorForTest("builtin-mp4", FormatMP4),
		formatMediaDescriptorForTest("builtin-mp3", FormatMP3),
	} {
		if err := registry.RegisterFormatDescriptor(descriptor); err != nil {
			t.Fatalf("register descriptor %s: %v", descriptor.ID, err)
		}
	}

	descriptors := registry.ListFormatDescriptors()
	for i := 1; i < len(descriptors); i++ {
		if descriptors[i-1].Format > descriptors[i].Format {
			t.Fatalf("descriptors are not sorted: %s before %s", descriptors[i-1].Format, descriptors[i].Format)
		}
	}

	descriptors[0].Layouts = append(descriptors[0].Layouts, "changed")
	next := registry.ListFormatDescriptors()
	if len(next[0].Layouts) == len(descriptors[0].Layouts) {
		t.Fatal("ListFormatDescriptors returned mutable internal descriptor slices")
	}
}

func TestGetFormatDescriptorReturnsStaticFacts(t *testing.T) {
	registry := newDescriptorRegistry()
	if err := registry.RegisterFormatDescriptor(FormatDescriptor{
		ID:       "builtin-markdown",
		Format:   FormatMarkdown,
		DataType: datatype.Document,
		Layouts:  []string{LayoutSingle},
	}); err != nil {
		t.Fatalf("register descriptor: %v", err)
	}

	descriptor, ok := registry.GetFormatDescriptor(FormatMarkdown)
	if !ok {
		t.Fatal("expected markdown descriptor")
	}
	if descriptor.ID != "builtin-markdown" {
		t.Fatalf("ID = %q, want builtin-markdown", descriptor.ID)
	}
	if descriptor.Format != FormatMarkdown {
		t.Fatalf("Format = %q, want %q", descriptor.Format, FormatMarkdown)
	}
	if descriptor.DataType != datatype.Document {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Document)
	}
}

func formatMediaDescriptorForTest(id string, formatType FormatType) FormatDescriptor {
	return FormatDescriptor{
		ID:       id,
		Format:   formatType,
		DataType: datatype.Media,
		Layouts:  []string{LayoutSingle},
	}
}

func TestNormalizeLayoutsCanonicalizesValues(t *testing.T) {
	got := NormalizeLayouts([]string{" Whole ", "single", "WHOLE", "", "multi"})
	want := []string{LayoutMulti, LayoutSingle, LayoutWhole}
	if len(got) != len(want) {
		t.Fatalf("NormalizeLayouts() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeLayouts() = %#v, want %#v", got, want)
		}
	}
}

func TestBootstrapFallbackDoesNotMatchEmptyExtension(t *testing.T) {
	if got := fallbackFormatByExtension(""); got != FormatUnknown {
		t.Fatalf("fallbackFormatByExtension(\"\") = %q, want unknown", got)
	}
}

func TestDescriptorMIMEPatternPrecedesBootstrapFallback(t *testing.T) {
	registry := newDescriptorRegistry()
	previous := globalDescriptorRegistry
	globalDescriptorRegistry = registry
	defer func() {
		globalDescriptorRegistry = previous
	}()

	if err := RegisterFormatDescriptor(FormatDescriptor{
		ID:       "test-image-family",
		Format:   FormatImage,
		DataType: datatype.Media,
		Layouts:  []string{LayoutSingle},
		Identification: FormatIdentification{
			MimeTypes: []string{"image/*"},
		},
	}); err != nil {
		t.Fatalf("RegisterFormatDescriptor() error = %v", err)
	}

	if got := MIMEToFormat("image/x-custom"); got != FormatImage {
		t.Fatalf("MIMEToFormat(image/x-custom) = %q, want image", got)
	}
}

func TestRegisterFormatDescriptorRejectsUnknownLayout(t *testing.T) {
	registry := newDescriptorRegistry()
	err := registry.RegisterFormatDescriptor(FormatDescriptor{
		ID:       "bad-layout",
		Format:   FormatType("bad-layout"),
		DataType: datatype.Document,
		Layouts:  []string{"bundle"},
	})
	if err == nil {
		t.Fatal("expected unknown layout error")
	}
}

func TestRegisterFormatDescriptorRecordsFormatConflictAndHonorsPriority(t *testing.T) {
	registry := newDescriptorRegistry()
	if err := registry.RegisterFormatDescriptor(FormatDescriptor{
		ID:       "builtin-test",
		Format:   FormatType("test"),
		DataType: datatype.Document,
		Priority: 10,
	}); err != nil {
		t.Fatalf("register builtin descriptor: %v", err)
	}
	if err := registry.RegisterFormatDescriptor(FormatDescriptor{
		ID:       "plugin-test-low",
		Format:   FormatType("test"),
		DataType: datatype.Table,
		Priority: 5,
	}); err != nil {
		t.Fatalf("register low priority descriptor: %v", err)
	}

	descriptor, ok := registry.GetFormatDescriptor(FormatType("test"))
	if !ok {
		t.Fatal("expected test descriptor")
	}
	if descriptor.ID != "builtin-test" {
		t.Fatalf("descriptor ID = %q, want builtin-test", descriptor.ID)
	}

	conflicts := registry.ListFormatConflictDiagnostics()
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %#v, want one conflict", conflicts)
	}
	if conflicts[0].Overridden {
		t.Fatalf("conflict overridden = true, want false")
	}

	if err := registry.RegisterFormatDescriptor(FormatDescriptor{
		ID:       "plugin-test-high",
		Format:   FormatType("test"),
		DataType: datatype.Table,
		Priority: 20,
	}); err != nil {
		t.Fatalf("register high priority descriptor: %v", err)
	}
	descriptor, ok = registry.GetFormatDescriptor(FormatType("test"))
	if !ok {
		t.Fatal("expected test descriptor")
	}
	if descriptor.ID != "plugin-test-high" {
		t.Fatalf("descriptor ID = %q, want plugin-test-high", descriptor.ID)
	}
}

func TestRegisterFormatDescriptorRecordsIdentificationConflicts(t *testing.T) {
	registry := newDescriptorRegistry()
	if err := registry.RegisterFormatDescriptor(FormatDescriptor{
		ID:       "csv-a",
		Format:   FormatType("csv-a"),
		DataType: datatype.Table,
		Identification: FormatIdentification{
			Extensions: []string{".csv"},
			MimeTypes:  []string{"text/csv"},
		},
	}); err != nil {
		t.Fatalf("register csv-a: %v", err)
	}
	if err := registry.RegisterFormatDescriptor(FormatDescriptor{
		ID:       "csv-b",
		Format:   FormatType("csv-b"),
		DataType: datatype.Table,
		Identification: FormatIdentification{
			Extensions: []string{"csv"},
			MimeTypes:  []string{"text/csv"},
		},
	}); err != nil {
		t.Fatalf("register csv-b: %v", err)
	}

	conflicts := registry.ListFormatConflictDiagnostics()
	if len(conflicts) != 2 {
		t.Fatalf("conflicts = %#v, want extension and mime conflicts", conflicts)
	}
}
