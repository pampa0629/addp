package pdf

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestPluginDescribeDocument(t *testing.T) {
	plugin := NewPlugin(nil)
	info, err := plugin.DescribeDocument(context.Background(), strings.NewReader("%PDF-1.4\n1 0 obj << /Type /Page >>\n"), nil)
	if err != nil {
		t.Fatalf("DescribeDocument() error = %v", err)
	}
	if info == nil {
		t.Fatal("DescribeDocument() returned nil")
	}
	if info.PageCount != 1 {
		t.Fatalf("PageCount = %d, want 1", info.PageCount)
	}
}

func TestPluginDescribeFormatKeepsPDFNativeFacts(t *testing.T) {
	plugin := NewPlugin(nil)
	input := `%PDF-1.4
/Info 1 0 obj << /Author (Ada) /Creator (Writer) /Producer (PDFLib) >>
1 0 obj << /Type /Page >>
`

	info, err := plugin.DescribeFormat(context.Background(), strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("DescribeFormat() error = %v", err)
	}
	if info["author"] != "Ada" || info["creator"] != "Writer" || info["producer"] != "PDFLib" {
		t.Fatalf("DescribeFormat() native metadata = %#v", info)
	}
	if _, ok := info["read_limit"]; !ok {
		t.Fatalf("DescribeFormat() missing read_limit: %#v", info)
	}
}

func TestPluginExtractRejectsNonPDF(t *testing.T) {
	plugin := NewPlugin(nil)
	_, err := plugin.DescribeFormat(context.Background(), strings.NewReader("not pdf"), nil)
	if err == nil {
		t.Fatal("DescribeFormat() error = nil, want invalid PDF error")
	}
}

func TestPluginDescribeDocumentHonorsReadLimit(t *testing.T) {
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.ExtraParams = map[string]interface{}{readLimitParam: int64(8)}
	_, err := plugin.DescribeDocument(context.Background(), strings.NewReader("%PDF-1.4\n1234567890"), opts)
	if err == nil {
		t.Fatal("DescribeDocument() error = nil, want read limit error")
	}
}
