package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"

	sdk "github.com/addp/meta-extractor-sdk"
	officeextractor "github.com/addp/plugins/office-extractor"
)

const (
	defaultDocIndex   = "asset-documents"
	docIndexMapping   = `{
  "settings": {
    "analysis": {
      "analyzer": {
        "zh_text": {
          "tokenizer": "standard",
          "filter": ["lowercase"]
        }
      }
    }
  },
  "mappings": {
    "properties": {
      "document_id": { "type": "keyword" },
      "asset_type": { "type": "keyword" },
      "source": { "type": "keyword" },
      "document_type": { "type": "keyword" },
      "title": { "type": "text", "fields": { "keyword": { "type": "keyword", "ignore_above": 256 } } },
      "author": { "type": "keyword" },
      "keywords": { "type": "keyword" },
      "file_name": { "type": "text", "fields": { "keyword": { "type": "keyword", "ignore_above": 256 } } },
      "file_path": { "type": "keyword" },
      "file_type": { "type": "keyword" },
      "content_type": { "type": "keyword" },
      "file_size": { "type": "long" },
      "word_count": { "type": "integer" },
      "page_count": { "type": "integer" },
      "last_modified": { "type": "date" },
      "created_date": { "type": "date" },
      "modified_date": { "type": "date" },
      "checksum_sha256": { "type": "keyword" },
      "ingested_at": { "type": "date" },
      "metadata": { "type": "object" },
      "content": { "type": "text", "analyzer": "zh_text" },
      "content_length": { "type": "integer" },
      "content_preview": { "type": "text", "analyzer": "zh_text" }
    }
  }
}`
)

const (
	docxContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	previewRuneLimit = 400
)

func main() {
	filePath := flag.String("file", "", "要入库的 DOCX 文件路径")
	indexName := flag.String("index", envOrDefault("ASSET_DOCUMENT_INDEX", defaultDocIndex), "Elasticsearch 索引名称")
	esURL := flag.String("es", detectESURL(), "Elasticsearch 地址（默认读取环境变量 ELASTICSEARCH_URL_LOCAL/ELASTICSEARCH_URL）")
	flag.Parse()

	if *filePath == "" {
		fmt.Fprintln(os.Stderr, "缺少 --file 参数：请提供要导入的 DOCX 文件路径")
		os.Exit(1)
	}

	content, stat, err := readFile(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取文件失败: %v\n", err)
		os.Exit(1)
	}

	extractor := &officeextractor.OfficeExtractor{}
	meta, err := extractor.Extract(context.Background(), sdk.ExtractInput{
		ObjectKey:    filepath.Base(*filePath),
		ContentType:  docxContentType,
		Size:         int64(len(content)),
		Reader:       bytes.NewReader(content),
		LastModified: stat.ModTime(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "提取 DOCX 元数据失败: %v\n", err)
		os.Exit(1)
	}

	docText, err := extractDocxText(content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 DOCX 正文失败: %v\n", err)
		os.Exit(1)
	}
	docText = strings.TrimSpace(docText)

	record, docID := buildDocumentRecord(*filePath, content, meta, docText)

	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{*esURL},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化 Elasticsearch 客户端失败: %v\n", err)
		os.Exit(1)
	}

	if err := ensureIndex(client, *indexName); err != nil {
		fmt.Fprintf(os.Stderr, "创建/检查索引失败: %v\n", err)
		os.Exit(1)
	}

	if err := indexDocument(client, *indexName, docID, record); err != nil {
		fmt.Fprintf(os.Stderr, "写入 Elasticsearch 失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ 文档已入库：index=%s, id=%s, 大小=%d 字节, 字数=%d\n", *indexName, docID, record["file_size"], record["content_length"])
}

func readFile(path string) ([]byte, os.FileInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	return data, info, nil
}

func extractDocxText(content []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", fmt.Errorf("无法以 ZIP 读取 DOCX: %w", err)
	}

	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("打开文档 XML 失败: %w", err)
		}
		defer rc.Close()

		var builder strings.Builder
		if err := parseDocumentXML(rc, &builder); err != nil {
			return "", err
		}
		return builder.String(), nil
	}

	return "", fmt.Errorf("DOCX 内未找到 word/document.xml")
}

func parseDocumentXML(r io.Reader, builder *strings.Builder) error {
	decoder := xml.NewDecoder(r)
	var inText bool

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("解析 DOCX XML 失败: %w", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				builder.WriteRune('\t')
			case "br":
				builder.WriteRune('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				builder.WriteRune('\n')
			}
		case xml.CharData:
			if inText {
				builder.WriteString(strings.ReplaceAll(string(t), "\r", ""))
			}
		}
	}
	return nil
}

