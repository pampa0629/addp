package zip

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/addp/common/format"
)

func TestDescribeContainerReturnsLightweightEntries(t *testing.T) {
	t.Parallel()

	data := zipBytes(t, map[string]string{
		"docs/readme.txt": "hello",
		"data/cities.csv": "id,name\n1,Hangzhou\n",
	})
	info, err := NewPlugin(nil).DescribeContainer(context.Background(), bytes.NewReader(data), format.ContainerParseOptions(100, 0))
	if err != nil {
		t.Fatalf("DescribeContainer() error = %v", err)
	}
	if info.Format != format.FormatZIP {
		t.Fatalf("Format = %q, want zip", info.Format)
	}
	if info.ChildCount != 2 || len(info.Children) != 2 {
		t.Fatalf("children = %#v, want 2 entries", info.Children)
	}
	if info.DefaultChild != "data/cities.csv" {
		t.Fatalf("DefaultChild = %q, want first sorted file", info.DefaultChild)
	}
	child := info.Children[0]
	if child.Name != "data/cities.csv" || child.Kind != "file" || child.DataType != format.FormatDataTypeTable {
		t.Fatalf("child = %#v, want CSV table entry", child)
	}
	if child.Properties["format"] != string(format.FormatCSV) {
		t.Fatalf("child format = %#v, want csv", child.Properties["format"])
	}
	if len(child.Fields) != 0 {
		t.Fatalf("zip container child should not carry fields: %#v", child)
	}
}

func TestDescribeContainerHonorsEntryLimit(t *testing.T) {
	t.Parallel()

	data := zipBytes(t, map[string]string{
		"a.txt": "a",
		"b.txt": "b",
	})
	info, err := NewPlugin(nil).DescribeContainer(context.Background(), bytes.NewReader(data), format.ContainerParseOptions(1, 0))
	if err != nil {
		t.Fatalf("DescribeContainer() error = %v", err)
	}
	if len(info.Children) != 1 || info.ChildCount != 2 {
		t.Fatalf("children = %#v child_count=%d, want one sampled of two", info.Children, info.ChildCount)
	}
	if info.FormatInfo["children_truncated"] != true {
		t.Fatalf("children_truncated = %#v, want true", info.FormatInfo["children_truncated"])
	}
}

func TestDescribeContainerZeroEntryLimitListsAllEntries(t *testing.T) {
	t.Parallel()

	data := zipBytes(t, map[string]string{
		"a.txt": "a",
		"b.txt": "b",
	})
	info, err := NewPlugin(nil).DescribeContainer(context.Background(), bytes.NewReader(data), format.ContainerParseOptions(0, 0))
	if err != nil {
		t.Fatalf("DescribeContainer() error = %v", err)
	}
	if len(info.Children) != 2 || info.ChildCount != 2 {
		t.Fatalf("children = %#v child_count=%d, want all entries", info.Children, info.ChildCount)
	}
	if info.FormatInfo["children_truncated"] != false {
		t.Fatalf("children_truncated = %#v, want false", info.FormatInfo["children_truncated"])
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
