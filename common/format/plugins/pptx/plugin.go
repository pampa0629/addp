package pptx

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
	return format.FormatPPTX
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:            "builtin-pptx",
		Format:        p.Format(),
		I18nKey:       "format.pptx",
		DataType:      datatype.DataTypeDocument,
		Layouts:       []string{format.LayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument},
		Providers:     format.FormatProviderDescriptor{DocumentInfo: true},
		Identification: format.FormatIdentification{
			Extensions: []string{".pptx"},
			MimeTypes:  []string{"application/vnd.openxmlformats-officedocument.presentationml.presentation"},
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
			if err := readPptxCoreProperties(ctx, file, info); err != nil {
				return nil, err
			}
		case "docProps/app.xml":
			if err := readPptxAppProperties(ctx, file, info); err != nil {
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
	slideFiles := make(map[int]*zip.File)
	noteFiles := make(map[int]*zip.File)
	commentFiles := make([]*zip.File, 0)
	for _, file := range reader.File {
		if isSlideXML(file.Name) {
			slideFiles[numberedPartNumber(file.Name, "ppt/slides/slide")] = file
			continue
		}
		if isNotesSlideXML(file.Name) {
			noteFiles[numberedPartNumber(file.Name, "ppt/notesSlides/notesSlide")] = file
			continue
		}
		if isCommentXML(file.Name) {
			commentFiles = append(commentFiles, file)
		}
	}
	if len(slideFiles) == 0 {
		return "", false, fmt.Errorf("pptx missing ppt/slides/slide*.xml")
	}
	sort.Slice(commentFiles, func(i, j int) bool {
		return numberedPartNumber(commentFiles[i].Name, "ppt/comments/comment") < numberedPartNumber(commentFiles[j].Name, "ppt/comments/comment")
	})

	var builder strings.Builder
	truncated := false
	used := int64(0)
	appendFileText := func(file *zip.File) error {
		rc, err := file.Open()
		if err != nil {
			return err
		}
		text, slideTruncated, readErr := readSlideText(ctx, rc, limit-used)
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
		if slideTruncated {
			truncated = true
		}
		return nil
	}

	for _, number := range sortedPartNumbers(slideFiles) {
		if truncated {
			break
		}
		if err := appendFileText(slideFiles[number]); err != nil {
			return "", false, err
		}
		if truncated {
			break
		}
		if noteFile := noteFiles[number]; noteFile != nil {
			if err := appendFileText(noteFile); err != nil {
				return "", false, err
			}
		}
	}
	for _, number := range sortedPartNumbers(noteFiles) {
		if truncated {
			break
		}
		if _, ok := slideFiles[number]; ok {
			continue
		}
		if err := appendFileText(noteFiles[number]); err != nil {
			return "", false, err
		}
	}
	for _, file := range commentFiles {
		if truncated {
			break
		}
		if err := appendFileText(file); err != nil {
			return "", false, err
		}
	}
	return strings.TrimSpace(builder.String()), truncated, nil
}

func readPptxCoreProperties(ctx context.Context, file *zip.File, info *datatype.DocumentInfo) error {
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

func readPptxAppProperties(ctx context.Context, file *zip.File, info *datatype.DocumentInfo) error {
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
			case "Slides":
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

func isSlideXML(name string) bool {
	return isNumberedXMLPart(name, "ppt/slides/slide")
}

func isNotesSlideXML(name string) bool {
	return isNumberedXMLPart(name, "ppt/notesSlides/notesSlide")
}

func isCommentXML(name string) bool {
	return isNumberedXMLPart(name, "ppt/comments/comment")
}

func slideNumber(name string) int {
	return numberedPartNumber(name, "ppt/slides/slide")
}

func isNumberedXMLPart(name, prefix string) bool {
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

func numberedPartNumber(name, prefix string) int {
	base := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".xml")
	number, err := strconv.Atoi(base)
	if err != nil {
		return 0
	}
	return number
}

func sortedPartNumbers(files map[int]*zip.File) []int {
	numbers := make([]int, 0, len(files))
	for number := range files {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	return numbers
}

func readSlideText(ctx context.Context, input io.Reader, limit int64) (string, bool, error) {
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
	appendSeparator := func() {
		if truncated || builder.Len() == 0 {
			return
		}
		appendText("\n")
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
			if t.Name.Local == "t" || t.Name.Local == "text" {
				inText = true
			}
			if t.Name.Local == "br" {
				appendSeparator()
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t", "text":
				inText = false
			case "p":
				appendSeparator()
			}
		case xml.CharData:
			if inText {
				appendText(string([]byte(t)))
			}
		}
	}
	return strings.TrimSpace(builder.String()), truncated, nil
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
