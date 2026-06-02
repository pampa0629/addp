package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
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
		Providers:     format.FormatProviderDescriptor{DocumentInfo: true},
		Identification: format.FormatIdentification{
			Extensions: []string{".docx"},
			MimeTypes:  []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		},
		ContentReaders: []string{
			string(format.ContentReaderDocumentText),
			string(format.ContentReaderRawContent),
			string(format.ContentReaderRangeContent),
		},
	}
}

func (p *Plugin) DescribeDocument(ctx context.Context, input io.Reader, options *format.ParseOptions) (*datatype.DocumentInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	info := &datatype.DocumentInfo{}
	for _, file := range reader.File {
		switch file.Name {
		case "docProps/core.xml":
			if err := readDocxCoreProperties(ctx, file, info); err != nil {
				return nil, err
			}
		case "docProps/app.xml":
			if err := readDocxAppProperties(ctx, file, info); err != nil {
				return nil, err
			}
		}
	}
	return info, nil
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
	var documentFile *zip.File
	headerFiles := make(map[int]*zip.File)
	footerFiles := make(map[int]*zip.File)
	var footnotesFile *zip.File
	var endnotesFile *zip.File
	var commentsFile *zip.File
	for _, file := range reader.File {
		switch {
		case file.Name == "word/document.xml":
			documentFile = file
		case isNumberedWordPart(file.Name, "word/header"):
			headerFiles[numberedWordPartNumber(file.Name, "word/header")] = file
		case isNumberedWordPart(file.Name, "word/footer"):
			footerFiles[numberedWordPartNumber(file.Name, "word/footer")] = file
		case file.Name == "word/footnotes.xml":
			footnotesFile = file
		case file.Name == "word/endnotes.xml":
			endnotesFile = file
		case file.Name == "word/comments.xml":
			commentsFile = file
		}
	}
	if documentFile == nil {
		return "", false, fmt.Errorf("docx missing word/document.xml")
	}

	var builder strings.Builder
	truncated := false
	used := int64(0)
	appendFileText := func(file *zip.File) error {
		rc, err := file.Open()
		if err != nil {
			return err
		}
		text, fileTruncated, readErr := readWordDocumentText(ctx, rc, limit-used)
		closeErr := rc.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		text = strings.TrimSpace(text)
		if text != "" {
			if builder.Len() > 0 {
				builder.WriteString("\n")
				used++
				if used >= limit {
					truncated = true
					return nil
				}
			}
			builder.WriteString(text)
			used += int64(len([]rune(text)))
		}
		if fileTruncated {
			truncated = true
		}
		return nil
	}

	if err := appendFileText(documentFile); err != nil {
		return "", false, err
	}
	for _, file := range sortedWordParts(headerFiles) {
		if truncated {
			break
		}
		if err := appendFileText(file); err != nil {
			return "", false, err
		}
	}
	for _, file := range sortedWordParts(footerFiles) {
		if truncated {
			break
		}
		if err := appendFileText(file); err != nil {
			return "", false, err
		}
	}
	for _, file := range []*zip.File{footnotesFile, endnotesFile, commentsFile} {
		if truncated || file == nil {
			continue
		}
		if err := appendFileText(file); err != nil {
			return "", false, err
		}
	}
	return strings.TrimSpace(builder.String()), truncated, nil
}

func readDocxCoreProperties(ctx context.Context, file *zip.File, info *datatype.DocumentInfo) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	decoder := xml.NewDecoder(rc)
	var current string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := token.(type) {
		case xml.StartElement:
			current = t.Name.Local
		case xml.EndElement:
			if current == t.Name.Local {
				current = ""
			}
		case xml.CharData:
			value := strings.TrimSpace(string([]byte(t)))
			if value == "" {
				continue
			}
			switch current {
			case "title":
				info.Title = value
			case "language":
				info.Language = value
			}
		}
	}
}

func readDocxAppProperties(ctx context.Context, file *zip.File, info *datatype.DocumentInfo) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	decoder := xml.NewDecoder(rc)
	var current string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := token.(type) {
		case xml.StartElement:
			current = t.Name.Local
		case xml.EndElement:
			if current == t.Name.Local {
				current = ""
			}
		case xml.CharData:
			value := strings.TrimSpace(string([]byte(t)))
			if value == "" {
				continue
			}
			switch current {
			case "Pages":
				if n, err := strconv.Atoi(value); err == nil {
					info.PageCount = n
				}
			case "Words":
				if n, err := strconv.Atoi(value); err == nil {
					info.WordCount = n
				}
			}
		}
	}
}

func readWordDocumentText(ctx context.Context, input io.Reader, limit int64) (string, bool, error) {
	decoder := xml.NewDecoder(input)
	var builder strings.Builder
	truncated := false
	used := int64(0)
	inText := false
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
			case "t":
				inText = true
			case "tab":
				appendText("\t")
			case "br":
				appendSeparator("\n")
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				appendSeparator("\n")
			}
		case xml.CharData:
			if inText {
				appendText(string([]byte(t)))
			}
		}
	}
	return strings.TrimSpace(builder.String()), truncated, nil
}

func isNumberedWordPart(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".xml") {
		return false
	}
	base := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".xml")
	if base == "" {
		return false
	}
	for _, r := range base {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func numberedWordPartNumber(name, prefix string) int {
	base := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".xml")
	number, err := strconv.Atoi(base)
	if err != nil {
		return 0
	}
	return number
}

func sortedWordParts(files map[int]*zip.File) []*zip.File {
	numbers := make([]int, 0, len(files))
	for number := range files {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	parts := make([]*zip.File, 0, len(numbers))
	for _, number := range numbers {
		parts = append(parts, files[number])
	}
	return parts
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
