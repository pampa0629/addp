package config

import (
    "fmt"
    "os"
    "strconv"
    "time"

    "gopkg.in/yaml.v3"
)

// Config 应用配置
type Config struct {
    // 服务配置
    Port string `yaml:"port"`

    // PostgreSQL 配置
    Database DatabaseConfig `yaml:"database"`

    // Redis 配置
    Redis RedisConfig `yaml:"redis"`

    // MinIO 配置
    MinIO MinIOConfig `yaml:"minio"`

    // 缓存策略配置
    CachePolicy CachePolicyConfig `yaml:"cache_policy"`

    // 预热配置
    Prewarm PrewarmConfig `yaml:"prewarm"`

    // 日志配置
    LogLevel string `yaml:"log_level"`

    // CORS 配置
    FrontendURL string `yaml:"frontend_url"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
    Host            string        `yaml:"host"`
    Port            string        `yaml:"port"`
    Name            string        `yaml:"name"`
    User            string        `yaml:"user"`
    Password        string        `yaml:"password"`
    MaxOpenConns    int           `yaml:"max_open_conns"`
    MaxIdleConns    int           `yaml:"max_idle_conns"`
    ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
    Host      string        `yaml:"host"`
    Port      string        `yaml:"port"`
    Password  string        `yaml:"password"`
    CacheTTL  time.Duration `yaml:"cache_ttl"`
    MaxMemory string        `yaml:"max_memory"`
}

// CachePolicyConfig 缓存落库策略（阈值）
type CachePolicyConfig struct {
    // 生成耗时达到该阈值则落库（默认 1s）
    PersistMinDuration time.Duration `yaml:"persist_min_duration"`
    // 压缩前（原始 MVT）大小达到该阈值（单位 KB）则落库（默认 50KB）
    PersistMinRawKB    int           `yaml:"persist_min_raw_kb"`
    // 内存 LRU 容量（默认 8192）
    MemoryLRUSize      int           `yaml:"memory_lru_size"`
}

// MinIOConfig MinIO 对象存储配置
type MinIOConfig struct {
    Endpoint  string `yaml:"endpoint"`
    AccessKey string `yaml:"access_key"`
    SecretKey string `yaml:"secret_key"`
    Bucket    string `yaml:"bucket"`
    UseSSL    bool   `yaml:"use_ssl"`
    Region    string `yaml:"region"`
}

// PrewarmConfig 预热
type PrewarmConfig struct {
    Enabled     bool `yaml:"enabled"`
    // 预热的最大缩放级别（默认 6）
    MaxZoom     int  `yaml:"max_zoom"`
    // 预热并发数（默认 2）
    Concurrency int  `yaml:"concurrency"`
}

// Load 加载配置
func Load() (*Config, error) {
	var config *Config

	// 1. 优先从 YAML 配置文件加载
	if path := getEnv("APP_CONFIG", "config/app.yaml"); path != "" {
		fmt.Printf("[DEBUG CONFIG] Attempting to load YAML from: %s\n", path)
		if data, err := os.ReadFile(path); err == nil {
			fmt.Printf("[DEBUG CONFIG] Successfully read YAML file, size: %d bytes\n", len(data))
			var yc Config
			if err := unmarshalYAML(data, &yc); err == nil {
				config = &yc
				fmt.Printf("[DEBUG CONFIG] YAML parsing successful\n")
				fmt.Printf("[DEBUG CONFIG] YAML Database Port: %s, Name: %s, User: %s\n",
					yc.Database.Port, yc.Database.Name, yc.Database.User)
			} else {
				fmt.Printf("[DEBUG CONFIG] YAML parsing failed: %v\n", err)
			}
		} else {
			fmt.Printf("[DEBUG CONFIG] Failed to read YAML file: %v\n", err)
		}
	}

	// 2. 如果 YAML 加载失败，使用最小默认配置
    if config == nil {
        fmt.Printf("[DEBUG CONFIG] Using fallback default config\n")
        config = &Config{
            Port:        "8090",
            FrontendURL: "http://localhost:5180",
            LogLevel:    "info",
            Database: DatabaseConfig{
                Host:            "localhost",
                Port:            "5432",
                MaxOpenConns:    25,
                MaxIdleConns:    5,
                ConnMaxLifetime: 5 * time.Minute,
            },
            Redis: RedisConfig{
                Host:      "localhost",
                Port:      "6379",
                Password:  "addp_redis",
                CacheTTL:  24 * time.Hour,
                MaxMemory: "2gb",
            },
            MinIO: MinIOConfig{
                Endpoint:  "localhost:9002",
                AccessKey: "minioadmin",
                SecretKey: "minioadmin",
                Bucket:    "addp-mvt-cache",
                UseSSL:    false,
                Region:    "",
            },
            CachePolicy: CachePolicyConfig{
                PersistMinDuration: 1 * time.Second,
                PersistMinRawKB:    50,
                MemoryLRUSize:      8192,
            },
            Prewarm: PrewarmConfig{
                Enabled:     true,
                MaxZoom:     6,
                Concurrency: 2,
            },
        }
    }

	// 3. 环境变量覆盖（优先级最高）
	if v := os.Getenv("PORT"); v != "" {
		config.Port = v
	}
	if v := os.Getenv("FRONTEND_URL"); v != "" {
		config.FrontendURL = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		config.LogLevel = v
	}

	// 数据库环境变量覆盖
	if v := os.Getenv("DATABASE_HOST"); v != "" {
		config.Database.Host = v
	}
	if v := os.Getenv("DATABASE_PORT"); v != "" {
		config.Database.Port = v
	}
	if v := os.Getenv("DATABASE_NAME"); v != "" {
		config.Database.Name = v
	}
	if v := os.Getenv("DATABASE_USER"); v != "" {
		config.Database.User = v
	}
	if v := os.Getenv("DATABASE_PASSWORD"); v != "" {
		config.Database.Password = v
	}
	if v := os.Getenv("DATABASE_MAX_OPEN_CONNS"); v != "" {
		if intVal, err := strconv.Atoi(v); err == nil {
			config.Database.MaxOpenConns = intVal
		}
	}
	if v := os.Getenv("DATABASE_MAX_IDLE_CONNS"); v != "" {
		if intVal, err := strconv.Atoi(v); err == nil {
			config.Database.MaxIdleConns = intVal
		}
	}
	if v := os.Getenv("DATABASE_CONN_MAX_LIFETIME"); v != "" {
		if duration, err := time.ParseDuration(v); err == nil {
			config.Database.ConnMaxLifetime = duration
		}
	}

	// Redis 环境变量覆盖
	if v := os.Getenv("REDIS_HOST"); v != "" {
		config.Redis.Host = v
	}
	if v := os.Getenv("REDIS_PORT"); v != "" {
		config.Redis.Port = v
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		config.Redis.Password = v
	}
    if v := os.Getenv("CACHE_TTL"); v != "" {
        if duration, err := time.ParseDuration(v); err == nil {
            config.Redis.CacheTTL = duration
        }
    }
    if v := os.Getenv("CACHE_MAX_MEMORY"); v != "" {
        config.Redis.MaxMemory = v
    }

    // MinIO 环境变量覆盖
    if v := os.Getenv("MINIO_ENDPOINT"); v != "" {
        config.MinIO.Endpoint = v
    }
    if v := os.Getenv("MINIO_ACCESS_KEY"); v != "" {
        config.MinIO.AccessKey = v
    }
    if v := os.Getenv("MINIO_SECRET_KEY"); v != "" {
        config.MinIO.SecretKey = v
    }
    if v := os.Getenv("MINIO_BUCKET"); v != "" {
        config.MinIO.Bucket = v
    }
    if v := os.Getenv("MINIO_USE_SSL"); v != "" {
        config.MinIO.UseSSL = v == "1" || v == "true" || v == "TRUE"
    }
    if v := os.Getenv("MINIO_REGION"); v != "" {
        config.MinIO.Region = v
    }

    // 缓存策略环境变量
    if v := os.Getenv("CACHE_PERSIST_MIN_DURATION"); v != "" {
        if duration, err := time.ParseDuration(v); err == nil {
            config.CachePolicy.PersistMinDuration = duration
        }
    }
    if v := os.Getenv("CACHE_PERSIST_MIN_RAW_KB"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            config.CachePolicy.PersistMinRawKB = n
        }
    }
    if v := os.Getenv("CACHE_MEMORY_LRU_SIZE"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            if n > 0 {
                config.CachePolicy.MemoryLRUSize = n
            }
        }
    }

    // 预热环境变量
    if v := os.Getenv("PREWARM_ENABLED"); v != "" {
        config.Prewarm.Enabled = v == "1" || v == "true" || v == "TRUE"
    }
    if v := os.Getenv("PREWARM_MAX_ZOOM"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            config.Prewarm.MaxZoom = n
        }
    }
    if v := os.Getenv("PREWARM_CONCURRENCY"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            if n <= 0 { n = 1 }
            config.Prewarm.Concurrency = n
        }
    }

	fmt.Printf("[DEBUG CONFIG] Final config - Port: %s, Database: %s@%s:%s/%s\n",
		config.Port, config.Database.User, config.Database.Host,
		config.Database.Port, config.Database.Name)

	return config, nil
}

// GetDSN 获取 PostgreSQL 连接字符串
func (c *Config) GetDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		c.Database.Host,
		c.Database.Port,
		c.Database.User,
		c.Database.Password,
		c.Database.Name,
	)
}

// GetRedisAddr 获取 Redis 地址
func (c *Config) GetRedisAddr() string {
	return fmt.Sprintf("%s:%s", c.Redis.Host, c.Redis.Port)
}

// 辅助函数：获取环境变量（带默认值）
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// 轻量封装 YAML 解析，集中 import 以避免在顶部增加依赖噪音
func unmarshalYAML(data []byte, v interface{}) error { return yaml.Unmarshal(data, v) }
