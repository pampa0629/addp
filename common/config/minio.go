package config

import "fmt"

// BuiltinMinIOConfig describes the platform built-in MinIO from the shared .env.
// Business data engines still come from System engine configuration.
type BuiltinMinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

func LoadBuiltinMinIOConfig() BuiltinMinIOConfig {
	minioPort := GetEnv("MINIO_API_PORT", "19000")
	defaultEndpoint := fmt.Sprintf("localhost:%s", minioPort)
	return BuiltinMinIOConfig{
		Endpoint:  GetEnv("MINIO_ENDPOINT", defaultEndpoint),
		AccessKey: GetEnv("MINIO_ROOT_USER", "minioadmin"),
		SecretKey: GetEnv("MINIO_ROOT_PASSWORD", "minioadmin"),
		UseSSL:    GetEnvBool("MINIO_USE_SSL", false),
	}
}
