package config

import (
	"os"
	"path/filepath"

	"github.com/addp/common/logger"
)

// LoggerOptions 用于在默认环境变量基础上覆盖日志配置。
type LoggerOptions struct {
	Level     string
	Format    string
	AddSource *bool
	File      string
}

// InitLogger 根据环境变量和可选覆盖配置初始化全局日志器。
// defaultFile 指定当 LOG_FILE 为空时使用的日志文件名，并总是定位到项目根目录下的 logs 目录。
func InitLogger(defaultFile string, override *LoggerOptions) {
	level := GetEnv("LOG_LEVEL", "info")
	format := GetEnv("LOG_FORMAT", "json")
	addSource := GetEnvBool("LOG_ADD_SOURCE", false)
	logFile := GetEnv("LOG_FILE", "")

	if override != nil {
		if override.Level != "" {
			level = override.Level
		}
		if override.Format != "" {
			format = override.Format
		}
		if override.AddSource != nil {
			addSource = *override.AddSource
		}
		if override.File != "" {
			logFile = override.File
		}
	}

	if logFile == "" {
		logFile = ResolveFromRoot("logs", defaultFile)
	} else if !filepath.IsAbs(logFile) {
		logFile = ResolveFromRoot(logFile)
	}
	_ = os.Setenv("LOG_FILE", logFile)

	logger.Init(logger.Options{
		Level:          level,
		Format:         format,
		AddSource:      addSource,
		FilePath:       logFile,
		RedirectStdLog: true,
	})
}
