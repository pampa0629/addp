package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/addp/common/logger"
	"github.com/joho/godotenv"
)

var (
	projectRoot   string
	projectRootMu sync.RWMutex
)

// LoadEnv 在项目根目录加载统一的 .env 文件，并在成功后设置 PROJECT_ROOT。
func LoadEnv() {
	root := discoverProjectRoot()
	if root != "" {
		setProjectRoot(root)
	}

	envPath := ".env"
	if root := ProjectRoot(); root != "" {
		envPath = filepath.Join(root, ".env")
	}

	// 使用 Overload 强制覆盖环境变量（开发模式需要 .env 文件优先）
	if err := godotenv.Overload(envPath); err != nil {
		logger.L().Warn("环境变量文件加载失败，使用系统环境变量", "path", envPath, "error", err)
	} else {
		logger.L().Info("已加载 .env 配置", "path", envPath)
	}

	if envRoot := os.Getenv("PROJECT_ROOT"); envRoot != "" {
		setProjectRoot(envRoot)
	} else if root := ProjectRoot(); root != "" {
		_ = os.Setenv("PROJECT_ROOT", root)
	}
}

func discoverProjectRoot() string {
	if envRoot := os.Getenv("PROJECT_ROOT"); envRoot != "" {
		if abs, err := filepath.Abs(envRoot); err == nil {
			return abs
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	dir := cwd
	for {
		candidate := filepath.Join(dir, ".env")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ""
}

// BaseConfig 所有模块共享的基础配置字段
type BaseConfig struct {
	// 部署数据库配置
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string

	// 通用配置
	SystemServiceURL    string
	InferenceRuntimeURL string
	EncryptionKey       []byte

	// 日志配置
	LogLevel     string
	LogFormat    string
	LogAddSource bool
	LogFile      string
}

// LoadDeploymentConfig loads process bootstrap configuration from the root env.
func LoadDeploymentConfig(target *BaseConfig) {
	target.DBHost = GetEnv("POSTGRES_HOST", "localhost")
	target.DBPort = GetEnv("POSTGRES_PORT", "15432")
	target.DBUser = GetEnv("POSTGRES_USER", "addp")
	target.DBPassword = GetEnv("POSTGRES_PASSWORD", "addp_password")
	target.DBName = GetEnv("POSTGRES_DB", "addp")

	target.EncryptionKey = LoadEncryptionKey()
	target.InferenceRuntimeURL = GetEnv("INFERENCE_URL", "http://localhost:8191")
	target.LogLevel = GetEnv("LOG_LEVEL", "info")
	target.LogFormat = GetEnv("LOG_FORMAT", "json")
	target.LogAddSource = GetEnvBool("LOG_ADD_SOURCE", false)
	target.LogFile = GetEnv("LOG_FILE", "")

}

func setProjectRoot(candidate string) {
	if candidate == "" {
		return
	}

	abs, err := filepath.Abs(candidate)
	if err != nil {
		return
	}

	projectRootMu.Lock()
	if projectRoot == "" {
		projectRoot = abs
	}
	projectRootMu.Unlock()

	_ = os.Setenv("PROJECT_ROOT", abs)
}

// ProjectRoot 返回项目根目录的绝对路径（如果已知）
func ProjectRoot() string {
	projectRootMu.RLock()
	root := projectRoot
	projectRootMu.RUnlock()

	if root != "" {
		return root
	}

	if envRoot := os.Getenv("PROJECT_ROOT"); envRoot != "" {
		if abs, err := filepath.Abs(envRoot); err == nil {
			return abs
		}
	}

	return ""
}

// ResolveFromRoot 基于仓库根目录构建路径（根目录未知时退化为 Join）
func ResolveFromRoot(parts ...string) string {
	root := ProjectRoot()
	if root == "" {
		return filepath.Join(parts...)
	}
	all := append([]string{root}, parts...)
	return filepath.Join(all...)
}

// LoadEncryptionKey 加载加密密钥 (32字节 AES-256)
func LoadEncryptionKey() []byte {
	keyStr := os.Getenv("ENCRYPTION_KEY")
	if keyStr == "" {
		// 开发环境使用默认密钥 (生产环境必须设置!)
		logger.L().Warn("ENCRYPTION_KEY 未设置，使用开发默认值（生产环境请务必设置）")
		// 与 System 开发环境使用同一个固定 32 字节密钥。
		return []byte("addp-dev-encryption-key-2025!!!!")
	}

	// 从 Base64 解码密钥
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		logger.L().Error("ENCRYPTION_KEY Base64 解码失败", "error", err)
		os.Exit(1)
	}

	if len(key) != 32 {
		logger.L().Error("ENCRYPTION_KEY 长度非法，必须为32字节", "length", len(key))
		os.Exit(1)
	}

	return key
}

// GetEnv 获取环境变量，如果不存在则返回默认值
func GetEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// GetEnvInt 获取整数类型的环境变量
func GetEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var intValue int
	if _, err := fmt.Sscanf(value, "%d", &intValue); err != nil {
		logger.L().Warn("整数环境变量解析失败，使用默认值", "key", key, "value", value, "default", defaultValue)
		return defaultValue
	}
	return intValue
}

// GetEnvDuration 获取时间duration类型的环境变量
func GetEnvDuration(key string, defaultValue string) time.Duration {
	value := GetEnv(key, defaultValue)
	duration, err := time.ParseDuration(value)
	if err != nil {
		logger.L().Warn("时长环境变量解析失败，使用默认值", "key", key, "value", value, "default", defaultValue)
		duration, _ = time.ParseDuration(defaultValue)
	}
	return duration
}

// GetEnvBool 获取布尔类型的环境变量
func GetEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1" || value == "yes"
}
