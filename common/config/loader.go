package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	if err := godotenv.Load(envPath); err != nil {
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

// SharedConfig 从 System 服务获取的共享配置
type SharedConfig struct {
	JWTSecret      string `json:"jwt_secret"`
	EncryptionKey  string `json:"encryption_key"`
	InternalAPIKey string `json:"internal_api_key"`
	Database       struct {
		Host     string `json:"host"`
		Port     string `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		Name     string `json:"name"`
	} `json:"database"`
	Map struct {
		AMapKey            string `json:"amap_key"`
		AMapSecurityJsCode string `json:"amap_security_js_code"`
		TDTKey             string `json:"tdt_key"`
	} `json:"map"`
}

// BaseConfig 所有模块共享的基础配置字段
type BaseConfig struct {
	// 从 System 获取的共享配置
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	JWTSecret  string

	// 通用配置
	SystemServiceURL  string
	EnableIntegration bool
	EncryptionKey     []byte
	InternalAPIKey    string

	// 地图服务配置
	AMapKey            string
	AMapSecurityJsCode string
	TDTKey             string

	// 日志配置
	LogLevel     string
	LogFormat    string
	LogAddSource bool
	LogFile      string
}

// LoadSharedConfig 从 System 服务获取共享配置
func LoadSharedConfig(systemURL string, target *BaseConfig) error {
	url := fmt.Sprintf("%s/internal/config", systemURL)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 可选：添加内部 API Key
	if apiKey := os.Getenv("INTERNAL_API_KEY"); apiKey != "" {
		req.Header.Set("X-Internal-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to System service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	var shared SharedConfig
	if err := json.NewDecoder(resp.Body).Decode(&shared); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// 应用共享配置
	target.JWTSecret = shared.JWTSecret
	target.DBHost = shared.Database.Host
	target.DBPort = shared.Database.Port
	target.DBUser = shared.Database.User
	target.DBPassword = shared.Database.Password
	target.DBName = shared.Database.Name
	target.InternalAPIKey = shared.InternalAPIKey
	target.AMapKey = shared.Map.AMapKey
	target.AMapSecurityJsCode = shared.Map.AMapSecurityJsCode
	target.TDTKey = shared.Map.TDTKey

	// 解析加密密钥
	if shared.EncryptionKey != "" {
		key, err := base64.StdEncoding.DecodeString(shared.EncryptionKey)
		if err == nil && len(key) == 32 {
			target.EncryptionKey = key
		}
	}

	// 如果没有从 System 获取到加密密钥，使用本地加载
	if target.EncryptionKey == nil {
		target.EncryptionKey = LoadEncryptionKey()
	}

	target.LogLevel = GetEnv("LOG_LEVEL", defaultString(target.LogLevel, "info"))
	target.LogFormat = GetEnv("LOG_FORMAT", defaultString(target.LogFormat, "json"))
	target.LogAddSource = GetEnvBool("LOG_ADD_SOURCE", target.LogAddSource)
	target.LogFile = GetEnv("LOG_FILE", target.LogFile)

	return nil
}

// LoadLocalConfig 从本地环境变量加载配置（降级方案）
func LoadLocalConfig(target *BaseConfig) {
	target.JWTSecret = GetEnv("JWT_SECRET", "")
	target.DBHost = GetEnv("DB_HOST", "localhost")
	target.DBPort = GetEnv("DB_PORT", "5432")
	target.DBUser = GetEnv("DB_USER", "addp")
	target.DBPassword = GetEnv("DB_PASSWORD", "addp_password")
	target.DBName = GetEnv("DB_NAME", "addp")
	target.EncryptionKey = LoadEncryptionKey()
	target.InternalAPIKey = GetEnv("INTERNAL_API_KEY", "")
	target.AMapKey = GetEnv("AMAP_KEY", "")
	target.AMapSecurityJsCode = GetEnv("AMAP_SECURITY_KEY", "")
	target.TDTKey = GetEnv("TDT_KEY", "")

	target.LogLevel = GetEnv("LOG_LEVEL", "info")
	target.LogFormat = GetEnv("LOG_FORMAT", "json")
	target.LogAddSource = GetEnvBool("LOG_ADD_SOURCE", false)
	target.LogFile = GetEnv("LOG_FILE", "")

	if target.JWTSecret == "" {
		logger.L().Warn("JWT_SECRET 未设置，认证将失败")
	}
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
		// 使用固定的32字节密钥作为开发默认值
		return []byte("dev-encryption-key-32-bytes!") // 正好32字节
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
