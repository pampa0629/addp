package service

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds the application configuration
type Config struct {
	Port            string
	ClaudeAPIKey    string
	ClaudeAuthToken string
	ClaudeBaseURL   string
	ClaudeModel     string
	CodexAPIKey     string
	CodexBaseURL    string
	CodexModel      string
	CodexCLIPath    string
	CodexCLIArgs    []string
	CodexCLITimeout time.Duration
	CodeGenProvider string
	FrontendURL     string
	WorkspaceDir    string
	AppsDataFile    string
	AutoReload      bool
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	// Load .env file if it exists
	_ = godotenv.Load()

	provider := strings.ToLower(getEnv("CODE_GENERATOR", "codex_cli"))

	// Get workspace directory and convert to absolute path
	workspaceDir := getEnv("WORKSPACE_DIR", "./workspace")
	absWorkspaceDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		// Fallback to relative path if conversion fails
		absWorkspaceDir = workspaceDir
	}

	// Get apps data file path and convert to absolute path
	appsDataFile := getEnv("APPS_DATA_FILE", "./workspace/apps.json")
	absAppsDataFile, err := filepath.Abs(appsDataFile)
	if err != nil {
		// Fallback to relative path if conversion fails
		absAppsDataFile = appsDataFile
	}

	return &Config{
		Port:            getEnv("PORT", "8090"),
		ClaudeAPIKey:    getEnv("ANTHROPIC_API_KEY", ""),
		ClaudeAuthToken: getEnv("ANTHROPIC_AUTH_TOKEN", ""),
		ClaudeBaseURL:   getEnv("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
		ClaudeModel:     getEnv("CLAUDE_MODEL", "claude-sonnet-4-5-20250929"),
		CodexAPIKey:     getEnv("CODEX_API_KEY", ""),
		CodexBaseURL:    getEnv("CODEX_BASE_URL", "https://api.aicodemirror.com/api/codex/backend-api/codex"),
		CodexModel:      getEnv("CODEX_MODEL", "gpt-5"),
		CodexCLIPath:    getEnv("CODEX_CLI_PATH", "codex"),
		CodexCLIArgs:    parseCLIArgs(getEnv("CODEX_CLI_ARGS", "--skip-git-repo-check --full-auto")),
		CodexCLITimeout: getEnvDuration("CODEX_CLI_TIMEOUT", 5*time.Minute),
		CodeGenProvider: provider,
		FrontendURL:     getEnv("FRONTEND_URL", "http://localhost:5180"),
		WorkspaceDir:    absWorkspaceDir,
		AppsDataFile:    absAppsDataFile,
		AutoReload:      getEnv("AUTO_RELOAD", "true") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

func parseCLIArgs(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return strings.Fields(trimmed)
}