func buildDocumentRecord(path string, content []byte, metadata *sdk.Metadata, text string) (map[string]interface{}, string) {
	absPath, _ := filepath.Abs(path)
	checksum := sha256Hex(content)

	documentType := asString(metadata.CustomAttrs["document_type"])
	title := asString(metadata.CustomAttrs["title"])
	author := asString(metadata.CustomAttrs["author"])

	officeMeta := extractOfficeMetadata(metadata)

	keywords := asStringSlice(officeMeta["keywords"])
	createdDate := formatTimeValue(officeMeta["created_date"])
	modifiedDate := formatTimeValue(officeMeta["modified_date"])
	wordCount := asInt(officeMeta["word_count"])
	pageCount := asInt(officeMeta["page_count"])

	if officeMeta != nil {
		officeMeta = normalizeMetadata(officeMeta)
	}

	record := map[string]interface{}{
		"document_id":      checksum,
		"asset_type":       "object_document",
		"source":           "local-file",
		"document_type":    documentType,
		"title":            title,
		"author":           author,
		"keywords":         keywords,
		"file_name":        metadata.BasicInfo.FileName,
		"file_path":        absPath,
		"file_type":        metadata.BasicInfo.FileType,
		"content_type":     metadata.BasicInfo.ContentType,
		"file_size":        metadata.BasicInfo.Size,
		"checksum_sha256":  checksum,
		"ingested_at":      time.Now().UTC().Format(time.RFC3339),
		"metadata":         officeMeta,
		"content":          text,
		"content_length":   utf8.RuneCountInString(text),
		"content_preview":  buildContentPreview(text),
	}

	if len(keywords) > 0 {
		record["keywords"] = keywords
	}
	if createdDate != "" {
		record["created_date"] = createdDate
	}
	if modifiedDate != "" {
		record["modified_date"] = modifiedDate
	}
	if wordCount > 0 {
		record["word_count"] = wordCount
	}
	if pageCount > 0 {
		record["page_count"] = pageCount
	}

	if !metadata.BasicInfo.LastModified.IsZero() {
		record["last_modified"] = metadata.BasicInfo.LastModified.UTC().Format(time.RFC3339)
	}

	return record, checksum
}

func buildContentPreview(text string) string {
	runes := []rune(text)
	if len(runes) <= previewRuneLimit {
		return text
	}
	return string(runes[:previewRuneLimit])
}

func ensureIndex(client *elasticsearch.Client, indexName string) error {
	res, err := client.Indices.Exists([]string{indexName})
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == 404 {
		createRes, err := client.Indices.Create(indexName, client.Indices.Create.WithBody(strings.NewReader(docIndexMapping)))
		if err != nil {
			return err
		}
		defer createRes.Body.Close()
		if createRes.IsError() {
			return fmt.Errorf("创建索引失败: %s", createRes.String())
		}
	}
	return nil
}

func indexDocument(client *elasticsearch.Client, indexName, docID string, record map[string]interface{}) error {
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}

	req := esapi.IndexRequest{
		Index:      indexName,
		DocumentID: docID,
		Body:       bytes.NewReader(body),
		Refresh:    "true",
	}

	res, err := req.Do(context.Background(), client)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("索引文档失败: %s", res.String())
	}

	return nil
}

func extractOfficeMetadata(meta *sdk.Metadata) map[string]interface{} {
	if meta == nil || meta.CustomAttrs == nil {
		return nil
	}

	raw, ok := meta.CustomAttrs["office_metadata"]
	if !ok {
		return make(map[string]interface{})
	}

	asMap, ok := raw.(map[string]interface{})
	if !ok {
		return make(map[string]interface{})
	}

	data, ok := asMap["data"].(map[string]interface{})
	if !ok {
		return make(map[string]interface{})
	}

	return data
}

func normalizeMetadata(meta map[string]interface{}) map[string]interface{} {
	if meta == nil {
		return nil
	}
	result := make(map[string]interface{}, len(meta))
	for k, v := range meta {
		switch val := v.(type) {
		case time.Time:
			if val.IsZero() {
				continue
			}
			result[k] = val.UTC().Format(time.RFC3339)
		case []time.Time:
			arr := make([]string, 0, len(val))
			for _, t := range val {
				if t.IsZero() {
					continue
				}
				arr = append(arr, t.UTC().Format(time.RFC3339))
			}
			result[k] = arr
		default:
			result[k] = v
		}
	}
	return result
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func formatTimeValue(value interface{}) string {
	switch t := value.(type) {
	case time.Time:
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	case string:
		return t
	default:
		return ""
	}
}

func asString(value interface{}) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}

func asStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	default:
		if s, ok := value.(string); ok && s != "" {
			return []string{s}
		}
	}
	return nil
}

func asInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func detectESURL() string {
	if v := strings.TrimSpace(os.Getenv("ELASTICSEARCH_URL_LOCAL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ELASTICSEARCH_URL")); v != "" {
		return v
	}
	return "http://localhost:9200"
}
