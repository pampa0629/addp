package zip

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
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
	if info.ChildCount != 2 || len(info.Children) != 2 {
		t.Fatalf("children = %#v, want 2 entries", info.Children)
	}
	if info.DefaultChild != "data/cities.csv" {
		t.Fatalf("DefaultChild = %q, want first sorted file", info.DefaultChild)
	}
	child := info.Children[0]
	if child.Name != "data/cities.csv" || child.ChildKind != "file" || child.DataType != datatype.DataTypeTable {
		t.Fatalf("child = %#v, want CSV table entry", child)
	}
	if child.Format != string(format.FormatCSV) {
		t.Fatalf("child format = %#v, want csv", child.Format)
	}
	if len(child.Fields) != 0 {
		t.Fatalf("zip container child should not carry fields: %#v", child)
	}
}

func TestDescribeContainerKeepsUnknownTextExtensionUnqualified(t *testing.T) {
	t.Parallel()

	data := zipBytes(t, map[string]string{
		"config/docker-compose.yml": "services:\n  app:\n    image: alpine\n",
	})
	info, err := NewPlugin(nil).DescribeContainer(context.Background(), bytes.NewReader(data), format.ContainerParseOptions(100, 0))
	if err != nil {
		t.Fatalf("DescribeContainer() error = %v", err)
	}
	if len(info.Children) != 1 {
		t.Fatalf("children = %#v, want one entry", info.Children)
	}
	child := info.Children[0]
	if child.Name != "config/docker-compose.yml" || child.Format != "" || child.DataType != datatype.DataTypeUnknown {
		t.Fatalf("child = %#v, want unknown unqualified YAML entry", child)
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
	formatInfo, err := NewPlugin(nil).DescribeFormat(context.Background(), bytes.NewReader(data), format.ContainerParseOptions(1, 0))
	if err != nil {
		t.Fatalf("DescribeFormat() error = %v", err)
	}
	if formatInfo["children_truncated"] != true {
		t.Fatalf("children_truncated = %#v, want true", formatInfo["children_truncated"])
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
	formatInfo, err := NewPlugin(nil).DescribeFormat(context.Background(), bytes.NewReader(data), format.ContainerParseOptions(0, 0))
	if err != nil {
		t.Fatalf("DescribeFormat() error = %v", err)
	}
	if formatInfo["children_truncated"] != false {
		t.Fatalf("children_truncated = %#v, want false", formatInfo["children_truncated"])
	}
}

func TestResolveContainerChildReturnsEntryReader(t *testing.T) {
	t.Parallel()

	data := zipBytes(t, map[string]string{
		"data/cities.csv": "id,name\n1,Hangzhou\n",
	})
	parentReader := singleTestContentReader{data: data}
	child := datatype.ContainerChildInfo{
		Name:      "data/cities.csv",
		ChildKind: "file",
		DataType:  datatype.DataTypeTable,
		Format:    string(format.FormatCSV),
		Native: map[string]interface{}{
			"path": "data/cities.csv",
		},
	}
	resolved, err := NewPlugin(nil).ResolveContainerChild(context.Background(), parentReader, contentio.NewRef("outer.zip", contentio.RoleMain), child, nil)
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

func TestResolveContainerChildRefsUseParentQualifiedRefs(t *testing.T) {
	t.Parallel()

	data := zipBytes(t, map[string]string{
		"roads.shp": "shape",
		"roads.shx": "index",
		"roads.dbf": "attrs",
	})
	parentReader := singleTestContentReader{data: data}
	child := datatype.ContainerChildInfo{
		Name:      "roads.shp",
		ChildKind: "file",
		DataType:  datatype.DataTypeTable,
		Format:    string(format.FormatShapefile),
		Native: map[string]interface{}{
			"path": "roads.shp",
		},
		Refs: []datatype.ContainerChildRef{
			{Role: "main", Path: "roads.shp", Primary: true, Required: true},
			{Role: "index", Path: "roads.shx", Required: true},
			{Role: "attributes", Path: "roads.dbf", Required: true},
		},
	}
	resolved, err := NewPlugin(nil).ResolveContainerChild(context.Background(), parentReader, contentio.NewRef("outer.zip", contentio.RoleMain), child, nil)
	if err != nil {
		t.Fatalf("ResolveContainerChild() error = %v", err)
	}
	if len(resolved.Refs) != 3 {
		t.Fatalf("refs = %#v, want 3", resolved.Refs)
	}
	if got := resolved.Refs[0].Ref.Path; got != "outer.zip/roads.shp" {
		t.Fatalf("main ref path = %q, want parent-qualified path", got)
	}
	rc, err := resolved.Reader.Open(context.Background(), resolved.Refs[1].Ref)
	if err != nil {
		t.Fatalf("open index ref: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read index ref: %v", err)
	}
	if string(body) != "index" {
		t.Fatalf("index ref body = %q", body)
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

type singleTestContentReader struct {
	data []byte
}

func (r singleTestContentReader) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.data)), nil
}

func (r singleTestContentReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return &contentio.Stat{Exists: true, Size: int64(len(r.data))}, nil
}
