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
	if info.Format != format.FormatPDF {
		t.Fatalf("document format = %q, want pdf", info.Format)
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
