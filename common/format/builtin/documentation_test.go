package builtin

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type documentedFormat struct {
	dataType datatype.DataType
	format   format.FormatType
	layout   string
}

type formatDocuments struct{ concept, overview, columnar string }

var documentationCode = regexp.MustCompile("`([^`]+)`")

func TestFormatDocumentation(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	documents := formatDocuments{
		concept:  read("docs/concepts/addp数据类型和格式体系图.md"),
		overview: read("docs/spec/addp内置数据类型与文件格式规范.md"),
		columnar: read("docs/spec/builtin-formats/列式文件与数据容器.md"),
	}
	// Read the enum with Go's parser rather than copying its members into this test.
	source, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "common/datatype/data_type.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var dataTypes []datatype.DataType
	for _, declaration := range source.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, declaration := range group.Specs {
			value := declaration.(*ast.ValueSpec)
			typeName, ok := value.Type.(*ast.Ident)
			if !ok || typeName.Name != "DataType" {
				continue
			}
			for _, expression := range value.Values {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatal("DataType constants must have literal string identities")
				}
				identity, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				if !datatype.IsKnownDataType(datatype.DataType(identity)) {
					t.Fatalf("declared datatype %q is missing from the runtime known-type set", identity)
				}
				dataTypes = append(dataTypes, datatype.DataType(identity))
			}
		}
	}
	if len(dataTypes) == 0 {
		t.Fatal("no DataType constants found")
	}
	descriptors := format.ListFormatDescriptors()
	if len(descriptors) == 0 {
		t.Fatal("builtin format registry is empty")
	}
	classified := documentedContentClassifications(t)
	validate := func(docs formatDocuments, descriptors []format.FormatDescriptor, types []datatype.DataType) []string {
		return checkFormatDocuments(docs, descriptors, types, classified)
	}
	if issues := validate(documents, descriptors, dataTypes); len(issues) != 0 {
		t.Fatalf("format documentation drift:\n%s", strings.Join(issues, "\n"))
	}
	t.Logf("checked %d datatypes and %d registered formats against both summaries and the Parquet topic", len(dataTypes), len(descriptors))

	// Mutate in-memory documents only: prove that omissions and mutually copied
	// errors cannot turn the two summaries into a second registry.
	tests := []struct {
		name, part, heading    string
		keyColumn              int
		key                    string
		column                 int
		old, replacement, want string
	}{
		{"missing format", "concept", "### 当前支持汇总", 0, "document", 2, "`xml`", "XML", "document/xml"},
		{"unregistered format", "concept", "### 当前支持汇总", 0, "container", 2, "`netcdf`", "`netcdf`、`kmz`", "container/kmz"},
		{"JSON classification", "concept", "### 当前支持汇总", 0, "table", 2, "`json`", "JSON", "table/json"},
		{"PLY classification", "concept", "### 当前支持汇总", 0, "point_cloud", 2, "`ply`", "PLY", "point_cloud/ply"},
		{"wrong datatype", "overview", "## 总览", 3, "xml", 2, "`document`", "`table`", "table/xml/single"},
		{"wrong layout", "overview", "## 总览", 3, "parquet", 1, "`whole`", "`multi`", "table/parquet/multi"},
		{"missing TIFF layout", "overview", "## 总览", 3, "tiff", 1, "`multi`", "multi", "media/tiff/multi"},
		{"topic layout", "columnar", "## Parquet / ORC / Avro / Iceberg", 3, "parquet", 1, "`whole`", "`multi`", "table/parquet/multi"},
		{"removed native datatype", "concept", "## 数据类型", 0, "key_value", -1, "", "", "key_value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := documents
			part := map[string]*string{"concept": &docs.concept, "overview": &docs.overview, "columnar": &docs.columnar}[tt.part]
			before, section, ok := strings.Cut(*part, tt.heading+"\n")
			if !ok {
				t.Fatalf("missing fixture heading: %s", tt.heading)
			}
			lines := strings.Split(section, "\n")
			changed := false
			for i, line := range lines {
				if !strings.HasPrefix(line, "|") {
					continue
				}
				cells := strings.Split(strings.Trim(line, "|"), "|")
				if len(cells) <= tt.keyColumn || !slices.Contains(documentationIDs(cells[tt.keyColumn]), tt.key) {
					continue
				}
				if tt.column < 0 {
					lines = slices.Delete(lines, i, i+1)
				} else {
					if !strings.Contains(cells[tt.column], tt.old) {
						continue
					}
					cells[tt.column] = strings.Replace(cells[tt.column], tt.old, tt.replacement, 1)
					lines[i] = "|" + strings.Join(cells, "|") + "|"
				}
				changed = true
				break
			}
			if !changed {
				t.Fatal("drift fixture did not modify a table row")
			}
			*part = before + tt.heading + "\n" + strings.Join(lines, "\n")
			if issues := strings.Join(validate(docs, descriptors, dataTypes), "\n"); !strings.Contains(issues, tt.want) {
				t.Fatalf("drift not diagnosed as %q:\n%s", tt.want, issues)
			}
		})
	}
	t.Run("new registered format", func(t *testing.T) {
		added := append(slices.Clone(descriptors), format.FormatDescriptor{Format: "new_format", DataType: datatype.Table, Layouts: []string{format.LayoutSingle}})
		if issues := strings.Join(validate(documents, added, dataTypes), "\n"); !strings.Contains(issues, "table/new_format") {
			t.Fatalf("new registration omitted from docs was accepted: %s", issues)
		}
	})
	t.Run("new datatype", func(t *testing.T) {
		if issues := strings.Join(validate(documents, descriptors, append(slices.Clone(dataTypes), "new_type")), "\n"); !strings.Contains(issues, "new_type") {
			t.Fatalf("new datatype omitted from docs was accepted: %s", issues)
		}
	})
	t.Run("same wrong format in both summaries", func(t *testing.T) {
		docs := documents
		docs.concept = strings.ReplaceAll(docs.concept, "`netcdf`", "`kmz`")
		docs.overview = strings.ReplaceAll(docs.overview, "`netcdf`", "`kmz`")
		issues := strings.Join(validate(docs, descriptors, dataTypes), "\n")
		for _, want := range []string{"concept support table: unexpected container/kmz/", "builtin overview: unexpected container/kmz/single"} {
			if !strings.Contains(issues, want) {
				t.Fatalf("matching mistakes in both summaries were accepted: %s", issues)
			}
		}
	})
}

