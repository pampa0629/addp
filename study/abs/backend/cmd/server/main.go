package main

import (
	"abs/internal/api"
	"abs/internal/service"
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	// Load configuration
	config := service.LoadConfig()

	// Validate configuration
	switch strings.ToLower(config.CodeGenProvider) {
	case "claude":
		if config.ClaudeAPIKey == "" && config.ClaudeAuthToken == "" {
			log.Fatal("ANTHROPIC_API_KEY or ANTHROPIC_AUTH_TOKEN is required when CODE_GENERATOR=claude")
		}
	case "codex":
		// Codex API 模式需要 API Key
		if config.CodexAPIKey == "" {
			log.Fatal("CODEX_API_KEY environment variable is required when CODE_GENERATOR=codex")
		}
	case "codex_cli", "codex-cli":
		// Codex CLI 模式从 ~/.codex/.apikey 读取，不需要环境变量
		log.Println("Using Codex CLI mode - API key will be read from ~/.codex/.apikey")
	default:
		// 默认使用 codex_cli，不需要验证
		log.Println("Using default code generator (codex_cli)")
	}

	// Create workspace directory
	if err := os.MkdirAll(config.WorkspaceDir, 0755); err != nil {
		log.Fatalf("Failed to create workspace directory: %v", err)
	}

	// Initialize services
	wsManager := service.NewWebSocketManager()
	appService, err := service.NewAppService(config)
	if err != nil {
		log.Fatalf("failed to init app service: %v", err)
	}
	taskService := service.NewTaskService(config, wsManager, appService)

	// Setup router
	router := api.SetupRouter(taskService, appService, wsManager, config)

	// Start server
	addr := fmt.Sprintf(":%s", config.Port)
	log.Printf("Starting ABS backend server on %s", addr)
	log.Printf("Frontend URL: %s", config.FrontendURL)
	log.Printf("Code generator provider: %s", strings.ToUpper(config.CodeGenProvider))

	switch strings.ToLower(config.CodeGenProvider) {
	case "claude":
		log.Printf("Claude API URL: %s", config.ClaudeBaseURL)
		log.Printf("Claude model: %s", config.ClaudeModel)
	case "codex":
		log.Printf("Codex API URL: %s", config.CodexBaseURL)
		log.Printf("Codex model: %s", config.CodexModel)
	case "codex_cli", "codex-cli":
		log.Printf("Codex CLI path: %s", config.CodexCLIPath)
		log.Printf("Codex CLI args: %v", config.CodexCLIArgs)
		log.Printf("Codex CLI timeout: %s", config.CodexCLITimeout)
	}

	log.Printf("Workspace directory: %s", config.WorkspaceDir)
	log.Printf("App registry file: %s", config.AppsDataFile)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
