package registry

import (
	"testing"

	"github.com/addp/common/datatype"
)

func init() {
	for _, descriptor := range []Descriptor{
		{
			ID:            "builtin-markdown",
			Format:        FormatMarkdown,
			DataType:      datatype.DataTypeDocument,
			Layouts:       []string{LayoutSingle},
			ProviderHints: []string{ProviderDocument},
			ContentReaders: []string{
				ContentReaderDocumentText,
				ContentReaderRawContent,
			},
			TransferRead:  true,
			TransferWrite: true,
		},
		mediaDescriptorForTest("builtin-webp", FormatWebP),
		mediaDescriptorForTest("builtin-svg", FormatSVG),
		mediaDescriptorForTest("builtin-mp4", FormatMP4),
		mediaDescriptorForTest("builtin-mp3", FormatMP3),
	} {
		mustRegisterDescriptor(descriptor)
	}
}

func mediaDescriptorForTest(id string, format Format) Descriptor {
	return Descriptor{
		ID:            id,
		Format:        format,
		DataType:      datatype.DataTypeMedia,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderMedia},
		ContentReaders: []string{
			ContentReaderRawContent,
			ContentReaderRangeContent,
		},
	}
}

func TestListDescriptorsReturnsSortedCopies(t *testing.T) {
	descriptors := ListDescriptors()
	if len(descriptors) == 0 {
		t.Fatal("expected builtin descriptors")
	}
	for i := 1; i < len(descriptors); i++ {
		if descriptors[i-1].Format > descriptors[i].Format {
			t.Fatalf("descriptors are not sorted: %s before %s", descriptors[i-1].Format, descriptors[i].Format)
		}
	}

	descriptors[0].Layouts = append(descriptors[0].Layouts, "changed")
	next := ListDescriptors()
	if len(next[0].Layouts) == len(descriptors[0].Layouts) {
		t.Fatal("ListDescriptors returned mutable internal descriptor slices")
	}
}

func TestCapabilityViewFromDescriptor(t *testing.T) {
	view, ok := GetCapabilityView(FormatMarkdown)
	if !ok {
		t.Fatal("expected markdown capability view")
	}
	if view.PluginID != "builtin-markdown" {
		t.Fatalf("PluginID = %q, want builtin-markdown", view.PluginID)
	}
	if view.Format != FormatMarkdown {
		t.Fatalf("Format = %q, want %q", view.Format, FormatMarkdown)
	}
	if view.DataType != datatype.DataTypeDocument {
		t.Fatalf("DataType = %q, want %q", view.DataType, datatype.DataTypeDocument)
	}
	if !containsStringForDescriptorTest(view.ContentReaders, ContentReaderDocumentText) {
		t.Fatalf("ContentReaders = %#v, want document_text", view.ContentReaders)
	}
	if !view.Transfer.Read || !view.Transfer.Write {
		t.Fatalf("Transfer = %#v, want read/write", view.Transfer)
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

func TestRegisterDescriptorRejectsUnknownLayout(t *testing.T) {
	registry := newRegistry()
	err := registry.RegisterDescriptor(Descriptor{
		ID:       "bad-layout",
		Format:   Format("bad-layout"),
		DataType: datatype.DataTypeDocument,
		Layouts:  []string{"bundle"},
	})
	if err == nil {
		t.Fatal("expected unknown layout error")
	}
}

func TestMediaDescriptorsDeclareRawRangeOnly(t *testing.T) {
	tests := []Format{
		FormatWebP,
		FormatSVG,
		FormatMP4,
		FormatMP3,
	}
	for _, format := range tests {
		t.Run(string(format), func(t *testing.T) {
			view, ok := GetCapabilityView(format)
			if !ok {
				t.Fatalf("expected %s capability view", format)
			}
			if view.DataType != datatype.DataTypeMedia {
				t.Fatalf("DataType = %q, want %q", view.DataType, datatype.DataTypeMedia)
			}
			if !containsStringForDescriptorTest(view.ContentReaders, ContentReaderRawContent) ||
				!containsStringForDescriptorTest(view.ContentReaders, ContentReaderRangeContent) {
				t.Fatalf("ContentReaders = %#v, want raw and range", view.ContentReaders)
			}
			if view.Transfer.Read || view.Transfer.Write {
				t.Fatalf("Transfer = %#v, media descriptors should not declare transfer", view.Transfer)
			}
		})
	}
}

func containsStringForDescriptorTest(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestRegisterDescriptorRecordsFormatConflictAndHonorsPriority(t *testing.T) {
	registry := newRegistry()
	if err := registry.RegisterDescriptor(Descriptor{
		ID:       "builtin-test",
		Format:   Format("test"),
		DataType: datatype.DataTypeDocument,
		Priority: 10,
	}); err != nil {
		t.Fatalf("register builtin descriptor: %v", err)
	}
	if err := registry.RegisterDescriptor(Descriptor{
		ID:       "plugin-test-low",
		Format:   Format("test"),
		DataType: datatype.DataTypeTable,
		Priority: 5,
	}); err != nil {
		t.Fatalf("register low priority descriptor: %v", err)
	}

	descriptor, ok := registry.GetDescriptor(Format("test"))
	if !ok {
		t.Fatal("expected test descriptor")
	}
	if descriptor.ID != "builtin-test" {
		t.Fatalf("descriptor ID = %q, want builtin-test", descriptor.ID)
	}

	conflicts := registry.ListConflictDiagnostics()
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %#v, want one conflict", conflicts)
	}
	if conflicts[0].Kind != "format" || conflicts[0].Overridden {
		t.Fatalf("conflict = %#v, want non-overriding format conflict", conflicts[0])
	}
}

func TestRegisterDescriptorRecordsIdentificationConflicts(t *testing.T) {
	registry := newRegistry()
	if err := registry.RegisterDescriptor(Descriptor{
		ID:       "builtin-a",
		Format:   Format("a"),
		DataType: datatype.DataTypeDocument,
		Identification: Identification{
			Extensions: []string{"md"},
			MimeTypes:  []string{"Text/Markdown"},
		},
	}); err != nil {
		t.Fatalf("register first descriptor: %v", err)
	}
	if err := registry.RegisterDescriptor(Descriptor{
		ID:       "plugin-b",
		Format:   Format("b"),
		DataType: datatype.DataTypeDocument,
		Identification: Identification{
			Extensions: []string{".MD"},
			MimeTypes:  []string{"text/markdown"},
		},
	}); err != nil {
		t.Fatalf("register second descriptor: %v", err)
	}

	conflicts := registry.ListConflictDiagnostics()
	if len(conflicts) != 2 {
		t.Fatalf("conflicts = %#v, want extension and MIME conflicts", conflicts)
	}
	for _, conflict := range conflicts {
		if conflict.Overridden {
			t.Fatalf("identification conflict should not override descriptor: %#v", conflict)
		}
	}
}
