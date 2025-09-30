package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Env                string
	ServerAddr         string
	DatabaseURL        string
	JWTSecret          string
	TokenExpireMinutes int
	ProjectName        string
}

func Load() *Config {
	dbPath := getEnv("DATABASE_URL", "./data/system.db")
	// 确保数据库目录存在
	if !filepath.IsAbs(dbPath) {
		absPath, _ := filepath.Abs(dbPath)
		dbPath = absPath
	}
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	return &Config{
		Env:                getEnv("ENV", "development"),
		ServerAddr:         getEnv("SERVER_ADDR", ":8080"),
		DatabaseURL:        dbPath,
		JWTSecret:          getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
		TokenExpireMinutes: 30,
		ProjectName:        getEnv("PROJECT_NAME", "全域数据平台"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}