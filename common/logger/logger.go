package logger

import (
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

// Options 定义日志初始化选项
type Options struct {
	Level     string
	AddSource bool
	Writer    io.Writer
	// RedirectStdLog 将标准库 log 输出连接到同一输出流。
	RedirectStdLog bool
}

var (
	defaultLogger *slog.Logger
	updateMu      sync.RWMutex
)

func init() {
	// Logging output can lose its receiver independently of business health.
	// Go otherwise terminates on a broken stdout/stderr pipe. Slog can ignore
	// the resulting write error while the service continues serving requests.
	signal.Ignore(syscall.SIGPIPE)
	initDefaultLogger()
}

func initDefaultLogger() {
	// 默认JSON格式、INFO级别
	opts := Options{
		Level: "info",
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

	handler := slog.NewJSONHandler(writer, handlerOpts)

	return slog.New(handler)
}

// Init 初始化全局日志器，需在服务启动阶段调用
func Init(opts Options) {
	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}

	logger := buildLogger(opts, writer)
	if opts.RedirectStdLog && writer != nil {
		log.SetOutput(writer)
		log.SetFlags(0)
		log.SetPrefix("")
	}
	updateMu.Lock()
	defaultLogger = logger
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
