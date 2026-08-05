package config

import (
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
)

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// LoadRedisConfig loads Redis configuration from environment variables
// Uses REDIS_HOST + REDIS_PORT format for consistency with other configs (PostgreSQL, etc.)
func LoadRedisConfig() RedisConfig {
	return RedisConfig{
		Host:     GetEnv("REDIS_HOST", "localhost"),
		Port:     GetEnv("REDIS_PORT", "6379"),
		Password: GetEnv("REDIS_PASSWORD", ""),
		DB:       GetEnvInt("REDIS_DB", 0),
	}
}

// Addr returns the Redis address in "host:port" format
func (c RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

// InitRedisClient initializes Redis client with the given configuration
// Returns nil if Redis is not configured (host is empty)
func InitRedisClient(cfg RedisConfig) *redis.Client {
	if cfg.Host == "" {
		log.Println("⚠️  Redis not configured, skipping initialization")
		return nil
	}

	addr := cfg.Addr()
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	log.Printf("✅ Redis client initialized: %s (DB: %d)", addr, cfg.DB)
	return client
}

// InitRedisClientFromEnv is a convenience function that loads config from env and initializes Redis
func InitRedisClientFromEnv() *redis.Client {
	cfg := LoadRedisConfig()
	return InitRedisClient(cfg)
}

// Note: VectorDBConfig is defined in vector.go
// LoadVectorDBConfigFromEnv loads vector DB configuration from environment variables
// Uses the VectorDBConfig from vector.go which has more complete field definitions
func LoadVectorDBConfigFromEnv(defaults VectorDBConfig) VectorDBConfig {
	return VectorDBConfig{
		Host:      GetEnv("VECTOR_DB_HOST", defaults.Host),
		Port:      GetEnv("VECTOR_DB_PORT", defaults.Port),
		Name:      GetEnv("VECTOR_DB_NAME", defaults.Name),
		User:      GetEnv("VECTOR_DB_USER", defaults.User),
		Password:  GetEnv("VECTOR_DB_PASSWORD", defaults.Password),
		Schema:    GetEnv("VECTOR_DB_SCHEMA", defaults.Schema),
		Table:     GetEnv("VECTOR_DB_TABLE", defaults.Table),
		Dimension: GetEnvInt("VECTOR_DB_DIMENSION", defaults.Dimension),
		SSLMode:   GetEnv("VECTOR_DB_SSL_MODE", defaults.SSLMode),
	}
}

// BuildVectorDBDSN builds PostgreSQL DSN for vector database
func BuildVectorDBDSN(cfg VectorDBConfig) string {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, sslMode,
	)
	if cfg.Schema != "" {
		dsn += fmt.Sprintf(" search_path=%s", cfg.Schema)
	}
	return dsn
}
