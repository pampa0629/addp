package logger

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Options 定义日志初始化选项
type Options struct {
	Level     string
	Format    string
	AddSource bool
	Writer    io.Writer
	FilePath  string
	// RedirectStdLog 表示是否将标准库 log 输出重定向到同一 writer，便于兼容 legacy 代码。
	RedirectStdLog bool
}

var (
	defaultLogger *slog.Logger
	updateMu      sync.RWMutex
	currentCloser io.Closer
)

func init() {
	initDefaultLogger()
}

func initDefaultLogger() {
	// 默认JSON格式、INFO级别
	opts := Options{
		Level:  "info",
		Format: "json",
	}
	defaultLogger = buildLogger(opts, os.Stdout)
}

// buildLogger 根据选项生成 slog.Logger
func buildLogger(opts Options, writer io.Writer) *slog.Logger {
	handlerOpts := &slog.HandlerOptions{
		Level:     parseLevel(opts.Level),
		AddSource: opts.AddSource,
	}

	if writer == nil {
		writer = os.Stdout
	}

	var handler slog.Handler
	switch strings.ToLower(opts.Format) {
	case "text", "console", "plain":
		handler = slog.NewTextHandler(writer, handlerOpts)
	default:
		handler = slog.NewJSONHandler(writer, handlerOpts)
	}

	return slog.New(handler)
}

// Init 初始化全局日志器，需在服务启动阶段调用
func Init(opts Options) {
	writer, closer := resolveWriter(opts)

	logger := buildLogger(opts, writer)
	if opts.RedirectStdLog && writer != nil {
		log.SetOutput(writer)
		log.SetFlags(0)
		log.SetPrefix("")
	}
	updateMu.Lock()
	if currentCloser != nil && currentCloser != closer {
		_ = currentCloser.Close()
	}
	defaultLogger = logger
	currentCloser = closer
	updateMu.Unlock()
}

// L 返回当前全局日志器
func L() *slog.Logger {
	updateMu.RLock()
	logger := defaultLogger
	updateMu.RUnlock()
	return logger
}

// With 返回带有额外上下文字段的日志器
func With(args ...any) *slog.Logger {
	return L().With(args...)
}

func parseLevel(level string) slog.Leveler {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info":
		fallthrough
	default:
		return slog.LevelInfo
	}
}

func resolveWriter(opts Options) (io.Writer, io.Closer) {
	if opts.Writer != nil {
		return opts.Writer, nil
	}

	if opts.FilePath != "" {
		writer, err := newReopenableFileWriter(opts.FilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "logger: failed to open log file %s: %v\n", opts.FilePath, err)
		} else {
			return writer, writer
		}
	}

	return os.Stdout, nil
}

type reopenableFileWriter struct {
	path string
	mu   sync.Mutex
	file *os.File
}

func newReopenableFileWriter(path string) (*reopenableFileWriter, error) {
	w := &reopenableFileWriter{path: path}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *reopenableFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.ensureFile(); err != nil {
		return 0, err
	}

	n, err := w.file.Write(p)
	if err == nil {
		return n, nil
	}

	// 尝试重新打开一次
	if reopenErr := w.open(); reopenErr != nil {
		return n, err
	}
	return w.file.Write(p)
}

func (w *reopenableFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

func (w *reopenableFileWriter) ensureFile() error {
	if w.file == nil {
		return w.open()
	}

	currentInfo, err := w.file.Stat()
	if err != nil {
		return w.open()
	}

	pathInfo, err := os.Stat(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return w.open()
		}
		return err
	}

	if !sameFile(currentInfo, pathInfo) {
		return w.open()
	}

	return nil
}

func (w *reopenableFileWriter) open() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}

	dir := filepath.Dir(w.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.file = file
	return nil
}

func sameFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b)
}
