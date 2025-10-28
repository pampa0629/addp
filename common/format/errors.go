package format

import "errors"

var (
	// ErrUnsupportedFormat 表示不支持的文件格式
	ErrUnsupportedFormat = errors.New("unsupported format")

	// ErrInvalidSchema 表示Schema定义无效
	ErrInvalidSchema = errors.New("invalid schema")

	// ErrFormatDetection 表示格式检测失败
	ErrFormatDetection = errors.New("format detection failed")

	// ErrInvalidMagicBytes 表示Magic Bytes不匹配
	ErrInvalidMagicBytes = errors.New("invalid magic bytes")

	// ErrEmptyFile 表示文件为空
	ErrEmptyFile = errors.New("empty file")

	// ErrCorruptedFile 表示文件损坏
	ErrCorruptedFile = errors.New("corrupted file")
)
