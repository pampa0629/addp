package sqlite

import (
	"fmt"
	"io"
	"os"

	"github.com/addp/common/format"
	_ "github.com/mattn/go-sqlite3"
)

// Parser 实现 SQLite 格式的解析器
type Parser struct {
	options *format.ParseOptions
}

// NewParser 创建 SQLite 解析器
func NewParser(opts *format.ParseOptions) *Parser {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Parser{options: opts}
}

// SupportedFormats 返回支持的格式
func (p *Parser) SupportedFormats() []format.FormatType {
	return []format.FormatType{format.FormatSQLite}
}

// mapSQLiteTypeToFieldType 将 SQLite 类型映射到 FieldType
func mapSQLiteTypeToFieldType(sqliteType string) format.FieldType {
	// SQLite 类型不区分大小写
	upperType := ""
	for _, r := range sqliteType {
		if r >= 'a' && r <= 'z' {
			upperType += string(r - 32)
		} else {
			upperType += string(r)
		}
	}

	// SQLite 类型亲和性规则
	switch {
	case contains(upperType, "INT"):
		return format.FieldTypeInt
	case contains(upperType, "CHAR") || contains(upperType, "CLOB") || contains(upperType, "TEXT"):
		return format.FieldTypeString
	case contains(upperType, "BLOB"):
		return format.FieldTypeBytes
	case contains(upperType, "REAL") || contains(upperType, "FLOA") || contains(upperType, "DOUB"):
		return format.FieldTypeFloat
	case contains(upperType, "DATE") || contains(upperType, "TIME"):
		return format.FieldTypeTimestamp
	case contains(upperType, "BOOL"):
		return format.FieldTypeBool
	default:
		return format.FieldTypeString
	}
}

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

// indexOf 查找子串位置
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// saveToTempFile 将 io.Reader 保存到临时文件
func (p *Parser) saveToTempFile(input io.Reader) (string, func(), error) {
	// 创建临时文件
	tempFile, err := os.CreateTemp("", "sqlite-*.db")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// 写入数据
	if _, err := io.Copy(tempFile, input); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return "", nil, fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		os.Remove(tempPath)
		return "", nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// 返回清理函数
	cleanup := func() {
		os.Remove(tempPath)
	}

	return tempPath, cleanup, nil
}

func init() {
	// TODO: SQLite parser 需要实现 FileTableParser 接口（ParseTableInfo、ReadPreview 方法）
	// 暂时不注册，等待实现新接口
	// parser := NewParser(nil)
	// _ = format.RegisterFileTableParser(parser)
}