// Only content-dependent additions need fixtures. The full default-format
// inventory and layouts always come from the loaded registry.
func documentedContentClassifications(t *testing.T) []documentedFormat {
	t.Helper()
	jsonProvider, err := format.GetTableInfoProvider(format.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	jsonInfo, err := jsonProvider.DescribeTable(t.Context(), strings.NewReader(`[{"id":1}]`), nil)
	if err != nil || jsonInfo == nil || jsonInfo.Table == nil {
		t.Fatalf("JSON record-set classification missing: %v, %#v", err, jsonInfo)
	}
	pointProvider, err := format.GetPointCloudInfoProvider(format.FormatPLY)
	if err != nil {
		t.Fatal(err)
	}
	point := "ply\nformat ascii 1.0\nelement vertex 1\nproperty float x\nproperty float y\nproperty float z\nend_header\n1 2 3\n"
	pointInfo, err := pointProvider.DescribePointCloud(t.Context(), format.PointCloudDescribeInput{Reader: strings.NewReader(point)}, nil)
	if err != nil || pointInfo == nil || pointInfo.PointCloud == nil {
		t.Fatalf("PLY point-cloud classification missing: %v, %#v", err, pointInfo)
	}
	splatProvider, err := format.GetGaussianSplatInfoProvider(format.FormatPLY)
	if err != nil {
		t.Fatal(err)
	}
	splat := strings.Replace(point, "end_header\n1 2 3\n", "property float opacity\nproperty float scale_0\nproperty float scale_1\nproperty float scale_2\nproperty float rot_0\nproperty float rot_1\nproperty float rot_2\nproperty float rot_3\nproperty float f_dc_0\nproperty float f_dc_1\nproperty float f_dc_2\nend_header\n1 2 3 1 0 0 0 1 0 0 0 0 0 0\n", 1)
	splatInfo, err := splatProvider.DescribeGaussianSplat(t.Context(), format.GaussianSplatDescribeInput{Reader: strings.NewReader(splat), SizeBytes: int64(len(splat))}, nil)
	if err != nil || splatInfo == nil || splatInfo.GaussianSplat == nil {
		t.Fatalf("PLY gaussian-splat classification missing: %v, %#v", err, splatInfo)
	}
	return []documentedFormat{
		{datatype.Table, format.FormatJSON, format.LayoutSingle},
		{datatype.PointCloud, format.FormatPLY, format.LayoutSingle},
		{datatype.GaussianSplat, format.FormatPLY, format.LayoutSingle},
	}
}

func documentationTable(text, heading string, headers []string) ([][]string, error) {
	_, section, ok := strings.Cut(text, heading+"\n")
	if !ok {
		return nil, fmt.Errorf("missing heading %s", heading)
	}
	var rows [][]string
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			if len(rows) != 0 || strings.HasPrefix(line, "#") {
				break
			}
			continue
		}
		cells := strings.Split(strings.TrimPrefix(strings.TrimSuffix(line, "|"), "|"), "|")
		if len(cells) != len(headers) {
			return nil, fmt.Errorf("%s: expected %d table columns: %s", heading, len(headers), line)
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if strings.Trim(cells[0], "-: ") == "" {
			continue
		}
		if rows == nil {
			for i, header := range headers {
				if strings.Trim(cells[i], "`") != header {
					return nil, fmt.Errorf("%s: expected table header %q, got %q", heading, header, cells[i])
				}
			}
		}
		rows = append(rows, cells)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("%s: missing data rows", heading)
	}
	return rows[1:], nil
}

