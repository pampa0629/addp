package config

import (
	"github.com/addp/common/logger"
	"os"
)

// RuntimeLogDeploymentState is deployment intent, independent of backend health.
// Invalid optional configuration disables evaluation, never business readiness.
func RuntimeLogDeploymentState() string {
	value, configured := os.LookupEnv("ADDP_OBSERVABILITY_LOGS_ENABLED")
	if !configured {
		value = "true"
	}
	switch value {
	case "true":
		return "enabled"
	case "false":
		return "disabled"
	default:
		return "unconfigured"
	}
}

// LoggerOptions 用于在默认环境变量基础上覆盖日志配置。
type LoggerOptions struct {
	Level     string
	AddSource *bool
}

// InitLogger 根据环境变量和可选覆盖配置初始化全局日志器。
// 文件保存和实例关联统一由标准启动边界负责。
func InitLogger(override *LoggerOptions) {
	level := GetEnv("LOG_LEVEL", "info")
	addSource := GetEnvBool("LOG_ADD_SOURCE", false)

	if override != nil {
		if override.Level != "" {
			level = override.Level
		}
		if override.AddSource != nil {
			addSource = *override.AddSource
		}
	}

	logger.Init(logger.Options{
		Level:          level,
		AddSource:      addSource,
		RedirectStdLog: true,
	})
}
