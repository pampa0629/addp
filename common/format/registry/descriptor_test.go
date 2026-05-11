package registry

import "testing"

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
	if view.DataType != DataTypeDocument {
		t.Fatalf("DataType = %q, want %q", view.DataType, DataTypeDocument)
	}
	if view.Preview.FrontendRenderer != "markdown" {
		t.Fatalf("FrontendRenderer = %q, want markdown", view.Preview.FrontendRenderer)
	}
	if !view.Transfer.Read || !view.Transfer.Write {
		t.Fatalf("Transfer = %#v, want read/write", view.Transfer)
	}
}

func TestRegisterDescriptorRecordsFormatConflictAndHonorsPriority(t *testing.T) {
	registry := newRegistry()
	if err := registry.RegisterDescriptor(Descriptor{
		ID:       "builtin-test",
		Format:   Format("test"),
		DataType: DataTypeDocument,
		Priority: 10,
	}); err != nil {
		t.Fatalf("register builtin descriptor: %v", err)
	}
	if err := registry.RegisterDescriptor(Descriptor{
		ID:       "plugin-test-low",
		Format:   Format("test"),
		DataType: DataTypeTable,
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
		DataType: DataTypeDocument,
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
		DataType: DataTypeDocument,
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
