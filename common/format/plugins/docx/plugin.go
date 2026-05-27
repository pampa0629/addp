package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const defaultDocumentTextLimit int64 = 512 * 1024

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatDOCX
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:            "builtin-docx",
		Format:        p.Format(),
		I18nKey:       "format.docx",
		DataType:      datatype.DataTypeDocument,
		Layouts:       []string{format.LayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument},
		Identification: format.FormatIdentification{
			Extensions: []string{".docx"},
			MimeTypes:  []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		},
		ContentReaders: []string{
			string(format.ContentReaderDocumentText),
			string(format.ContentReaderRawContent),
			string(format.ContentReaderRangeContent),
		},
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile, format.EngineFamilyDocument},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.Format())
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        p.Format(),
		DataType:      datatype.DataTypeDocument,
		Layouts:       []string{format.LayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument},
		ContentReaders: []string{
			string(format.ContentReaderDocumentText),
			string(format.ContentReaderRawContent),
			string(format.ContentReaderRangeContent),
		},
	}
}

func (p *Plugin) ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *format.ParseOptions) (string, bool, error) {
	if limit <= 0 {
		limit = defaultDocumentTextLimit
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	data, err := io.ReadAll(input)
	if err != nil {
		return "", false, err
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", false, err
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return "", false, err
		}
		text, truncated, readErr := readWordDocumentText(ctx, rc, limit)
		closeErr := rc.Close()
		if readErr != nil {
			return "", false, readErr
		}
		if closeErr != nil {
			return "", false, closeErr
		}
		return text, truncated, nil
	}
	return "", false, fmt.Errorf("docx missing word/document.xml")
}

func readWordDocumentText(ctx context.Context, input io.Reader, limit int64) (string, bool, error) {
	decoder := xml.NewDecoder(input)
	var builder strings.Builder
	truncated := false
	used := int64(0)
	appendText := func(value string) {
		if truncated || value == "" {
			return
		}
		for _, r := range value {
			if used >= limit {
				truncated = true
				return
			}
			builder.WriteRune(r)
			used++
		}
	}
	appendSeparator := func(value string) {
		if truncated || builder.Len() == 0 {
			return
		}
		appendText(value)
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tab":
				appendText("\t")
			case "br":
				appendSeparator("\n")
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				appendSeparator("\n")
			}
		case xml.CharData:
			appendText(string([]byte(t)))
		}
	}
	return strings.TrimSpace(builder.String()), truncated, nil
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