func documentationIDs(cell string) []string {
	var ids []string
	for _, match := range documentationCode.FindAllStringSubmatch(cell, -1) {
		ids = append(ids, match[1])
	}
	return ids
}

func checkFormatDocuments(docs formatDocuments, descriptors []format.FormatDescriptor, types []datatype.DataType, classified []documentedFormat) []string {
	var issues []string
	compare := func(label string, got, want map[documentedFormat]bool) {
		for entry := range want {
			if !got[entry] {
				issues = append(issues, fmt.Sprintf("%s: missing %s/%s/%s", label, entry.dataType, entry.format, entry.layout))
			}
		}
		for entry := range got {
			if !want[entry] {
				issues = append(issues, fmt.Sprintf("%s: unexpected %s/%s/%s", label, entry.dataType, entry.format, entry.layout))
			}
		}
	}
	readTable := func(text, heading string, headers []string) [][]string {
		rows, err := documentationTable(text, heading, headers)
		if err != nil {
			issues = append(issues, err.Error())
		}
		return rows
	}
	wantTypes := map[documentedFormat]bool{}
	for _, dataType := range types {
		wantTypes[documentedFormat{dataType: dataType}] = true
	}
	for _, table := range []struct {
		heading string
		headers []string
	}{
		{"## 数据类型", []string{"数据类型", "含义", "典型处理方式"}},
		{"### 当前支持汇总", []string{"数据类型", "原生 / 承载引擎示例", "当前内置格式与容器 child", "说明"}},
	} {
		got := map[documentedFormat]bool{}
		for _, row := range readTable(docs.concept, table.heading, table.headers) {
			ids := documentationIDs(row[0])
			if len(ids) != 1 {
				issues = append(issues, table.heading+": each row must name one datatype in backticks")
				continue
			}
			entry := documentedFormat{dataType: datatype.DataType(ids[0])}
			if got[entry] {
				issues = append(issues, table.heading+": duplicate datatype "+ids[0])
			}
			got[entry] = true
		}
		compare(table.heading, got, wantTypes)
	}
	want := map[documentedFormat]bool{}
	for _, descriptor := range descriptors {
		for _, layout := range descriptor.Layouts {
			want[documentedFormat{descriptor.DataType, descriptor.Format, layout}] = true
		}
	}
	for _, entry := range classified {
		want[entry] = true
	}
	gotPairs, wantPairs := map[documentedFormat]bool{}, map[documentedFormat]bool{}
	for entry := range want {
		entry.layout = ""
		wantPairs[entry] = true
	}
	for _, row := range readTable(docs.concept, "### 当前支持汇总", []string{"数据类型", "原生 / 承载引擎示例", "当前内置格式与容器 child", "说明"}) {
		for _, dataType := range documentationIDs(row[0]) {
			for _, identity := range documentationIDs(row[2]) {
				gotPairs[documentedFormat{dataType: datatype.DataType(dataType), format: format.FormatType(identity)}] = true
			}
		}
	}
	compare("concept support table", gotPairs, wantPairs)
	got := map[documentedFormat]bool{}
	for _, row := range readTable(docs.overview, "## 总览", []string{"格式 / 场景", "layout", "data_type", "format", "说明"}) {
		layouts, dataTypes, formats := documentationIDs(row[1]), documentationIDs(row[2]), documentationIDs(row[3])
		if len(layouts) == 0 || len(dataTypes) == 0 || len(formats) == 0 {
			issues = append(issues, "overview: each row must name layout, datatype and format IDs in backticks: "+row[0])
		}
		for _, dataType := range dataTypes {
			for _, identity := range formats {
				for _, layout := range layouts {
					got[documentedFormat{datatype.DataType(dataType), format.FormatType(identity), layout}] = true
				}
			}
		}
	}
	compare("builtin overview", got, want)
	_, parquetSection, _ := strings.Cut(docs.columnar, "## Parquet / ORC / Avro / Iceberg\n")
	got, wantParquet := map[documentedFormat]bool{}, map[documentedFormat]bool{}
	for entry := range want {
		if entry.format == format.FormatParquet {
			wantParquet[entry] = true
		}
	}
	for _, row := range readTable(parquetSection, "### 识别与组织", []string{"场景", "layout", "data_type", "format"}) {
		if !slices.Contains(documentationIDs(row[3]), string(format.FormatParquet)) {
			continue
		}
		for _, dataType := range documentationIDs(row[2]) {
			for _, layout := range documentationIDs(row[1]) {
				got[documentedFormat{datatype.DataType(dataType), format.FormatParquet, layout}] = true
			}
		}
	}
	compare("Parquet topic", got, wantParquet)
	slices.Sort(issues)
	return issues
}
