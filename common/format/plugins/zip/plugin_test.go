package zip

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	"github.com/addp/common/resource"
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

func TestResolveContainerChildReturnsEntryReader(t *testing.T) {
	t.Parallel()

	data := zipBytes(t, map[string]string{
		"data/cities.csv": "id,name\n1,Hangzhou\n",
	})
	parentReader := singleTestResourceReader{data: data}
	child := format.ContainerChildInfo{
		Name:     "data/cities.csv",
		Kind:     "file",
		DataType: format.FormatDataTypeTable,
		Properties: map[string]interface{}{
			"path":   "data/cities.csv",
			"format": string(format.FormatCSV),
		},
	}
	resolved, err := NewPlugin(nil).ResolveContainerChild(context.Background(), parentReader, resource.NewResourceRef("outer.zip", resource.ResourceRoleMain), child, nil)
	if err != nil {
		t.Fatalf("ResolveContainerChild() error = %v", err)
	}
	if resolved.ResourceKind != format.ContainerChildResourceStream || resolved.Format != format.FormatCSV {
		t.Fatalf("resolved = %#v, want stream csv child", resolved)
	}
	rc, err := resolved.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "id,name\n1,Hangzhou\n" {
		t.Fatalf("entry body = %q", body)
	}
}

func TestResolveContainerChildComponentsUseParentQualifiedRefs(t *testing.T) {
	t.Parallel()

	data := zipBytes(t, map[string]string{
		"roads.shp": "shape",
		"roads.shx": "index",
		"roads.dbf": "attrs",
	})
	parentReader := singleTestResourceReader{data: data}
	child := format.ContainerChildInfo{
		Name:     "roads.shp",
		Kind:     "file",
		DataType: format.FormatDataTypeTable,
		Format:   format.FormatShapefile,
		Properties: map[string]interface{}{
			"path":   "roads.shp",
			"format": string(format.FormatShapefile),
		},
		Components: []format.ContainerChildComponent{
			{Role: "main", Path: "roads.shp", Primary: true, Required: true},
			{Role: "index", Path: "roads.shx", Required: true},
			{Role: "attributes", Path: "roads.dbf", Required: true},
		},
	}
	resolved, err := NewPlugin(nil).ResolveContainerChild(context.Background(), parentReader, resource.NewResourceRef("outer.zip", resource.ResourceRoleMain), child, nil)
	if err != nil {
		t.Fatalf("ResolveContainerChild() error = %v", err)
	}
	if len(resolved.Components) != 3 {
		t.Fatalf("components = %#v, want 3", resolved.Components)
	}
	if got := resolved.Components[0].Path; got != "outer.zip/roads.shp" {
		t.Fatalf("main component path = %q, want parent-qualified path", got)
	}
	rc, err := resolved.Reader.Open(context.Background(), resolved.Components[1].ResourceRef)
	if err != nil {
		t.Fatalf("open index component: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read index component: %v", err)
	}
	if string(body) != "index" {
		t.Fatalf("index component body = %q", body)
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

type singleTestResourceReader struct {
	data []byte
}

func (r singleTestResourceReader) Open(context.Context, resource.ResourceRef) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.data)), nil
}

func (r singleTestResourceReader) Stat(context.Context, resource.ResourceRef) (*resource.ResourceMetadata, error) {
	return &resource.ResourceMetadata{Exists: true, Size: int64(len(r.data))}, nil
}

func (r singleTestResourceReader) List(context.Context, resource.ResourceRef) ([]resource.ResourceRef, error) {
	return nil, nil
}
