package extractors

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/meta/internal/scanner"
)

// DefaultExtractor 默认的元数据提取器（兜底）
// 当没有专门的提取器时使用，只提取基本文件信息
type DefaultExtractor struct{}

func (e *DefaultExtractor) SupportedTypes() []string {
	return []string{
		"*/*", // 通配符，匹配所有类型
	}
}

func (e *DefaultExtractor) Priority() int {
	return -100 // 最低优先级（兜底）
}

func (e *DefaultExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
	// 1. 计算文件校验和（可选，对于大文件可能耗时）
	checksum := ""
	if input.Size < 100*1024*1024 { // 只对小于100MB的文件计算MD5
		if input.Reader != nil {
			checksum = calculateMD5(input.Reader)
		}
	}

	// 2. 推断文件类型
	fileType := inferFileType(input.ObjectKey, input.ContentType)

	// 3. 构建基础元数据
	metadata := &scanner.Metadata{
		BasicInfo: scanner.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     fileType,
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			Checksum:     checksum,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 4. 添加文件扩展名信息
	ext := strings.ToLower(filepath.Ext(input.ObjectKey))
	if ext != "" {
		metadata.CustomAttrs["file_extension"] = ext
		metadata.CustomAttrs["file_category"] = categorizeByExtension(ext)
	}

	// 5. 添加路径信息
	dir := filepath.Dir(input.ObjectKey)
	if dir != "" && dir != "." {
		metadata.CustomAttrs["directory"] = dir
		metadata.CustomAttrs["depth"] = strings.Count(input.ObjectKey, "/")
	}

	// 6. 文件大小分类
	metadata.CustomAttrs["size_category"] = categorizeSizeBySize(input.Size)

	return metadata, nil
}

// calculateMD5 计算MD5校验和
func calculateMD5(reader io.Reader) string {
	hash := md5.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// inferFileType 根据文件名和MIME类型推断文件类型
func inferFileType(filename, contentType string) string {
	// 1. 优先根据MIME类型判断
	if contentType != "" && contentType != "application/octet-stream" {
		parts := strings.Split(contentType, "/")
		if len(parts) == 2 {
			mainType := parts[0]
			subType := parts[1]

			switch mainType {
			case "text":
				return "Text"
			case "image":
				return "Image"
			case "audio":
				return "Audio"
			case "video":
				return "Video"
			case "application":
				// 具体化application类型
				switch subType {
				case "json":
					return "JSON"
				case "xml":
					return "XML"
				case "pdf":
					return "PDF"
				case "zip":
					return "Archive (ZIP)"
				default:
					return "Application"
				}
			}
		}
	}

	// 2. 根据文件扩展名判断
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" {
		fileTypeMap := map[string]string{
			".txt":  "Text",
			".md":   "Markdown",
			".json": "JSON",
			".xml":  "XML",
			".yaml": "YAML",
			".yml":  "YAML",
			".csv":  "CSV",
			".tsv":  "TSV",
			".log":  "Log",
			".sql":  "SQL",
			".sh":   "Shell Script",
			".py":   "Python Script",
			".js":   "JavaScript",
			".go":   "Go Source",
			".java": "Java Source",
			".c":    "C Source",
			".cpp":  "C++ Source",
			".h":    "Header File",
			".zip":  "Archive (ZIP)",
			".tar":  "Archive (TAR)",
			".gz":   "Archive (GZIP)",
			".7z":   "Archive (7Z)",
			".rar":  "Archive (RAR)",
		}

		if fileType, ok := fileTypeMap[ext]; ok {
			return fileType
		}

		// 返回扩展名（去掉点号）
		return strings.ToUpper(strings.TrimPrefix(ext, "."))
	}

	return "Unknown"
}

// categorizeByExtension 根据扩展名分类文件
func categorizeByExtension(ext string) string {
	ext = strings.ToLower(ext)

	categoryMap := map[string][]string{
		"document": {".doc", ".docx", ".pdf", ".txt", ".md", ".rtf", ".odt"},
		"spreadsheet": {".xls", ".xlsx", ".csv", ".tsv", ".ods"},
		"presentation": {".ppt", ".pptx", ".odp"},
		"image": {".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg", ".webp", ".tiff", ".ico"},
		"video": {".mp4", ".avi", ".mov", ".mkv", ".flv", ".wmv", ".webm"},
		"audio": {".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a", ".wma"},
		"archive": {".zip", ".tar", ".gz", ".7z", ".rar", ".bz2", ".xz"},
		"code": {".py", ".js", ".go", ".java", ".c", ".cpp", ".h", ".sh", ".rb", ".php"},
		"data": {".json", ".xml", ".yaml", ".yml", ".toml", ".ini", ".conf"},
		"database": {".db", ".sqlite", ".sql", ".mdb"},
		"geospatial": {".shp", ".geojson", ".kml", ".kmz", ".gpx", ".gml"},
	}

	for category, extensions := range categoryMap {
		for _, e := range extensions {
			if ext == e {
				return category
			}
		}
	}

	return "other"
}

// categorizeSizeBySize 根据文件大小分类
func categorizeSizeBySize(size int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case size < KB:
		return "tiny" // < 1KB
	case size < 100*KB:
		return "small" // 1KB - 100KB
	case size < 10*MB:
		return "medium" // 100KB - 10MB
	case size < 100*MB:
		return "large" // 10MB - 100MB
	case size < GB:
		return "very_large" // 100MB - 1GB
	default:
		return "huge" // > 1GB
	}
}
