package config

import "time"

// VectorDBConfig 描述 PgVector 连接所需的字段
type VectorDBConfig struct {
	Host      string
	Port      string
	Name      string
	User      string
	Password  string
	Schema    string
	Table     string
	Dimension int
	SSLMode   string
}

// EmbeddingServiceConfig 描述在线向量化服务所需参数
type EmbeddingServiceConfig struct {
	BaseURL    string
	APIKey     string
	TextModel  string
	ImageModel string
	AudioModel string
	VideoModel string
	Timeout    time.Duration
}
